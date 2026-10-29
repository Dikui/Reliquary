package service

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/vaultpath"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type vaultRecordingEmbedder struct {
	chunkSearchEmbedder
	mu           sync.Mutex
	inputs       []string
	callContexts []context.Context
}

func (e *vaultRecordingEmbedder) BatchEmbedWithPool(ctx context.Context, _ embedding.Embedder, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.inputs = append(e.inputs, texts...)
	e.callContexts = append(e.callContexts, ctx)
	e.mu.Unlock()
	return e.chunkSearchEmbedder.BatchEmbed(ctx, texts)
}

func (e *vaultRecordingEmbedder) inputCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.inputs)
}

type vaultDocumentModels struct {
	interfaces.ModelService
	embedder *vaultRecordingEmbedder
}

func (m vaultDocumentModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.embedder, nil
}

type vaultDocumentHarness struct {
	*retainedIntakeHarness
	documents *VaultDocumentService
	embedder  *vaultRecordingEmbedder
	root      string
}

func newVaultDocumentHarness(t *testing.T, wait time.Duration) *vaultDocumentHarness {
	t.Helper()
	h := newRetainedIntakeHarness(t)
	h.library.ModelTiers = types.ModelTierConfig{Embedding: "embedding-fixture"}
	h.library.IndexingStrategy = types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true}
	h.library.ChunkingConfig = types.ChunkingConfig{ChunkSize: 160, ChunkOverlap: 0, ChunkOverlapSet: true, Strategy: chunker.StrategyHeading}
	require.NoError(t, h.db.Save(h.library).Error)
	require.NoError(t, h.db.Model(&types.Space{}).Where("id=1").Update("embedding_model_id", h.library.ModelTiers.Embedding).Error)
	require.NoError(t, h.db.Model(&types.Library{}).Where("space_id=1").Updates(map[string]any{"chunking_config": h.library.ChunkingConfig, "indexing_strategy": h.library.IndexingStrategy}).Error)
	e := &vaultRecordingEmbedder{}
	root := t.TempDir()
	r := retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(h.db))
	s, err := NewVaultDocumentService(h.db, vaultDocumentModels{embedder: e}, r, root)
	require.NoError(t, err)
	s.reportStableWait = wait
	s.reportScanInterval = 25 * time.Millisecond
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return &vaultDocumentHarness{h, s, e, root}
}

func (h *vaultDocumentHarness) byPath(t *testing.T, path string) *types.Entry {
	t.Helper()
	var entry types.Entry
	require.NoError(t, h.db.Where("vault_path=?", path).First(&entry).Error)
	return &entry
}

func (h *vaultDocumentHarness) write(t *testing.T, path, body string) string {
	t.Helper()
	absolute := filepath.Join(h.root, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(absolute), 0o755))
	require.NoError(t, os.WriteFile(absolute, []byte(body), 0o644))
	return absolute
}

func (h *vaultDocumentHarness) assertSearch(t *testing.T, id, term string) {
	t.Helper()
	var entry types.Entry
	require.NoError(t, h.db.First(&entry, "id=?", id).Error)
	hits, err := h.documents.retrieval.Retrieve(h.ctx, []types.RetrieveParams{{RetrieverType: types.KeywordsRetrieverType,
		LibraryIDs: []string{entry.LibraryID}, EntryIDs: []string{id}, Query: term, TopK: 10}})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
}

