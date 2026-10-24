package service

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reliquary/internal/config"
	"reliquary/internal/maintenance"
	"reliquary/internal/types"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newVaultArticleHarness(t *testing.T) (*articleProcessingHarness, *VaultService) {
	t.Helper()
	h := newArticleProcessingHarness(t, true)
	v, err := NewVaultService(h.db, &config.Config{Vault: &config.VaultConfig{Root: t.TempDir(), HeaderDebounce: 35 * time.Millisecond}})
	require.NoError(t, err)
	h.service.SetVault(v)
	t.Cleanup(func() { require.NoError(t, v.Close()) })
	return h, v
}

func readVaultArticle(t *testing.T, v *VaultService, k *types.Entry) ([]byte, []byte, []byte) {
	t.Helper()
	path := filepath.Join(v.root, filepath.FromSlash(k.VaultPath))
	if _, err := os.Stat(path); os.IsNotExist(err) {
		require.NoError(t, v.RefreshHeader(context.Background(), k.ID))
	}
	data, err := os.ReadFile(filepath.Join(v.root, filepath.FromSlash(k.VaultPath)))
	require.NoError(t, err)
	header, body, err := SplitVaultArticle(data)
	require.NoError(t, err)
	return data, header, body
}

func vaultWriteCount(v *VaultService) int {
	v.articles.mu.Lock()
	defer v.articles.mu.Unlock()
	return v.articles.writeCount
}

func TestVaultArticleNamingUsesLibraryAndSanitizesTitle(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := &types.Entry{
		ID:          uuid.NewString(),
		SpaceID:     h.library.SpaceID,
		LibraryID:   h.library.ID,
		Title:       "中文 / 标题:*?\"<>|",
		SourceKind:  "article",
		Type:        "url",
		Source:      "https://example.org/date-less",
		ParseStatus: types.ParseStatusCompleted,
		CreatedAt:   time.Date(2025, 9, 10, 0, 0, 0, 0, time.UTC),
	}
	require.NoError(t, h.db.Create(k).Error)
	require.NoError(t, v.WriteArticle(h.ctx, k, "正文", nil))
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	require.Equal(t, h.library.Directory+"/中文-标题-"+k.ID[:8]+".md", k.VaultPath)
	_, header, _ := readVaultArticle(t, v, k)
	require.NotContains(t, string(header), "published_at:")
	require.LessOrEqual(t, len([]rune(vaultSlug(strings.Repeat("中", 200)))), 80)
	require.Equal(t, "untitled", vaultSlug("../:*?<>|"))
}

func TestVaultArticleHeaderDeterminismAndUnchangedModificationTime(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "确定性", "# Header\n\nDeterministic header has a stable source body with measured values.")
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	header, err := v.articleHeader(h.ctx, k)
	require.NoError(t, err)
	header.Author = header.Publisher
	header.Entities = []string{"[[乙]]", "[[甲]]", "[[乙]]"}
	require.Equal(t, RenderVaultArticleHeader(header), RenderVaultArticleHeader(header))
	require.Contains(t, string(RenderVaultArticleHeader(header)), "\nauthor:")
	path := filepath.Join(v.root, filepath.FromSlash(k.VaultPath))
	fixed := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, os.Chtimes(path, fixed, fixed))
	before := vaultWriteCount(v)
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, fixed, info.ModTime().UTC())
	require.Equal(t, before, vaultWriteCount(v))
}

func TestVaultInitialHeaderUsesPersistedPostgresTimestampPrecision(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := &types.Entry{
		ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID,
		Title: "日期精度", SourceKind: "article", Type: "url", ParseStatus: types.ParseStatusCompleted,
		CreatedAt: time.Date(2026, 10, 11, 1, 2, 3, 123456789, time.UTC),
	}
	require.NoError(t, h.db.Create(k).Error)
	require.NoError(t, v.WriteArticle(h.ctx, k, "不可改动正文", nil))
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	path := filepath.Join(v.root, k.VaultPath)
	fixed := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, os.Chtimes(path, fixed, fixed))
	before := vaultWriteCount(v)
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	require.Equal(t, before, vaultWriteCount(v))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, fixed, info.ModTime().UTC())
}

