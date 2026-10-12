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

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/config"
	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/vaultpath"
)

type vaultRecordingEmbedder struct {
	chunkSearchEmbedder
	mu     sync.Mutex
	inputs []string
}

func (e *vaultRecordingEmbedder) BatchEmbedWithPool(ctx context.Context, _ embedding.Embedder, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.inputs = append(e.inputs, texts...)
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
	h.kb.ModelTiers = types.ModelTierConfig{Embedding: "embedding-fixture"}
	h.kb.IndexingStrategy = types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true}
	h.kb.ChunkingConfig = types.ChunkingConfig{ChunkSize: 160, ChunkOverlap: 0, ChunkOverlapSet: true, Strategy: chunker.StrategyHeading}
	require.NoError(t, h.db.Save(h.kb).Error)
	e := &vaultRecordingEmbedder{}
	root := t.TempDir()
	r := retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(h.db))
	s, err := NewVaultDocumentService(h.db, vaultDocumentModels{embedder: e}, r, root, wait, 25*time.Millisecond)
	require.NoError(t, err)
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
	hits, err := h.documents.retrieval.Retrieve(h.ctx, []types.RetrieveParams{{RetrieverType: types.KeywordsRetrieverType,
		KnowledgeBaseIDs: []string{h.kb.ID}, EntryIDs: []string{id}, Query: term, TopK: 10}})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
}

func TestVaultMineSettlesAndOnlyChangedChunkIsEmbedded(t *testing.T) {
	h := newVaultDocumentHarness(t, 70*time.Millisecond)
	body := "# 私人笔记\n\n## Alpha\n\n" + strings.Repeat("alpha ", 14) + "\n\n## Bravo\n\n" + strings.Repeat("bravo ", 14) + "\n\n## Charlie\n\n" + strings.Repeat("charlie ", 14)
	path := h.write(t, "mine/nested/note.md", body)
	require.NoError(t, h.documents.Scan(h.ctx, false))
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Count(&count).Error)
	require.Zero(t, count, "an actively edited document must not be indexed")
	time.Sleep(90 * time.Millisecond)
	require.NoError(t, h.documents.Scan(h.ctx, false))
	document := h.byPath(t, "mine/nested/note.md")
	require.Equal(t, "私人笔记", document.Title)
	require.Equal(t, "mine", document.SourceKind)
	require.Empty(t, document.ContentType)
	require.Nil(t, document.StructuredSummary)
	var old []*types.Chunk
	require.NoError(t, h.db.Where("entry_id=?", document.ID).Order("chunk_index").Find(&old).Error)
	require.Len(t, old, 3, "heading boundaries should isolate the changed paragraph")
	require.Equal(t, len(old), h.embedder.inputCount())
	h.assertSearch(t, document.ID, "bravo")
	modified := strings.ReplaceAll(body, "bravo ", "delta ")
	require.NoError(t, os.WriteFile(path, []byte(modified), 0o644))
	require.NoError(t, h.documents.Scan(h.ctx, false))
	require.Equal(t, len(old), h.embedder.inputCount(), "wait applies to updates as well")
	time.Sleep(90 * time.Millisecond)
	require.NoError(t, h.documents.Scan(h.ctx, false))
	var current []*types.Chunk
	require.NoError(t, h.db.Where("entry_id=?", document.ID).Order("chunk_index").Find(&current).Error)
	require.Len(t, current, 3)
	require.Equal(t, len(old)+1, h.embedder.inputCount(), "only the changed hash needs a new vector")
	kept := 0
	for _, after := range current {
		for _, before := range old {
			if before.ID == after.ID {
				kept++
			}
		}
	}
	require.Equal(t, 2, kept)
	h.assertSearch(t, document.ID, "delta")
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, modified, string(actual), "indexing is read-only")
	require.NoError(t, h.documents.Scan(h.ctx, false))
	require.Equal(t, len(old)+1, h.embedder.inputCount(), "unchanged files do not invoke the model")
	var relations int64
	require.NoError(t, h.db.Model(&types.EntryEntity{}).Count(&relations).Error)
	require.Zero(t, relations)
	require.NoError(t, h.db.Model(&types.EntryTopic{}).Count(&relations).Error)
	require.Zero(t, relations)
}