func TestVaultPersonalFileChangesDoNotOverwriteStoredBody(t *testing.T) {
	h := newVaultDocumentHarness(t, 20*time.Millisecond)
	var own types.Library
	require.NoError(t, h.db.Where("system_role=?", "personal").First(&own).Error)
	own.ChunkingConfig = h.library.ChunkingConfig
	own.IndexingStrategy = h.library.IndexingStrategy
	h.library = &own
	header := VaultArticleHeader{ID: uuid.NewString(), SourceKind: "mine", Title: "Imported personal document", CollectedAt: time.Now().UTC().Format(time.RFC3339Nano), ContentType: "tutorial"}
	body := "# Imported personal document\n\noriginalminefixture remains indexed from import"
	document, err := h.documents.restoreVaultArticle(h.ctx, h.library, "personal/imported.md", header, []byte(body))
	require.NoError(t, err)
	require.NoError(t, h.documents.indexVaultChunks(h.ctx, h.library, document, body))
	path := h.write(t, document.VaultPath, string(RenderVaultArticleHeader(header))+body)
	h.assertSearch(t, document.ID, "originalminefixture")
	calls := h.embedder.inputCount()
	require.NoError(t, h.documents.Start(h.ctx))
	h.write(t, "personal/hand-placed.md", "# Hand placed\n\nmanualminefixture must stay unindexed")
	require.NoError(t, os.WriteFile(path, []byte("# Modified\n\nmodifiedminefixture must stay unindexed"), 0644))
	time.Sleep(120 * time.Millisecond)
	require.NoError(t, h.documents.Scan(h.ctx, true))
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("source_kind='mine'").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.Equal(t, calls, h.embedder.inputCount())
	require.Equal(t, body, h.byPath(t, document.VaultPath).VaultBody)
	h.assertSearch(t, document.ID, "originalminefixture")
	var modifiedChunks int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("content LIKE '%modifiedminefixture%'").Count(&modifiedChunks).Error)
	require.Zero(t, modifiedChunks)
	require.NoError(t, os.Remove(path))
	require.NoError(t, h.documents.Scan(h.ctx, true))
	require.Equal(t, body, h.byPath(t, document.VaultPath).VaultBody)
	require.Contains(t, h.documents.watcher.WatchList(), filepath.Join(h.root, "personal"))
	status, err := h.documents.Status(h.ctx)
	require.NoError(t, err)
	require.NotNil(t, vaultStatusLibrary(t, status, "personal").LastScanAt)
	require.EqualValues(t, 1, vaultStatusLibrary(t, status, "personal").ArchivedEntries)
}

func TestVaultWatcherAndIndexIgnoreObsidianConfigurationWithoutChangingFiles(t *testing.T) {
	h := newVaultDocumentHarness(t, 40*time.Millisecond)
	configuration := map[string]string{
		".obsidian/app.json":                        `{"theme":"obsidian"}`,
		".trash/x.md":                               "# Trashed root note\n",
		".DS_Store":                                 "Finder metadata",
		"personal/.obsidian/app.json":               `{"readableLineLength":true}`,
		"personal/.trash/x.md":                      string([]byte{0xff, 0xfe}),
		"personal/nested/._private.md":              "# AppleDouble note\n",
		"ai-output/.obsidian/templates/template.md": "# Editor template\n",
		"ai-output/.trash/x.md":                     "# Trashed report\n",
		"ai-output/.DS_Store":                       "Report Finder metadata",
	}
	for relative, data := range configuration {
		h.write(t, relative, data)
	}
	h.write(t, "personal/keep.md", "# Personal note\n\nobsidianfixture is not imported")
	report, err := h.documents.CreateReport(h.ctx, "Report", "# Report\n\nreportobsidianfixture remains indexed", nil)
	require.NoError(t, err)
	require.NoError(t, h.documents.Start(h.ctx))
	require.Eventually(t, func() bool {
		var entry types.Entry
		return h.db.Where("vault_path=? AND parse_status=?", report.VaultPath, types.ParseStatusCompleted).First(&entry).Error == nil
	}, 3*time.Second, 20*time.Millisecond)
	// New editor directories exercise the event handler as well as startup walks.
	configuration["personal/new/.obsidian/template.md"] = "# New hidden template\n"
	configuration["ai-output/.trash/new.md"] = "# New discarded report\n"
	configuration["personal/.obsidian/app.json"] = `{"readableLineLength":false}`
	fixed := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	for relative, data := range configuration {
		path := h.write(t, relative, data)
		require.NoError(t, os.Chtimes(path, fixed, fixed))
	}
	h.write(t, report.VaultPath, "# Report\n\nreportobsidianfixture remains indexed")
	require.Eventually(t, func() bool {
		var entry types.Entry
		return h.db.Where("vault_path=? AND parse_status=?", report.VaultPath, types.ParseStatusCompleted).First(&entry).Error == nil
	}, 3*time.Second, 20*time.Millisecond)
	require.NoError(t, h.documents.Scan(h.ctx, true))
	for _, path := range h.documents.watcher.WatchList() {
		relative, err := filepath.Rel(h.root, path)
		require.NoError(t, err)
		require.False(t, vaultpath.UserMetadata(relative), "editor directories must not receive watches")
	}
	require.NoError(t, h.documents.Close())
	var indexed []string
	require.NoError(t, h.db.Model(&types.Entry{}).Order("vault_path").Pluck("vault_path", &indexed).Error)
	require.Equal(t, []string{report.VaultPath}, indexed)
	for relative := range h.documents.observations {
		require.False(t, vaultpath.UserMetadata(relative), "ignored event paths must not enter indexing observations")
	}
	for relative, data := range configuration {
		actual, err := os.ReadFile(filepath.Join(h.root, relative))
		require.NoError(t, err)
		require.Equal(t, data, string(actual))
		info, err := os.Stat(filepath.Join(h.root, relative))
		require.NoError(t, err)
		require.Equal(t, fixed, info.ModTime().UTC())
	}
	status, err := h.documents.Status(h.ctx)
	require.NoError(t, err)
	require.Zero(t, vaultStatusLibrary(t, status, "personal").ArchivedEntries)
	require.EqualValues(t, 1, vaultStatusLibrary(t, status, "ai-output").ArchivedEntries)
	require.Empty(t, vaultStatusLibrary(t, status, "personal").LastError)
	require.Empty(t, vaultStatusLibrary(t, status, "ai-output").LastError)
}

