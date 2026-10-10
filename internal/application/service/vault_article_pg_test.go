package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/config"
	"reliquary/internal/types"
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

func readVaultArticle(t *testing.T, v *VaultService, k *types.Knowledge) ([]byte, []byte, []byte) {
	t.Helper()
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

func TestVaultArticleFullWritesOrderedHeaderRelativeImagesAndOriginal(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	topic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{Name: "中文主题", Description: "发布新闻"})
	require.NoError(t, err)
	h.processor.output = articleProcessingOutput(t, "research", []types.EntityCandidate{{Name: "规范名", EntityType: "company"}, {Name: "Alpha", EntityType: "product"}}, []string{topic.ID})
	sourceBody := "# 中文发布\n\n测量结果包含非 ASCII 内容。\n\n![图](" + articleImageDataURI(t, 22) + ")\n"
	k := h.upload(t, "中文标题", sourceBody)
	require.Equal(t, filepath.ToSlash(filepath.Join("articles", "2026-10", vaultSlug(k.Title)+"-"+k.ID[:8]+".md")), k.VaultPath)
	require.Equal(t, "article", k.SourceKind)
	data, header, body := readVaultArticle(t, v, k)
	fields := []string{"id", "url", "title", "publisher", "author", "published_at", "collected_at", "content_type", "summary", "entities", "topics", "starred", "collect_reason"}
	var keys []string
	for _, line := range strings.Split(string(header), "\n") {
		if line != "" && !strings.HasPrefix(line, " ") && strings.Contains(line, ":") {
			keys = append(keys, strings.SplitN(line, ":", 2)[0])
		}
	}
	require.Equal(t, fields, keys)
	require.Contains(t, string(header), "author: null\n")
	require.Contains(t, string(header), "collect_reason: null\n")
	require.Contains(t, string(header), "  - \"[[Alpha]]\"\n  - \"[[规范名]]\"")
	require.Contains(t, string(header), "  key_terms:\n")
	require.Contains(t, string(body), "../../attachments/"+k.ID+"/image-")
	require.NotContains(t, string(body), "local://")
	require.NotContains(t, string(body), "data:image/")
	require.Equal(t, string(body), k.VaultBody)
	parsed, parsedBody, err := ParseVaultArticle(data)
	require.NoError(t, err)
	require.Equal(t, k.StructuredSummary, parsed.Summary)
	require.Equal(t, body, parsedBody)
	original, err := os.ReadFile(filepath.Join(v.root, filepath.FromSlash(k.VaultOriginalPath)))
	require.NoError(t, err)
	require.Equal(t, sourceBody, string(original))
	attachments, err := os.ReadDir(filepath.Join(v.root, "attachments", k.ID))
	require.NoError(t, err)
	require.Len(t, attachments, 2)
	image, err := v.ReadImage(k, articleImagesInOrder(string(body), nil)[0])
	require.NoError(t, err)
	imageData, err := io.ReadAll(image)
	require.NoError(t, err)
	require.NoError(t, image.Close())
	require.True(t, bytes.HasPrefix(imageData, []byte("\x89PNG")))
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	require.Empty(t, intake.OriginalPath)
	require.NotContains(t, string(k.Metadata), "processed_body_path")
	down, name, err := h.service.GetKnowledgeFile(h.ctx, k.ID)
	require.NoError(t, err)
	require.Equal(t, k.FileName, name)
	download, err := io.ReadAll(down)
	require.NoError(t, err)
	require.NoError(t, down.Close())
	require.Equal(t, sourceBody, string(download))
}

func TestVaultArticleSummaryOnlyWritesHeaderWithoutAttachments(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	h.processor.output = validProcessingFixture("tutorial")
	k := h.upload(t, "仅摘要", "# 仅摘要\n\nGuide instructions contain original steps and supported deployment instructions.\n\n![图]("+articleImageDataURI(t, 40)+")")
	data, header, body := readVaultArticle(t, v, k)
	require.Equal(t, header, data)
	require.Empty(t, body)
	require.Empty(t, k.VaultBody)
	require.Empty(t, k.VaultOriginalPath)
	_, err := os.Stat(filepath.Join(v.root, "attachments", k.ID))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Contains(t, string(header), "content_type: \"tutorial\"")
}