func TestVaultArticleAuthorRoundTripDistinguishesOmissionAndNull(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "作者字段", "# Source author\n\nOriginal publication has a stable publisher and independently tracked author.")
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	header, err := v.articleHeader(h.ctx, k)
	require.NoError(t, err)
	for _, author := range []string{header.Publisher, "", "另一作者"} {
		header.Author = author
		original := RenderVaultArticleHeader(header)
		parsed, body, err := ParseVaultArticle(original)
		require.NoError(t, err)
		require.Empty(t, body)
		require.Equal(t, author, parsed.Author)
		require.Equal(t, original, RenderVaultArticleHeader(parsed))
	}
}

func TestVaultArticleHeaderRewritePreservesBodyAndRejectsHashMismatch(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "正文校验", "# 原文\n\n---\n\nyaml: \"样式文字\"\n  nested: 中文é\n\n正文最后一行\n")
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	before, _, body := readVaultArticle(t, v, k)
	require.NoError(t, h.db.Model(k).Update("collect_reason", "保留测量结论").Error)
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	after, header, newBody := readVaultArticle(t, v, k)
	require.Equal(t, body, newBody)
	require.NotEqual(t, before, after)
	require.Contains(t, string(header), "保留测量结论")
	require.NoError(t, h.db.Model(k).Update("starred", true).Error)
	v.articles.beforeTempWrite = func(data []byte) []byte { return append(data, []byte("人为修改正文")...) }
	err := v.RefreshHeader(h.ctx, k.ID)
	require.ErrorContains(t, err, "body hash changed")
	refused, _, _ := readVaultArticle(t, v, k)
	require.Equal(t, after, refused)
	require.Contains(t, v.ArticleError(), "body hash changed")
	files, err := filepath.Glob(filepath.Join(filepath.Dir(filepath.Join(v.root, k.VaultPath)), ".vault-*.tmp"))
	require.NoError(t, err)
	require.Empty(t, files)
	v.articles.beforeTempWrite = nil
}

func TestDatabaseArticlePreviewDoesNotPersistMarkdown(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "按需预览", "# 正文\n\nThis material stays in PostgreSQL and can be previewed without a Markdown file.")
	path := filepath.Join(v.root, k.VaultPath)
	require.NoFileExists(t, path)
	body, err := v.ReadBody(k)
	require.NoError(t, err)
	require.Equal(t, k.VaultBody, string(body))
	reader, _, err := h.service.GetEntryBody(h.ctx, k.ID)
	require.NoError(t, err)
	preview, err := io.ReadAll(reader)
	reader.Close()
	require.NoError(t, err)
	require.Equal(t, body, preview)
	verified, err := v.VerifyRuntime(h.ctx)
	require.NoError(t, err)
	require.True(t, verified.OK, verified.Differences)
	v.ScheduleRefresh(h.ctx, k.ID)
	time.Sleep(100 * time.Millisecond)
	require.NoFileExists(t, path)
	manifest, err := maintenance.BuildRuntimeManifest(h.ctx, h.db, v.root, "")
	require.NoError(t, err)
	require.True(t, manifest.DatabaseOnly)
	require.NotEmpty(t, manifest.BodyHashes[k.ID])
	require.Empty(t, manifest.Files["vault"])
	require.NoError(t, h.db.Model(k).Update("vault_body", "").Error)
	changed, err := maintenance.BuildRuntimeManifest(h.ctx, h.db, v.root, "")
	require.NoError(t, err)
	require.Contains(t, maintenance.CompareManifest(manifest, changed), "document body: "+k.ID)
	verified, err = v.VerifyRuntime(h.ctx)
	require.NoError(t, err)
	require.False(t, verified.OK, "a backup must refuse file-only archived documents")
	require.NoError(t, h.db.Model(k).Update("vault_body", string(body)).Error)
	require.NoError(t, v.Close())
	require.NoFileExists(t, path)
}