func TestVaultCancelledReportScanPreservesLastCompletedStatus(t *testing.T) {
	for _, queryTable := range []string{"libraries", "entries"} {
		t.Run(queryTable, func(t *testing.T) {
			h := newVaultDocumentHarness(t, 20*time.Millisecond)
			report, err := h.documents.CreateReport(h.ctx, "Original report", "# Original report\n\nKeep the last completed report status.", nil)
			require.NoError(t, err)
			path := filepath.Join(h.root, report.VaultPath)
			require.NoError(t, h.documents.Scan(h.ctx, true))
			before, err := h.documents.Status(h.ctx)
			require.NoError(t, err)
			require.NotNil(t, vaultStatusLibrary(t, before, "ai-output").LastScanAt)
			calls := h.embedder.inputCount()
			require.NoError(t, os.WriteFile(path, []byte("# Changed report\n\nInterrupt this scan during shutdown."), 0644))
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			callback := "reliquary:test_cancel_report_scan"
			require.NoError(t, h.db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == queryTable {
					cancel()
				}
			}))
			t.Cleanup(func() { require.NoError(t, h.db.Callback().Query().Remove(callback)) })
			err = h.documents.Scan(ctx, true)
			require.ErrorIs(t, err, context.Canceled)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			after, err := h.documents.Status(h.ctx)
			require.NoError(t, err)
			require.Equal(t, vaultStatusLibrary(t, before, "ai-output"), vaultStatusLibrary(t, after, "ai-output"))
			require.Equal(t, calls, h.embedder.inputCount())
		})
	}
}

func TestVaultReportImmediatelyIndexesReadOnlyBodyAndValidatesRelations(t *testing.T) {
	h := newVaultDocumentHarness(t, 10*time.Minute)
	report, err := h.documents.createReport(h.ctx, h.library, "整理:结果/中文", "# 整理结果\n\nreportfixture compares three releases", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "report", report.SourceKind)
	require.Equal(t, "整理:结果/中文", report.Title)
	require.Regexp(t, `^official/整理结果中文-[0-9a-f]{8}\.md$`, report.VaultPath)
	h.assertSearch(t, report.ID, "reportfixture")
	require.NoFileExists(t, filepath.Join(h.root, report.VaultPath))
	require.NoError(t, (&VaultService{db: h.db, root: h.root}).RefreshHeader(h.ctx, report.ID))
	content, err := os.ReadFile(filepath.Join(h.root, report.VaultPath))
	require.NoError(t, err)
	require.Contains(t, string(content), "title: \"整理:结果/中文\"")
	require.NotContains(t, string(content), "related_entry_ids:")
	require.True(t, strings.HasSuffix(string(content), "# 整理结果\n\nreportfixture compares three releases"))
	count := h.embedder.inputCount()
	require.NoError(t, h.documents.Scan(h.ctx, false))
	require.Equal(t, count, h.embedder.inputCount())
	_, err = h.documents.CreateReport(h.ctx, "bad", "some Markdown", []string{uuid.NewString()})
	require.ErrorContains(t, err, "related_entry_ids[0]")
	require.ErrorContains(t, err, "does not exist or is deleted")
	_, err = h.documents.CreateReport(h.ctx, "", "body", nil)
	require.Error(t, err)
	_, err = h.documents.CreateReport(h.ctx, "Empty", " \n", nil)
	require.Error(t, err)
}

func vaultFileHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(rel)] = vaultContentHash(string(body))
		return nil
	}))
	return result
}