func TestVaultArticleCopiesOriginalPDFAndPreservesImageDestinationSyntax(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := &types.Knowledge{ID: uuid.NewString(), TenantID: h.kb.TenantID, KnowledgeBaseID: h.kb.ID,
		Title: "PDF 研究", SourceKind: "article", Type: "file", FileType: "pdf", FileName: "原始 文档.pdf", FilePath: "local://fixture/original.pdf",
		ParseStatus: types.ParseStatusCompleted, ProcessingMode: "full", ContentType: "research", CreatedAt: time.Now().UTC()}
	require.NoError(t, h.db.Create(k).Error)
	pdf := []byte("%PDF-1.7\nOriginal PDF source bytes\n%%EOF")
	imageData, err := readVaultImage(h.ctx, articleImageDataURI(t, 25), nil)
	require.NoError(t, err)
	read := func(_ context.Context, ref string) (io.ReadCloser, error) {
		if ref == k.FilePath {
			return io.NopCloser(bytes.NewReader(pdf)), nil
		}
		require.Equal(t, "local://fixture/chart.png", ref)
		return io.NopCloser(bytes.NewReader(imageData)), nil
	}
	body := "# PDF 研究\n\n![中文](<local://fixture/chart.png> \"说明\")\n\n<img src='local://fixture/chart.png' alt='图'/>\n"
	require.NoError(t, v.WriteArticle(h.ctx, k, body, read))
	_, _, stored := readVaultArticle(t, v, k)
	require.NotContains(t, string(stored), "local://")
	require.Contains(t, string(stored), "![中文](<../../attachments/")
	require.Contains(t, string(stored), "> \"说明\")")
	require.Contains(t, string(stored), "<img src='../../attachments/")
	actual, err := os.ReadFile(filepath.Join(v.root, k.VaultOriginalPath))
	require.NoError(t, err)
	require.Equal(t, pdf, actual)
	require.True(t, strings.HasSuffix(k.VaultOriginalPath, "original-原始-文档.pdf"))
}

func TestVaultArticleOriginalAttachmentPreservesLongUnicodeFilenameExtension(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := &types.Knowledge{ID: uuid.NewString(), TenantID: h.kb.TenantID, KnowledgeBaseID: h.kb.ID,
		Title: "附件命名", SourceKind: "article", Type: "file", FileType: "pdf", FileName: strings.Repeat("中文文件", 30) + ".PDF", FilePath: "local://fixture/long-original.pdf",
		ParseStatus: types.ParseStatusCompleted, ProcessingMode: "full", ContentType: "research", CreatedAt: time.Now().UTC()}
	require.NoError(t, h.db.Create(k).Error)
	pdf := []byte("%PDF-1.7\nLong filename source\n%%EOF")
	read := func(_ context.Context, ref string) (io.ReadCloser, error) {
		require.Equal(t, k.FilePath, ref)
		return io.NopCloser(bytes.NewReader(pdf)), nil
	}
	body := "# 附件命名\n\n正文原样保留。\n\n---\n最后一行\n"
	require.NoError(t, v.WriteArticle(h.ctx, k, body, read))
	name := filepath.Base(k.VaultOriginalPath)
	require.True(t, strings.HasSuffix(name, ".pdf"))
	require.LessOrEqual(t, len([]byte(name)), 255)
	require.Equal(t, "pdf", getFileType(name))
	actual, err := os.ReadFile(filepath.Join(v.root, k.VaultOriginalPath))
	require.NoError(t, err)
	require.Equal(t, pdf, actual)
	_, _, savedBody := readVaultArticle(t, v, k)
	require.Equal(t, body, string(savedBody))
	require.Equal(t, "articles/"+k.CreatedAt.UTC().Format("2006-01")+"/附件命名-"+k.ID[:8]+".md", k.VaultPath)
	// Four-byte Unicode and a long supported extension still fit NAME_MAX.
	unicodeName := vaultOriginalFileName(strings.Repeat("𠀀", 100) + ".markdown")
	require.LessOrEqual(t, len([]byte(unicodeName)), 255)
	require.True(t, utf8.ValidString(unicodeName))
	require.True(t, strings.HasSuffix(unicodeName, ".markdown"))
}