func TestVaultMineWatcherRenameAndDeleteCleanRecordsAndVectors(t *testing.T) {
	h := newVaultDocumentHarness(t, 60*time.Millisecond)
	// The preexisting file verifies startup schedules stabilization without an event.
	path := h.write(t, "mine/start.md", "# First heading\n\nwatchfixture searchable paragraph")
	require.NoError(t, h.documents.Start(h.ctx))
	var first types.Entry
	require.Eventually(t, func() bool {
		return h.db.Where("vault_path=? AND parse_status=?", "mine/start.md", types.ParseStatusCompleted).First(&first).Error == nil
	}, 3*time.Second, 20*time.Millisecond)
	renamed := filepath.Join(h.root, "mine", "renamed.md")
	require.NoError(t, os.Rename(path, renamed))
	var second types.Entry
	require.Eventually(t, func() bool {
		var count int64
		if h.db.Model(&types.Entry{}).Where("id=?", first.ID).Count(&count).Error != nil || count != 0 {
			return false
		}
		return h.db.Where("vault_path=? AND parse_status=?", "mine/renamed.md", types.ParseStatusCompleted).First(&second).Error == nil
	}, 3*time.Second, 20*time.Millisecond)
	require.NotEqual(t, first.ID, second.ID)
	var count int64
	require.NoError(t, h.db.Table("embeddings").Where("entry_id=?", first.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, os.Remove(renamed))
	require.Eventually(t, func() bool {
		return h.db.Unscoped().Model(&types.Entry{}).Where("id=?", second.ID).Count(&count).Error == nil && count == 0
	}, 3*time.Second, 20*time.Millisecond)
	require.NoError(t, h.db.Unscoped().Model(&types.Chunk{}).Where("entry_id IN ?", []string{first.ID, second.ID}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, h.db.Table("embeddings").Where("entry_id IN ?", []string{first.ID, second.ID}).Count(&count).Error)
	require.Zero(t, count)
	status, err := h.documents.Status(h.ctx)
	require.NoError(t, err)
	require.NotNil(t, status.Mine.LastScanAt)
	require.Zero(t, status.Mine.IndexedFiles)
	require.Empty(t, status.Mine.LastError)
}

func TestVaultWatcherAndIndexIgnoreObsidianConfigurationWithoutChangingFiles(t *testing.T) {
	h := newVaultDocumentHarness(t, 40*time.Millisecond)
	configuration := map[string]string{
		".obsidian/app.json":                      `{"theme":"obsidian"}`,
		".trash/x.md":                             "# Trashed root note\n",
		".DS_Store":                               "Finder metadata",
		"mine/.obsidian/app.json":                 `{"readableLineLength":true}`,
		"mine/.trash/x.md":                        string([]byte{0xff, 0xfe}),
		"mine/nested/._private.md":                "# AppleDouble note\n",
		"reports/.obsidian/templates/template.md": "# Editor template\n",
		"reports/.trash/x.md":                     "# Trashed report\n",
		"reports/.DS_Store":                       "Report Finder metadata",
	}
	for relative, data := range configuration {
		h.write(t, relative, data)
	}
	h.write(t, "mine/keep.md", "# Personal note\n\nobsidianfixture stays searchable")
	require.NoError(t, h.documents.Start(h.ctx))
	require.Eventually(t, func() bool {
		var entry types.Entry
		return h.db.Where("vault_path=? AND parse_status=?", "mine/keep.md", types.ParseStatusCompleted).First(&entry).Error == nil
	}, 3*time.Second, 20*time.Millisecond)
	// New editor directories exercise the event handler as well as startup walks.
	configuration["mine/new/.obsidian/template.md"] = "# New hidden template\n"
	configuration["reports/.trash/new.md"] = "# New discarded report\n"
	configuration["mine/.obsidian/app.json"] = `{"readableLineLength":false}`
	fixed := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	for relative, data := range configuration {
		path := h.write(t, relative, data)
		require.NoError(t, os.Chtimes(path, fixed, fixed))
	}
	h.write(t, "reports/keep.md", "# Report\n\nreportobsidianfixture remains indexed")
	require.Eventually(t, func() bool {
		var entry types.Entry
		return h.db.Where("vault_path=? AND parse_status=?", "reports/keep.md", types.ParseStatusCompleted).First(&entry).Error == nil
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
	require.Equal(t, []string{"mine/keep.md", "reports/keep.md"}, indexed)
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
	require.EqualValues(t, 1, status.Mine.IndexedFiles)
	require.EqualValues(t, 1, status.Reports.IndexedFiles)
	require.Empty(t, status.Mine.LastError)
	require.Empty(t, status.Reports.LastError)
}

func TestVaultReportImmediatelyIndexesReadOnlyBodyAndValidatesRelations(t *testing.T) {
	h := newVaultDocumentHarness(t, 10*time.Minute)
	report, err := h.documents.CreateReport(h.ctx, "整理:结果/中文", "# 整理结果\n\nreportfixture compares three releases", nil)
	require.NoError(t, err)
	require.Equal(t, "report", report.SourceKind)
	require.Equal(t, "整理结果", report.Title)
	require.Regexp(t, `^reports/\d{4}-\d{2}/整理结果中文-[0-9a-f]{8}\.md$`, report.VaultPath)
	h.assertSearch(t, report.ID, "reportfixture")
	content, err := os.ReadFile(filepath.Join(h.root, report.VaultPath))
	require.NoError(t, err)
	require.Contains(t, string(content), "title: 整理:结果/中文")
	require.Contains(t, string(content), "related_article_ids: []")
	require.True(t, strings.HasSuffix(string(content), "# 整理结果\n\nreportfixture compares three releases"))
	count := h.embedder.inputCount()
	require.NoError(t, h.documents.Scan(h.ctx, false))
	require.Equal(t, count, h.embedder.inputCount())
	_, err = h.documents.CreateReport(h.ctx, "bad", "some Markdown", []string{uuid.NewString()})
	require.ErrorContains(t, err, "unavailable article")
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

func TestVaultRebuildRestoresGeneratedArticlesMetadataAndSearchWithoutWritingFiles(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	root := t.TempDir()
	vault, err := NewVaultService(h.db, &config.Config{Vault: &config.VaultConfig{Root: root, HeaderDebounce: 20 * time.Millisecond}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, vault.Close()) })
	h.service.SetVault(vault)
	topic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{Name: "产品研究", Description: "Original topic description"})
	require.NoError(t, err)
	h.processor.output = articleProcessingOutput(t, "research", []types.EntityCandidate{{Name: "研究实体", EntityType: "company"}}, []string{topic.ID})
	full := h.upload(t, "全文 中文", "# 全文中文\n\nrebuildfixture original research body")
	require.NoError(t, h.db.Model(full).Updates(map[string]interface{}{"starred": true, "collect_reason": "保留实验结果"}).Error)
	require.NoError(t, vault.RefreshHeader(h.ctx, full.ID))
	full = h.article(t, full.ID)
	h.processor.output = validProcessingFixture("tutorial")
	summary := h.upload(t, "摘要 中文", "# 教程\n\nsummaryfixture explains reusable instructions")
	summarySource, err := summary.IntakeMetadata()
	require.NoError(t, err)
	summarySource.SourceInfo.Author = summarySource.SourceInfo.Publisher
	require.NoError(t, summary.SetIntakeMetadata(summarySource))
	require.NoError(t, h.db.Save(summary).Error)
	require.NoError(t, vault.RefreshHeader(h.ctx, summary.ID))
	summary = h.article(t, summary.ID)
	summaryFile, err := os.ReadFile(filepath.Join(root, summary.VaultPath))
	require.NoError(t, err)
	require.NotContains(t, string(summaryFile), "\nauthor:", "author matching the publisher is omitted")
	documents, err := NewVaultDocumentService(h.db, h.service.modelService, h.service.retrieveEngine, root, 20*time.Millisecond, time.Minute)
	require.NoError(t, err)
	mine := filepath.Join(root, "mine", "personal.md")
	require.NoError(t, os.WriteFile(mine, []byte("# 本人文档\n\npersonalfixture first-hand notes"), 0o644))
	report, err := documents.CreateReport(h.ctx, "汇总", "# 汇总\n\nreportrebuildfixture retained synthesis", []string{full.ID})
	require.NoError(t, err)
	bad := filepath.Join(root, "articles", "broken.md")
	require.NoError(t, os.WriteFile(bad, []byte("---\nid: [unterminated\n---\nbody"), 0o644))
	before := vaultFileHashes(t, root)
	// Use a fresh real PostgreSQL database with the same configured infrastructure.
	fresh := newRetainedIntakeHarness(t)
	fresh.kb.ModelTiers = h.kb.ModelTiers
	fresh.kb.EmbeddingModelID = h.kb.EmbeddingModelID
	fresh.kb.IndexingStrategy = h.kb.IndexingStrategy
	require.NoError(t, fresh.db.Save(fresh.kb).Error)
	r := retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(fresh.db))
	rebuild, err := NewVaultDocumentService(fresh.db, h.service.modelService, r, root, 20*time.Millisecond, time.Minute)
	require.NoError(t, err)
	result, err := rebuild.Rebuild(fresh.ctx)
	require.NoError(t, err)
	require.Equal(t, 2, result.Articles)
	require.EqualValues(t, 1, result.Mine)
	require.EqualValues(t, 1, result.Reports)
	require.Len(t, result.Skipped, 1)
	require.Equal(t, "articles/broken.md", result.Skipped[0].Path)
	require.Equal(t, before, vaultFileHashes(t, root))
	freshVault, err := NewVaultService(fresh.db, &config.Config{Vault: &config.VaultConfig{Root: root, HeaderDebounce: 20 * time.Millisecond}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, freshVault.Close()) })
	for _, original := range []*types.Entry{full, summary} {
		var restored types.Entry
		require.NoError(t, fresh.db.Where("id=?", original.ID).First(&restored).Error)
		require.Equal(t, original.Title, restored.Title)
		require.Equal(t, original.ContentType, restored.ContentType)
		require.True(t, restored.ContentTypeCorrected, "a header cannot reveal whether its type was corrected by the user")
		require.Equal(t, original.ProcessingMode, restored.ProcessingMode)
		require.Equal(t, original.StructuredSummary, restored.StructuredSummary)
		require.Equal(t, original.Description, restored.Description)
		var articleVectors int64
		require.NoError(t, fresh.db.Table("article_embeddings").Where("entry_id=?", restored.ID).Count(&articleVectors).Error)
		require.EqualValues(t, 1, articleVectors, "both full and summary-only articles restore their summary vector")
		require.Equal(t, original.Starred, restored.Starred)
		require.Equal(t, original.CollectReason, restored.CollectReason)
		require.Equal(t, original.VaultPath, restored.VaultPath)
		require.Equal(t, original.VaultBody, restored.VaultBody)
		require.Equal(t, original.VaultOriginalPath, restored.VaultOriginalPath)
		require.NotEmpty(t, restored.NormalizedURL)
		if restored.VaultOriginalPath != "" {
			require.Equal(t, "file", restored.Type)
			require.Equal(t, original.FileType, restored.FileType)
			require.Equal(t, strings.TrimPrefix(filepath.Base(restored.VaultOriginalPath), "original-"), restored.FileName)
		}
		if restored.ProcessingMode == types.ProcessingModeFull {
			require.NotEmpty(t, restored.ContentHash)
			require.Positive(t, restored.ContentRuneCount)
		}
		originalSource, err := original.IntakeMetadata()
		require.NoError(t, err)
		restoredSource, err := restored.IntakeMetadata()
		require.NoError(t, err)
		require.Equal(t, originalSource.SourceInfo, restoredSource.SourceInfo)
		var chunks int64
		require.NoError(t, fresh.db.Model(&types.Chunk{}).Where("entry_id=?", restored.ID).Count(&chunks).Error)
		if restored.ProcessingMode == types.ProcessingModeSummaryOnly {
			require.Zero(t, chunks)
		} else {
			require.Positive(t, chunks)
		}
		require.NoError(t, freshVault.RefreshHeader(fresh.ctx, restored.ID))
		require.Equal(t, before, vaultFileHashes(t, root), "refreshing a rebuilt header preserves every file byte")
	}
	var topics []string
	require.NoError(t, fresh.db.Table("topics").Joins("JOIN entry_topics links ON links.topic_id=topics.id").Where("links.entry_id=?", full.ID).Pluck("topics.name", &topics).Error)
	require.Equal(t, []string{topic.Name}, topics)
	var restoredTopicLinks []types.EntryTopic
	require.NoError(t, fresh.db.Where("entry_id=?", full.ID).Find(&restoredTopicLinks).Error)
	require.Len(t, restoredTopicLinks, 1)
	require.Equal(t, types.TopicSourceUser, restoredTopicLinks[0].Source, "recovery conservatively preserves every topic selection")
	require.NoError(t, NewTopicService(fresh.db).ReplaceEntryTopics(fresh.ctx, full.ID, nil))
	var restoredTopicCount int64
	require.NoError(t, fresh.db.Model(&types.EntryTopic{}).Where("entry_id=?", full.ID).Count(&restoredTopicCount).Error)
	require.EqualValues(t, 1, restoredTopicCount, "later model output cannot erase recovered topics")
	var entities []string
	require.NoError(t, fresh.db.Table("entities").Joins("JOIN entry_entities links ON links.entity_id=entities.id").Where("links.entry_id=?", full.ID).Pluck("entities.canonical_name", &entities).Error)
	require.Equal(t, []string{"研究实体"}, entities)
	for _, query := range []struct{ id, term string }{{full.ID, "rebuildfixture"}, {summary.ID, "Original"}} {
		hits, err := r.Retrieve(fresh.ctx, []types.RetrieveParams{{RetrieverType: types.KeywordsRetrieverType, KnowledgeBaseIDs: []string{fresh.kb.ID}, EntryIDs: []string{query.id}, Query: query.term, TopK: 10}})
		require.NoError(t, err)
		require.NotEmpty(t, hits)
	}
	var restoredReport types.Entry
	require.NoError(t, fresh.db.Where("vault_path=?", report.VaultPath).First(&restoredReport).Error)
	require.Contains(t, string(restoredReport.Metadata), full.ID)
	_, err = rebuild.Rebuild(fresh.ctx)
	require.ErrorContains(t, err, "empty entry database")
}

func TestVaultRebuildKeywordOnlyNeedsNoModelAndSkipsInvalidArticles(t *testing.T) {
	h := newVaultDocumentHarness(t, time.Millisecond)
	h.kb.ModelTiers = types.ModelTierConfig{}
	h.kb.EmbeddingModelID = ""
	h.kb.IndexingStrategy = types.IndexingStrategy{KeywordEnabled: true}
	require.NoError(t, h.db.Save(h.kb).Error)
	h.documents.models = nil
	now := time.Now().UTC().Format(time.RFC3339Nano)
	valid := VaultArticleHeader{ID: uuid.NewString(), Title: "无模型重建", CollectedAt: now, ContentType: "event"}
	body := append(RenderVaultArticleHeader(valid), []byte("# 无模型重建\n\nkeywordfixture survives without embedding setup")...)
	h.write(t, "articles/2026-10/valid.md", string(body))
	h.write(t, "articles/2026-10/zz-duplicate.md", string(body))
	for index, invalid := range []VaultArticleHeader{
		{ID: "not-uuid", Title: "Bad ID", CollectedAt: now},
		{ID: uuid.NewString(), Title: "Bad links", CollectedAt: now, Entities: []string{"plain name"}},
		{ID: uuid.NewString(), Title: "Bad date", CollectedAt: "yesterday"},
		{ID: uuid.NewString(), Title: "Bad type", CollectedAt: now, ContentType: "unknown"},
	} {
		h.write(t, fmt.Sprintf("articles/2026-10/bad-%d.md", index), string(RenderVaultArticleHeader(invalid)))
	}
	h.write(t, "mine/first.md", "# 第一手\n\nindependentfixture personal note")
	h.write(t, "mine/bad-utf8.md", string([]byte{0xff, 0xfe}))
	h.write(t, "reports/bad-utf8.md", string([]byte{0xff, 0xfe}))
	for relative, data := range map[string]string{
		".obsidian/app.json": `{"readingView":true}`, ".trash/x.md": "discarded article without a header", ".DS_Store": "Finder metadata",
		"mine/.trash/x.md": string([]byte{0xff}), "reports/.obsidian/template.md": "Editor template",
		"articles/.trash/x.md": string(body),
	} {
		h.write(t, relative, data)
	}
	before := vaultFileHashes(t, h.root)
	result, err := h.documents.Rebuild(h.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Articles)
	require.Len(t, result.Skipped, 7)
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
	h.write(t, "mine/first.md", "# First\n\nsymlinkfixture retained index")
	require.NoError(t, h.documents.Scan(h.ctx, true))
	document := h.byPath(t, "mine/first.md")
	external := t.TempDir()
	require.NoError(t, os.RemoveAll(filepath.Join(h.root, "mine")))
	require.NoError(t, os.Symlink(external, filepath.Join(h.root, "mine")))
	require.ErrorContains(t, h.documents.Scan(h.ctx, true), "symbolic link")
	var saved types.Entry
	require.NoError(t, h.db.Where("id=?", document.ID).First(&saved).Error)
	require.NoError(t, os.RemoveAll(filepath.Join(h.root, "reports")))
	require.NoError(t, os.Symlink(external, filepath.Join(h.root, "reports")))
	_, err := h.documents.CreateReport(h.ctx, "outside", "report body", nil)
	require.ErrorContains(t, err, "symbolic link")
	entries, err := os.ReadDir(external)
	require.NoError(t, err)
	require.Empty(t, entries)
}