func TestVaultEntityMergeAndRenameRefreshEveryRelatedArticle(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	h.processor.output = articleProcessingOutput(t, []types.EntityCandidate{{Name: "旧公司", EntityType: "company"}})
	first := h.upload(t, "关联一", "# Article one\n\nThe company introduced the first widget with detailed specifications.")
	h.processor.output = articleProcessingOutput(t, []types.EntityCandidate{{Name: "新公司", EntityType: "company"}})
	second := h.upload(t, "关联二", "# Article two\n\nThe second organization published distinct measurements for another product.")
	var oldEntity, keep types.Entity
	require.NoError(t, h.db.Where("canonical_name=?", "旧公司").First(&oldEntity).Error)
	require.NoError(t, h.db.Where("canonical_name=?", "新公司").First(&keep).Error)
	entities := NewEntityService(h.db, h.service.modelService)
	entities.SetHeaderRefresher(v)
	require.NoError(t, entities.MergeEntities(h.ctx, keep.ID, oldEntity.ID))
	require.NoError(t, v.RefreshHeader(h.ctx, first.ID))
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(v.root, first.VaultPath))
		return err == nil && strings.Contains(string(data), "[[新公司]]") && !strings.Contains(string(data), "[[旧公司]]")
	}, 3*time.Second, 10*time.Millisecond)
	_, err := entities.RenameEntity(h.ctx, keep.ID, "最终公司")
	require.NoError(t, err)
	for _, k := range []*types.Entry{first, second} {
		require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
		require.Eventually(t, func() bool {
			data, err := os.ReadFile(filepath.Join(v.root, k.VaultPath))
			return err == nil && strings.Contains(string(data), "[[最终公司]]") && !strings.Contains(string(data), "[[新公司]]")
		}, 3*time.Second, 10*time.Millisecond)
	}
}

func TestVaultExternallyDeletedArticleRebuildsCompleteSavedBody(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "删除恢复", "# Rebuild\n\nOriginal body contains a source chart.\n\n![image]("+articleImageDataURI(t, 9)+")")
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	before, _, _ := readVaultArticle(t, v, k)
	require.NoError(t, os.Remove(filepath.Join(v.root, k.VaultPath)))
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	after, _, _ := readVaultArticle(t, v, k)
	require.Equal(t, before, after)
	h.processor.output = validProcessingFixture()
	summary := h.upload(t, "摘要恢复", "# Reference\n\nReference source explains configuration options and defaults.")
	before, _, _ = readVaultArticle(t, v, summary)
	require.NoError(t, os.Remove(filepath.Join(v.root, summary.VaultPath)))
	require.NoError(t, v.RefreshHeader(h.ctx, summary.ID))
	after, _, body := readVaultArticle(t, v, summary)
	require.Equal(t, before, after)
	require.Contains(t, string(body), "Reference source explains configuration options and defaults.")
}

func TestVaultAcceptedEntityMergeSuggestionRefreshesHeader(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	h.processor.output = articleProcessingOutput(t, []types.EntityCandidate{{Name: "待合并实体", EntityType: "company"}})
	k := h.upload(t, "接受合并", "# Suggested merge\n\nThe organization published original product specifications and measured results.")
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	var old types.Entity
	require.NoError(t, h.db.Where("canonical_name=?", "待合并实体").First(&old).Error)
	keep := entityRecord(uuid.NewString(), "规范实体", "company")
	require.NoError(t, h.db.Create(&keep).Error)
	suggestion := &types.EntityMergeSuggestion{ID: uuid.NewString(), SpaceID: h.library.SpaceID, NewEntityID: old.ID, CandidateEntityID: keep.ID, EntryID: k.ID, Reason: "同一主体", Status: "pending", CreatedAt: time.Now()}
	require.NoError(t, h.db.Create(suggestion).Error)
	entities := NewEntityService(h.db, h.service.modelService)
	entities.SetHeaderRefresher(v)
	_, err := entities.DecideMergeSuggestion(h.ctx, suggestion.ID, true)
	require.NoError(t, err)
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(v.root, k.VaultPath))
		return err == nil && strings.Contains(string(data), "[[规范实体]]") && !strings.Contains(string(data), "[[待合并实体]]")
	}, 3*time.Second, 10*time.Millisecond)
}