func TestVaultArticleNamingFallsBackToCollectedMonthAndSanitizesTitle(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := &types.Knowledge{ID: uuid.NewString(), TenantID: h.kb.TenantID, KnowledgeBaseID: h.kb.ID, Title: "中文 / 标题:*?\"<>|", SourceKind: "article", Type: "url", Source: "https://example.org/date-less", ParseStatus: types.ParseStatusCompleted, ProcessingMode: "full", ContentType: "event", CreatedAt: time.Date(2025, 9, 10, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, h.db.Create(k).Error)
	require.NoError(t, v.WriteArticle(h.ctx, k, "正文", nil))
	require.Equal(t, "articles/2025-09/中文-标题-"+k.ID[:8]+".md", k.VaultPath)
	_, header, _ := readVaultArticle(t, v, k)
	require.Contains(t, string(header), "published_at: null\n")
	require.LessOrEqual(t, len([]rune(vaultSlug(strings.Repeat("中", 200)))), 80)
	require.Equal(t, "untitled", vaultSlug("../:*?<>|"))
}

func TestVaultArticleHeaderDeterminismAndUnchangedModificationTime(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "确定性", "# Header\n\nDeterministic header has a stable source body with measured values.")
	header, err := v.articleHeader(h.ctx, k)
	require.NoError(t, err)
	header.Author = header.Publisher
	header.Entities = []string{"[[乙]]", "[[甲]]", "[[乙]]"}
	header.Topics = []string{"主题 B", "主题 A"}
	require.Equal(t, RenderVaultArticleHeader(header), RenderVaultArticleHeader(header))
	require.NotContains(t, string(RenderVaultArticleHeader(header)), "\nauthor:")
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
	k := &types.Knowledge{ID: uuid.NewString(), TenantID: h.kb.TenantID, KnowledgeBaseID: h.kb.ID,
		Title: "日期精度", SourceKind: "article", Type: "url", ParseStatus: types.ParseStatusCompleted,
		ProcessingMode: "full", ContentType: "event", CreatedAt: time.Date(2026, 10, 11, 1, 2, 3, 123456789, time.UTC)}
	require.NoError(t, h.db.Create(k).Error)
	require.NoError(t, v.WriteArticle(h.ctx, k, "不可改动正文", nil))
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

func TestVaultArticleDebounceWritesOnceAndCloseCancelsQueuedTasks(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "去抖", "# Debounce\n\nThe trailing timer coalesces changes without modifying the source body.")
	before := vaultWriteCount(v)
	require.NoError(t, h.db.Model(k).Update("collect_reason", "最后一次判断").Error)
	for i := 0; i < 20; i++ {
		v.ScheduleRefresh(h.ctx, k.ID)
	}
	require.Eventually(t, func() bool { return vaultWriteCount(v) == before+1 }, 3*time.Second, 10*time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	require.Equal(t, before+1, vaultWriteCount(v))
	require.NoError(t, h.db.Model(k).Update("starred", true).Error)
	v.ScheduleRefresh(h.ctx, k.ID)
	require.NoError(t, v.Close())
	time.Sleep(60 * time.Millisecond)
	require.Equal(t, before+1, vaultWriteCount(v))
}

func TestVaultEntityMergeAndRenameRefreshEveryRelatedArticle(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	h.processor.output = articleProcessingOutput(t, "event", []types.EntityCandidate{{Name: "旧公司", EntityType: "company"}}, []string{})
	first := h.upload(t, "关联一", "# Article one\n\nThe company introduced the first widget with detailed specifications.")
	h.processor.output = articleProcessingOutput(t, "event", []types.EntityCandidate{{Name: "新公司", EntityType: "company"}}, []string{})
	second := h.upload(t, "关联二", "# Article two\n\nThe second organization published distinct measurements for another product.")
	var oldEntity, keep types.Entity
	require.NoError(t, h.db.Where("canonical_name=?", "旧公司").First(&oldEntity).Error)
	require.NoError(t, h.db.Where("canonical_name=?", "新公司").First(&keep).Error)
	entities := NewEntityService(h.db, h.service.modelService)
	entities.SetHeaderRefresher(v)
	require.NoError(t, entities.MergeEntities(h.ctx, keep.ID, oldEntity.ID))
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(v.root, first.VaultPath))
		return err == nil && strings.Contains(string(data), "[[新公司]]") && !strings.Contains(string(data), "[[旧公司]]")
	}, 3*time.Second, 10*time.Millisecond)
	_, err := entities.RenameEntity(h.ctx, keep.ID, "最终公司")
	require.NoError(t, err)
	for _, k := range []*types.Knowledge{first, second} {
		require.Eventually(t, func() bool {
			data, err := os.ReadFile(filepath.Join(v.root, k.VaultPath))
			return err == nil && strings.Contains(string(data), "[[最终公司]]") && !strings.Contains(string(data), "[[新公司]]")
		}, 3*time.Second, 10*time.Millisecond)
	}
}