func TestVaultRebuildKeywordOnlyNeedsNoModelAndSkipsInvalidArticles(t *testing.T) {
	h := newVaultDocumentHarness(t, time.Millisecond)
	h.library.ModelTiers = types.ModelTierConfig{}
	h.library.EmbeddingModelID = ""
	h.library.IndexingStrategy = types.IndexingStrategy{KeywordEnabled: true}
	require.NoError(t, h.db.Save(h.library).Error)
	require.NoError(t, h.db.Model(&types.Space{}).Where("id=1").Update("embedding_model_id", h.library.ModelTiers.Embedding).Error)
	require.NoError(t, h.db.Model(&types.Library{}).Where("space_id=1").Updates(map[string]any{"chunking_config": h.library.ChunkingConfig, "indexing_strategy": h.library.IndexingStrategy}).Error)
	h.documents.models = nil
	now := time.Now().UTC().Format(time.RFC3339Nano)
	valid := VaultArticleHeader{LibraryID: h.library.ID, ID: uuid.NewString(), Title: "无模型重建", CollectedAt: now, ContentType: "event"}
	body := append(RenderVaultArticleHeader(valid), []byte("# 无模型重建\n\nkeywordfixture survives without embedding setup")...)
	h.write(t, "official/2026-10/valid.md", string(body))
	h.write(t, "official/2026-10/zz-duplicate.md", string(body))
	for index, invalid := range []VaultArticleHeader{
		{ID: "not-uuid", Title: "Bad ID", CollectedAt: now},
		{ID: uuid.NewString(), Title: "Bad links", CollectedAt: now, Entities: []string{"plain name"}},
		{ID: uuid.NewString(), Title: "Bad date", CollectedAt: "yesterday"},
		{ID: uuid.NewString(), Title: "Bad library", CollectedAt: now, LibraryID: "unknown"},
	} {
		h.write(t, fmt.Sprintf("official/2026-10/bad-%d.md", index), string(RenderVaultArticleHeader(invalid)))
	}
	h.write(t, "personal/first.md", "# 第一手\n\nindependentfixture legacy note")
	var mine types.Library
	require.NoError(t, h.db.Where("system_role=?", "personal").First(&mine).Error)
	personal := VaultArticleHeader{LibraryID: mine.ID, ID: uuid.NewString(), SourceKind: "mine", Title: "Imported tutorial", CollectedAt: now, ContentType: "tutorial"}
	// New exports omit source_kind. An older file still retains its imported-document identity.
	legacyHeader := strings.Replace(string(RenderVaultArticleHeader(personal)), "---\n", "---\nsource_kind: mine\n", 1)
	h.write(t, "personal/imported.md", legacyHeader+"# Imported tutorial\n\npersonaltutorialfixture full text")
	h.write(t, "personal/bad-utf8.md", string([]byte{0xff, 0xfe}))
	h.write(t, "ai-output/bad-utf8.md", string([]byte{0xff, 0xfe}))
	for relative, data := range map[string]string{
		".obsidian/app.json": `{"readingView":true}`, ".trash/x.md": "discarded article without a header", ".DS_Store": "Finder metadata",
		"personal/.trash/x.md": string([]byte{0xff}), "ai-output/.obsidian/template.md": "Editor template",
		"official/.trash/x.md": string(body),
	} {
		h.write(t, relative, data)
	}
	before := vaultFileHashes(t, h.root)
	result, err := h.documents.Rebuild(h.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Articles)
	require.Len(t, result.Skipped, 8)
	require.EqualValues(t, 1, result.Mine)
	require.Zero(t, h.embedder.inputCount())
	require.Equal(t, before, vaultFileHashes(t, h.root))
	h.assertSearch(t, valid.ID, "keywordfixture")
	paths := []string{}
	for _, skip := range result.Skipped {
		paths = append(paths, skip.Path)
		require.NotEmpty(t, skip.Error)
	}
	require.True(t, sort.StringsAreSorted(paths))
}

func TestVaultDirectorySymlinkDoesNotDeleteExistingIndexOrWriteReport(t *testing.T) {
	h := newVaultDocumentHarness(t, time.Millisecond)
	report, err := h.documents.CreateReport(h.ctx, "First", "# First\n\nsymlinkfixture retained index", nil)
	require.NoError(t, err)
	require.NoError(t, h.documents.Scan(h.ctx, true))
	document := h.byPath(t, report.VaultPath)
	external := t.TempDir()
	require.NoError(t, os.RemoveAll(filepath.Join(h.root, "ai-output")))
	require.NoError(t, os.Symlink(external, filepath.Join(h.root, "ai-output")))
	require.ErrorContains(t, h.documents.Scan(h.ctx, true), "symbolic link")
	var saved types.Entry
	require.NoError(t, h.db.Where("id=?", document.ID).First(&saved).Error)
	_, err = h.documents.CreateReport(h.ctx, "outside", "report body", nil)
	require.ErrorContains(t, err, "symbolic link")
	entries, err := os.ReadDir(external)
	require.NoError(t, err)
	require.Empty(t, entries)
}