func TestVaultDeleteArticleRemovesFileAndAttachments(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "删除文章", "# Delete\n\nDelete the article file and its isolated attachment directory.\n\n![image]("+articleImageDataURI(t, 5)+")")
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	require.NoError(t, h.service.DeleteEntry(h.ctx, k.ID))
	_, err := os.Stat(filepath.Join(v.root, k.VaultPath))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(v.root, "attachments", k.ID))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestVaultReprocessingUpdatesHeaderAndReadsVaultBody(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "重新处理", "# Reprocess\n\nMeasured product capabilities remain in the immutable article body.\n\n![chart]("+articleImageDataURI(t, 6)+")")
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	_, _, beforeBody := readVaultArticle(t, v, k)
	h.processor.output = articleProcessingOutput(t, []types.EntityCandidate{})
	var metadata map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(k.Metadata, &metadata))
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	// Model processing consumes the vault body when manual reprocessing is used.
	k.Type = types.EntryTypeManual
	metadata["intake"], _ = json.Marshal(intake)
	metadata["format"], _ = json.Marshal("markdown")
	metadata["status"], _ = json.Marshal("publish")
	k.Metadata, err = json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, k))
	_, err = h.service.ReparseEntry(h.ctx, k.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(v.root, k.VaultPath))
		return err == nil && strings.Contains(string(data), "Acme reports an original product announcement")
	}, 3*time.Second, 10*time.Millisecond)
	_, _, afterBody := readVaultArticle(t, v, k)
	require.Equal(t, beforeBody, afterBody)
	reader, _, err := h.service.GetEntryBody(h.ctx, k.ID)
	require.NoError(t, err)
	served, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, beforeBody, served)
}

func TestImportedVaultDocumentReadBodyExcludesHeaderAndDeleteRemovesFile(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	var mine types.Library
	require.NoError(t, h.db.Where("system_role=?", "personal").First(&mine).Error)
	doc := &types.Entry{
		ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: mine.ID,
		SourceKind:  "mine",
		VaultPath:   "personal/user.md",
		Title:       "User",
		Type:        types.EntryTypeManual,
		ParseStatus: types.ParseStatusCompleted,
	}
	require.NoError(t, h.db.Create(doc).Error)
	data := []byte("---\ntitle: User\n---\n# User\n\nBody")
	require.NoError(t, os.WriteFile(filepath.Join(v.root, doc.VaultPath), data, 0o644))
	body, err := v.ReadBody(doc)
	require.NoError(t, err)
	require.Equal(t, []byte("# User\n\nBody"), body)
	require.NoError(t, v.DeleteArticle(context.Background(), doc))
	_, err = os.Stat(filepath.Join(v.root, doc.VaultPath))
	require.True(t, os.IsNotExist(err))
	_, err = h.service.ReparseEntry(h.ctx, doc.ID, nil)
	require.True(t, os.IsNotExist(err), "a missing saved body still cannot be reprocessed")
	require.Empty(t, h.queue.tasks)
	require.Empty(t, h.processor.requests)
	var stored types.Entry
	require.NoError(t, h.db.Where("id=?", doc.ID).First(&stored).Error)
	require.Equal(t, types.ParseStatusCompleted, stored.ParseStatus)
}

func TestVaultPersistedArticleBodyCannotBeEditedAfterProcessingFailure(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "已写正文", "# Saved body\n\nThe approved body remains fixed after later processing attempts fail.")
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	k.Type, k.ParseStatus = types.EntryTypeManual, types.ParseStatusFailed
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	intake.Decision = "error"
	require.NoError(t, k.SetIntakeMetadata(intake))
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, k))
	before, _, _ := readVaultArticle(t, v, k)
	tasks, calls := len(h.queue.tasks), len(h.processor.requests)
	after, _, _ := readVaultArticle(t, v, k)
	require.Equal(t, before, after)
	require.Len(t, h.queue.tasks, tasks)
	require.Len(t, h.processor.requests, calls)
	stored := h.article(t, k.ID)
	require.Equal(t, k.Title, stored.Title)
	require.JSONEq(t, string(k.Metadata), string(stored.Metadata))
	require.Equal(t, k.VaultBody, stored.VaultBody)
}