func TestVaultExternallyDeletedArticleRebuildsCompleteSavedBody(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "删除恢复", "# Rebuild\n\nOriginal body contains a source chart.\n\n![image]("+articleImageDataURI(t, 9)+")")
	before, _, _ := readVaultArticle(t, v, k)
	require.NoError(t, os.Remove(filepath.Join(v.root, k.VaultPath)))
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	after, _, _ := readVaultArticle(t, v, k)
	require.Equal(t, before, after)
	h.processor.output = validProcessingFixture("reference")
	summary := h.upload(t, "摘要恢复", "# Reference\n\nReference source explains configuration options and defaults.")
	before, _, _ = readVaultArticle(t, v, summary)
	require.NoError(t, os.Remove(filepath.Join(v.root, summary.VaultPath)))
	require.NoError(t, v.RefreshHeader(h.ctx, summary.ID))
	after, header, body := readVaultArticle(t, v, summary)
	require.Equal(t, before, after)
	require.Equal(t, header, after)
	require.Empty(t, body)
}

func TestVaultAcceptedEntityMergeSuggestionRefreshesHeader(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	h.processor.output = articleProcessingOutput(t, "event", []types.EntityCandidate{{Name: "待合并实体", EntityType: "company"}}, []string{})
	k := h.upload(t, "接受合并", "# Suggested merge\n\nThe organization published original product specifications and measured results.")
	var old types.Entity
	require.NoError(t, h.db.Where("canonical_name=?", "待合并实体").First(&old).Error)
	keep := entityRecord(uuid.NewString(), "规范实体", "company")
	require.NoError(t, h.db.Create(&keep).Error)
	suggestion := &types.EntityMergeSuggestion{ID: uuid.NewString(), TenantID: h.kb.TenantID, NewEntityID: old.ID, CandidateEntityID: keep.ID, KnowledgeID: k.ID, Reason: "同一主体", Status: "pending", CreatedAt: time.Now()}
	require.NoError(t, h.db.Create(suggestion).Error)
	entities := NewEntityService(h.db, h.service.modelService)
	entities.SetHeaderRefresher(v)
	_, err := entities.DecideMergeSuggestion(h.ctx, suggestion.ID, true)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(v.root, k.VaultPath))
		return err == nil && strings.Contains(string(data), "[[规范实体]]") && !strings.Contains(string(data), "[[待合并实体]]")
	}, 3*time.Second, 10*time.Millisecond)
}

func TestVaultDeleteArticleRemovesFileAndAttachments(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "删除文章", "# Delete\n\nDelete the article file and its isolated attachment directory.\n\n![image]("+articleImageDataURI(t, 5)+")")
	require.NoError(t, h.service.DeleteKnowledge(h.ctx, k.ID))
	_, err := os.Stat(filepath.Join(v.root, k.VaultPath))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(v.root, "attachments", k.ID))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestVaultReprocessingUpdatesHeaderAndReadsVaultBody(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "重新处理", "# Reprocess\n\nMeasured product capabilities remain in the immutable article body.\n\n![chart]("+articleImageDataURI(t, 6)+")")
	_, _, beforeBody := readVaultArticle(t, v, k)
	h.processor.output = articleProcessingOutput(t, "opinion", []types.EntityCandidate{}, []string{})
	var metadata map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(k.Metadata, &metadata))
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	// Model processing consumes the vault body when manual reprocessing is used.
	k.Type = types.KnowledgeTypeManual
	metadata["intake"], _ = json.Marshal(intake)
	metadata["format"], _ = json.Marshal("markdown")
	metadata["status"], _ = json.Marshal("publish")
	k.Metadata, err = json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, h.service.repo.UpdateKnowledge(h.ctx, k))
	_, err = h.service.ReparseKnowledge(h.ctx, k.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(v.root, k.VaultPath))
		return err == nil && strings.Contains(string(data), "content_type: \"opinion\"")
	}, 3*time.Second, 10*time.Millisecond)
	_, _, afterBody := readVaultArticle(t, v, k)
	require.Equal(t, beforeBody, afterBody)
	reader, _, err := h.service.GetKnowledgeBody(h.ctx, k.ID)
	require.NoError(t, err)
	served, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, beforeBody, served)
}

func TestVaultDocumentReadBodyIncludesUserFrontMatterAndDeleteKeepsFile(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	doc := &types.Knowledge{ID: uuid.NewString(), TenantID: h.kb.TenantID, KnowledgeBaseID: h.kb.ID,
		SourceKind: "mine", VaultPath: "mine/user.md", Title: "User", Type: types.KnowledgeTypeManual, ParseStatus: types.ParseStatusCompleted}
	require.NoError(t, h.db.Create(doc).Error)
	data := []byte("---\ntitle: User\n---\n# User\n\nBody")
	require.NoError(t, os.WriteFile(filepath.Join(v.root, doc.VaultPath), data, 0644))
	body, err := v.ReadBody(doc)
	require.NoError(t, err)
	require.Equal(t, data, body)
	require.NoError(t, v.DeleteArticle(context.Background(), doc))
	actual, err := os.ReadFile(filepath.Join(v.root, doc.VaultPath))
	require.NoError(t, err)
	require.Equal(t, data, actual)
	_, err = h.service.ReparseKnowledge(h.ctx, doc.ID, nil)
	require.ErrorContains(t, err, "indexed directly from their files")
	_, err = h.service.UpdateManualKnowledge(h.ctx, doc.ID, &types.ManualKnowledgePayload{Title: "User", Content: "修改", Status: "publish"})
	require.ErrorContains(t, err, "edited directly in their files")
	require.Empty(t, h.queue.tasks)
	require.Empty(t, h.processor.requests)
	var stored types.Knowledge
	require.NoError(t, h.db.Where("id=?", doc.ID).First(&stored).Error)
	require.Equal(t, types.ParseStatusCompleted, stored.ParseStatus)
}

func TestVaultPersistedArticleBodyCannotBeEditedAfterProcessingFailure(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "已写正文", "# Saved body\n\nThe approved body remains fixed after later processing attempts fail.")
	k.Type, k.ParseStatus = types.KnowledgeTypeManual, types.ParseStatusFailed
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	intake.Decision = "error"
	require.NoError(t, k.SetIntakeMetadata(intake))
	require.NoError(t, h.service.repo.UpdateKnowledge(h.ctx, k))
	before, _, _ := readVaultArticle(t, v, k)
	tasks, calls := len(h.queue.tasks), len(h.processor.requests)
	_, err = h.service.UpdateManualKnowledge(h.ctx, k.ID, &types.ManualKnowledgePayload{Title: "替换标题", Content: "替换正文", Status: "publish"})
	require.ErrorContains(t, err, "approved article body cannot be changed")
	after, _, _ := readVaultArticle(t, v, k)
	require.Equal(t, before, after)
	require.Len(t, h.queue.tasks, tasks)
	require.Len(t, h.processor.requests, calls)
	stored := h.article(t, k.ID)
	require.Equal(t, k.Title, stored.Title)
	require.JSONEq(t, string(k.Metadata), string(stored.Metadata))
	require.Equal(t, k.VaultBody, stored.VaultBody)
}
