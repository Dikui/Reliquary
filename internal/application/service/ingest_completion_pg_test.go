package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

func ingestFixtureRequest(url, body string) *types.IngestRequest {
	return &types.IngestRequest{URL: url, Content: body, Acquisition: "verbatim", Publisher: "Acme", Title: "Original observations"}
}
func ingestMissingImageRequest(url, source string) *types.IngestRequest {
	return ingestFixtureRequest(url, "# Original observations\n\nAcme reports the exact measured pressure, the calibration method and instrument configuration.\n\n![measurement]("+source+")")
}

func TestIngestBatchStatesValidationLimitsAndIsolation(t *testing.T) {
	h, _ := newIngestHarness(t)
	full := *ingestFixtureRequest("https://example.org/batch-complete", "A complete account of the instrument calibration procedure and its original measurements.")
	missing := types.IngestRequest{URL: "https://example.org/batch-missing", Acquisition: "extracted", Missing: []types.IngestMissing{{Type: "content", SourceURL: "https://example.org/batch-missing", Reason: "登录后才能读取正文"}}}
	invalid := full
	invalid.URL = "https://example.org/invalid"
	invalid.Acquisition = ""
	result, err := h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{full, full, invalid, missing}})
	require.NoError(t, err)
	require.Len(t, result.Items, 4)
	statuses := []string{}
	for i, item := range result.Items {
		statuses = append(statuses, item.Status)
		require.Equal(t, i, item.Index)
		require.NotNil(t, item.Completeness)
	}
	require.Equal(t, []string{"accepted", "exists", "invalid", "pending_review"}, statuses)
	require.Equal(t, "正文缺失", result.Items[3].Reason)
	require.Contains(t, result.Items[3].Completeness[0].Reason, "登录")
	require.Len(t, h.processor.stages, 1, "missing content does not invoke review or processing")
	badMissing := *ingestFixtureRequest("https://example.org/unexplained", "")
	item, err := h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{badMissing}})
	require.NoError(t, err)
	require.Equal(t, "invalid", item.Items[0].Status)
	badImage := full
	badImage.URL = "https://example.org/unexplained-image"
	badImage.Images = []types.IngestImage{{SourceURL: "https://example.org/chart.png"}}
	item, err = h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{badImage}})
	require.NoError(t, err)
	require.Equal(t, "invalid", item.Items[0].Status)
	many := make([]types.IngestRequest, 50)
	result, err = h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: many})
	require.NoError(t, err)
	require.Len(t, result.Items, 50)
	_, err = h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: append(many, types.IngestRequest{})})
	require.Error(t, err)
	broken := full
	broken.URL = "https://example.org/broken"
	broken.Content = "An independent document whose review model deliberately encounters a temporary failure."
	h.service.modelService = articleProcessingModels{summaryStateModelService: &summaryStateModelService{model: &ingestSelectiveFailureModel{articleProcessingChat: h.processor}, metadata: &types.Model{ID: "local-fixture", SpaceID: h.kb.SpaceID}}}
	after := *ingestFixtureRequest("https://example.org/after-error", "The committee publishes its original housing statistics including regional totals and methodology.")
	result, err = h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{broken, after}})
	require.NoError(t, err)
	require.Equal(t, "invalid", result.Items[0].Status)
	require.Equal(t, "accepted", result.Items[1].Status)
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("source=?", broken.URL).Count(&count).Error)
	require.Zero(t, count, "an internal error rolls back only that item")
}

type ingestSelectiveFailureModel struct{ *articleProcessingChat }

func (m *ingestSelectiveFailureModel) Chat(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	for _, message := range messages {
		if strings.Contains(message.Content, "deliberately encounters") {
			return nil, fmt.Errorf("fixture model internal failure")
		}
	}
	return m.articleProcessingChat.Chat(ctx, messages, options)
}

func TestIngestCheckFourStatesRepostAndDiscardedRedelivery(t *testing.T) {
	h, _ := newIngestHarness(t)
	request := ingestFixtureRequest("https://example.org/canonical?utm_source=fixture", "The sourced report presents observations of an instrument and its tested performance.")
	accepted, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	request.URL = "https://repost.example.org/origin"
	duplicate, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "exists", duplicate.Status)
	pending, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/pending", Acquisition: "extracted", Missing: []types.IngestMissing{{Type: "content", Reason: "未获得权限"}}})
	require.NoError(t, err)
	discarded, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/discarded", Acquisition: "extracted", Missing: []types.IngestMissing{{Type: "content", Reason: "暂无正文"}}})
	require.NoError(t, err)
	require.NoError(t, h.service.DiscardIngest(h.ctx, discarded.EntryID, "用户不再保留"))
	checks, err := h.service.CheckURLs(h.ctx, []string{"https://EXAMPLE.org/CANONICAL/#anchor", "https://example.org/pending", "https://example.org/discarded", "https://example.org/not-found", "https://repost.example.org/origin"})
	require.NoError(t, err)
	require.Equal(t, []string{"in_library", "pending_review", "discarded", "not_found", "in_library"}, []string{checks[0].Status, checks[1].Status, checks[2].Status, checks[3].Status, checks[4].Status})
	require.Equal(t, accepted.EntryID, checks[0].EntryID)
	require.Equal(t, accepted.EntryID, checks[4].EntryID)
	require.Equal(t, pending.EntryID, checks[1].EntryID)
	redelivery, err := h.service.Ingest(h.ctx, ingestFixtureRequest("https://example.org/discarded", "Complete content supplied after the user discarded its original link."))
	require.NoError(t, err)
	require.Equal(t, "discarded", redelivery.Status)
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("source=?", "https://example.org/discarded").Count(&count).Error)
	require.Zero(t, count)
}

func TestIngestImageOnlySupplementPreservesTextReprocessesAndUpdatesHeader(t *testing.T) {
	h, v := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	source := "https://example.org/figure.png"
	request := ingestMissingImageRequest("https://example.org/completion", source)
	accepted, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "accepted", accepted.Status)
	h.drain(t)
	before := h.article(t, accepted.EntryID)
	_, _, oldBody := readVaultArticle(t, v, before)
	require.True(t, before.Incomplete)
	require.Len(t, h.processor.requests, 1)
	h.processor.output = strings.Replace(validProcessingFixture("research"), "Acme released", "Acme measured", 1)
	supplement, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: request.URL, Acquisition: "extracted", Images: []types.IngestImage{{SourceURL: source, Data: articleImageDataURI(t, 71)}}})
	require.NoError(t, err)
	require.Equal(t, "supplemented", supplement.Status)
	require.Empty(t, supplement.Completeness)
	h.drain(t)
	after := h.article(t, before.ID)
	require.False(t, after.Incomplete)
	require.Len(t, h.processor.requests, 2)
	require.Equal(t, "research", after.ContentType)
	waitCorrectionHeader(t, v, after, "research")
	_, header, newBody := readVaultArticle(t, v, after)
	require.Contains(t, string(header), "content_type: \"research\"")
	refs := articleImagesInOrder(string(newBody), nil)
	require.Len(t, refs, 1)
	require.Contains(t, refs[0], "../../attachments/")
	require.Equal(t, replaceIngestImageTargets(string(oldBody), map[string]string{source: refs[0]}), string(newBody), "all text outside the image destination is byte-identical")
	require.Zero(t, transport.calls.Load())
	require.EqualValues(t, 1, after.IngestAPIKeyID)
	require.Equal(t, "Fixture collector", after.LastIngestAPIKeyName)
	logs, err := NewOperationLogService(h.db).List(h.ctx, "ingest_supplement", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, logs.Total)
	require.Equal(t, "Fixture collector", logs.Items[0].APIKeyName)
}

func TestIngestPartialCompletionListPaginationAcceptAndKeyRevocation(t *testing.T) {
	h, _ := newIngestHarness(t)
	forbidIngestNetwork(t, h)
	sources := []string{"https://example.org/a.png", "https://example.org/b.png"}
	req := ingestMissingImageRequest("https://example.org/partial", sources[0])
	req.Content += "\n\n![second](" + sources[1] + ")"
	first, err := h.service.Ingest(h.ctx, req)
	require.NoError(t, err)
	h.drain(t)
	second, err := h.service.Ingest(h.ctx, ingestMissingImageRequest("https://example.org/other", "https://example.org/other.png"))
	require.NoError(t, err)
	// Distinct text avoids content deduplication when source URLs are compared.
	if second.Status == "exists" {
		t.Fatal("fixture requires an independent second article")
	}
	h.drain(t)
	page, err := h.service.ListIncomplete(h.ctx, 1, 1, false)
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Len(t, page.Items, 1)
	require.NotEmpty(t, page.Items[0].URL)
	require.Len(t, page.Items[0].Missing, 2)
	next, err := h.service.ListIncomplete(h.ctx, 2, 1, false)
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	supplement, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: req.URL, Acquisition: "extracted", Images: []types.IngestImage{{SourceURL: sources[0], Data: articleImageDataURI(t, 72)}}})
	require.NoError(t, err)
	require.Equal(t, "supplemented", supplement.Status)
	require.Len(t, supplement.Completeness, 1)
	article := h.article(t, first.EntryID)
	require.True(t, article.Incomplete)
	require.True(t, article.ExternalCompletionAttempted)
	page, err = h.service.ListIncomplete(h.ctx, 0, 0, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	page, err = h.service.ListIncomplete(h.ctx, 1, 100, true)
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	accepted, err := h.service.AcceptIncomplete(h.ctx, first.EntryID)
	require.NoError(t, err)
	require.Empty(t, accepted.Completeness)
	require.False(t, h.article(t, first.EntryID).Incomplete)
	now := time.Now()
	require.NoError(t, h.db.Model(&types.APIKey{}).Where("id=1").Update("revoked_at", now).Error)
	_, err = h.service.CheckURLs(h.ctx, []string{req.URL})
	require.Error(t, err)
	_, err = h.service.Ingest(h.ctx, req)
	require.Error(t, err)
	_, err = h.service.AcceptIncomplete(h.ctx, second.EntryID)
	require.Error(t, err)
}

func TestIngestPendingBodySupplementReturnsToNormalReview(t *testing.T) {
	h, _ := newIngestHarness(t)
	url := "https://example.org/body-later"
	first, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: url, Acquisition: "extracted", Missing: []types.IngestMissing{{Type: "content", SourceURL: url, Reason: "暂时需要登录"}}})
	require.NoError(t, err)
	require.Equal(t, "pending_review", first.Status)
	require.Empty(t, h.processor.stages)
	result, err := h.service.Ingest(h.ctx, ingestFixtureRequest(url, "Acme publishes the complete original observation report with measurements and detailed methodology."))
	require.NoError(t, err)
	require.Equal(t, "supplemented", result.Status)
	require.Equal(t, first.EntryID, result.EntryID)
	h.drain(t)
	article := h.article(t, first.EntryID)
	require.False(t, article.Incomplete)
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	intake, err := article.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
	require.Equal(t, []string{"intake_review", "article_processing"}, h.processor.stages)
}

func TestIngestConflictPreservesOriginalAndCanBeDiscardedOrSaved(t *testing.T) {
	h, v := newIngestHarness(t)
	forbidIngestNetwork(t, h)
	req := ingestMissingImageRequest("https://example.org/conflict", "https://example.org/conflict.png")
	first, err := h.service.Ingest(h.ctx, req)
	require.NoError(t, err)
	h.drain(t)
	original := h.article(t, first.EntryID)
	_, _, body := readVaultArticle(t, v, original)
	different := ingestFixtureRequest(req.URL, "An unrelated novel discusses a fictional seafaring kingdom, characters and narrative choices.")
	different.CollectReason = "保留新材料供用户判断"
	different.AgentSummary = "外部说明"
	different.AgentTags = []string{"小说"}
	different.SourceName = "Fixture new source"
	result, err := h.service.Ingest(h.ctx, different)
	require.NoError(t, err)
	require.Equal(t, "conflict", result.Status)
	require.NotEqual(t, first.EntryID, result.EntryID)
	conflict := h.article(t, result.EntryID)
	require.Equal(t, first.EntryID, conflict.RelatedArticleID)
	require.Equal(t, different.CollectReason, conflict.CollectReason)
	require.Equal(t, different.AgentSummary, conflict.AgentSummary)
	_, _, still := readVaultArticle(t, v, h.article(t, first.EntryID))
	require.Equal(t, body, still)
	require.NoError(t, h.service.DiscardIngest(h.ctx, result.EntryID, "用户不保留冲突材料"))
	checks, err := h.service.CheckURLs(h.ctx, []string{req.URL})
	require.NoError(t, err)
	require.Equal(t, "in_library", checks[0].Status)
	result, err = h.service.Ingest(h.ctx, different)
	require.NoError(t, err)
	require.Equal(t, "conflict", result.Status)
	_, err = h.service.ReviewEntryIntake(h.ctx, result.EntryID, "approve", types.SourceInfo{})
	require.NoError(t, err)
	h.drain(t)
	saved := h.article(t, result.EntryID)
	require.Equal(t, types.ParseStatusCompleted, saved.ParseStatus)
	require.NotEmpty(t, saved.VaultPath)
	require.Equal(t, first.EntryID, saved.RelatedArticleID)
}

func TestIngestConcurrentSameSupplementMutatesOnce(t *testing.T) {
	h, _ := newIngestHarness(t)
	forbidIngestNetwork(t, h)
	source := "https://example.org/concurrent.png"
	first, err := h.service.Ingest(h.ctx, ingestMissingImageRequest("https://example.org/concurrent", source))
	require.NoError(t, err)
	h.drain(t)
	request := &types.IngestRequest{URL: "https://example.org/concurrent", Acquisition: "verbatim", Images: []types.IngestImage{{SourceURL: source, Data: articleImageDataURI(t, 73)}}}
	var wg sync.WaitGroup
	results := make([]*types.IngestResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = h.service.Ingest(h.ctx, request) }(i)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	statuses := []string{results[0].Status, results[1].Status}
	require.ElementsMatch(t, []string{"supplemented", "exists"}, statuses)
	logs, err := NewOperationLogService(h.db).List(h.ctx, "ingest_supplement", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, logs.Total)
	require.False(t, h.article(t, first.EntryID).Incomplete)
}

func TestIngestFileBase64LocalParsingAndReferencedUpload(t *testing.T) {
	h, v := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	content := "# File original\n\nAcme reports the complete temperature readings and the original calibration procedure.\n\n![external](https://example.org/file-remote.png)"
	uploaded, err := h.service.UploadIngestAsset(h.ctx, "original.md", []byte(content))
	require.NoError(t, err)
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/file", Acquisition: "verbatim", Publisher: "Acme", File: &types.IngestFile{AssetID: uploaded.AssetID}})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	require.Len(t, result.Completeness, 1)
	for h.processed < len(h.queue.tasks) {
		task := h.queue.tasks[h.processed]
		h.processed++
		if task.Type() == types.TypeManualProcess {
			require.NoError(t, h.service.ProcessManualUpdate(h.ctx, task))
		}
	}
	article := h.article(t, result.EntryID)
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus, article.ErrorMessage)
	require.True(t, article.Incomplete)
	require.NotEmpty(t, article.VaultOriginalPath)
	_, _, body := readVaultArticle(t, v, article)
	require.Contains(t, string(body), "https://example.org/file-remote.png")
	var asset types.IngestAsset
	require.NoError(t, h.db.Where("id=?", uploaded.AssetID).First(&asset).Error)
	require.NotNil(t, asset.ReferencedAt)
	require.Equal(t, article.ID, asset.EntryID)
	require.NoError(t, h.service.CleanupIngestAssets(h.ctx, time.Now().Add(25*time.Hour)))
	require.NoError(t, h.db.Where("id=?", uploaded.AssetID).First(&asset).Error)
	require.Zero(t, transport.calls.Load())
	inline := strings.Repeat("A", base64.StdEncoding.EncodedLen(types.MaxIngestInlineAssetBytes+1))
	rejected, err := h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{{URL: "https://example.org/large", Acquisition: "verbatim", File: &types.IngestFile{Name: "large.pdf", Data: inline}}}})
	require.NoError(t, err)
	require.Equal(t, "invalid", rejected.Items[0].Status)
	_, err = h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{{File: &types.IngestFile{Data: inline}}, {File: &types.IngestFile{Data: inline}}}})
	require.Error(t, err)
}

func TestIngestSupplementRollbackRestoresVaultBody(t *testing.T) {
	h, v := newIngestHarness(t)
	forbidIngestNetwork(t, h)
	source := "https://example.org/rollback.png"
	first, err := h.service.Ingest(h.ctx, ingestMissingImageRequest("https://example.org/rollback", source))
	require.NoError(t, err)
	h.drain(t)
	article := h.article(t, first.EntryID)
	before, err := os.ReadFile(filepath.Join(v.root, article.VaultPath))
	require.NoError(t, err)
	require.NoError(t, h.db.Exec("DROP TABLE operation_logs").Error)
	_, err = h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/rollback", Acquisition: "verbatim", Images: []types.IngestImage{{SourceURL: source, Data: articleImageDataURI(t, 74)}}})
	require.Error(t, err)
	after, err := os.ReadFile(filepath.Join(v.root, article.VaultPath))
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.True(t, h.article(t, article.ID).Incomplete)
}

// This fixture uses valid PDF bytes and substitutes only the parser result.
// It verifies downstream handling of a PDF-derived remote image without
// claiming that the optional native PDF parser is available on this host.
type ingestPDFResultReader struct {
	t    *testing.T
	want []byte
}

func (r *ingestPDFResultReader) Read(_ context.Context, request *types.ReadRequest) (*types.ReadResult, error) {
	require.Empty(r.t, request.URL)
	require.Equal(r.t, "pdf", request.FileType)
	require.Equal(r.t, r.want, request.FileContent)
	return &types.ReadResult{MarkdownContent: "# PDF original\n\nAcme publishes the pressure measurements and complete original calibration protocol.\n\n![remote figure](https://example.org/pdf-image.png)", Metadata: map[string]string{"parser": "PDF result fixture"}}, nil
}
func TestIngestPDFParserResultRemoteImageNeverFetches(t *testing.T) {
	h, v := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	pdf := sourcedMetadataPDF("Original measurements are reproducible.", "/Title (Original study) /URL (https://example.org/pdf-original)")
	h.service.ingestDocReader = &ingestPDFResultReader{t, pdf}
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/pdf-original", Acquisition: "verbatim", Publisher: "Acme", File: &types.IngestFile{Name: "original.pdf", Data: base64.StdEncoding.EncodeToString(pdf)}})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	require.Len(t, result.Completeness, 1)
	require.Equal(t, "https://example.org/pdf-image.png", result.Completeness[0].SourceURL)
	require.Equal(t, "未提供图片文件", result.Completeness[0].Reason)
	h.drain(t)
	article := h.article(t, result.EntryID)
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus, article.ErrorMessage)
	require.True(t, article.Incomplete)
	_, _, body := readVaultArticle(t, v, article)
	require.Contains(t, string(body), "https://example.org/pdf-image.png")
	actual, err := os.ReadFile(filepath.Join(v.root, article.VaultOriginalPath))
	require.NoError(t, err)
	require.Equal(t, pdf, actual)
	require.Zero(t, transport.calls.Load())
}

func TestIngestFileSupplementAddsAttachmentWithoutChangingExistingText(t *testing.T) {
	h, v := newIngestHarness(t)
	forbidIngestNetwork(t, h)
	request := ingestFixtureRequest("https://example.org/file-supplement", "The original report describes the instrument accuracy and the complete measurement conditions.")
	request.Missing = []types.IngestMissing{{Type: "file", SourceURL: "https://example.org/original.pdf", Reason: "暂无原始文件权限"}}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	h.drain(t)
	before := h.article(t, first.EntryID)
	_, _, beforeBody := readVaultArticle(t, v, before)
	bytes := []byte("Original attachment supplied by an external tool.")
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: request.URL, Acquisition: "verbatim", File: &types.IngestFile{Name: "original.txt", Data: base64.StdEncoding.EncodeToString(bytes), SourceURL: "https://example.org/original.pdf"}})
	require.NoError(t, err)
	require.Equal(t, "supplemented", result.Status)
	require.Empty(t, result.Completeness)
	h.drain(t)
	after := h.article(t, first.EntryID)
	require.False(t, after.Incomplete)
	require.Len(t, after.IngestAttachmentRecords(), 1)
	_, _, afterBody := readVaultArticle(t, v, after)
	require.True(t, strings.HasPrefix(string(afterBody), string(beforeBody)))
	require.Contains(t, string(afterBody), "[original.txt](<../../attachments/")
	ref := after.IngestAttachmentRecords()[0].Path
	actual, err := os.ReadFile(filepath.Join(v.root, filepath.Clean(filepath.Join(filepath.Dir(after.VaultPath), ref))))
	require.NoError(t, err)
	require.Equal(t, bytes, actual)
}

func TestIngestCorrectedSummaryReceivesItsFirstBody(t *testing.T) {
	h, v := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	h.processor.output = validProcessingFixture("tutorial")
	request := ingestFixtureRequest("https://example.org/corrected-completion", "Calibrate the original pressure instrument and record its sensitivity before measuring the supplied specimens.")
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	h.drain(t)
	corrected, err := h.service.CorrectContentType(h.ctx, first.EntryID, "research", false)
	require.NoError(t, err)
	require.True(t, corrected.Incomplete)
	_, _, body := readVaultArticle(t, v, corrected)
	require.Empty(t, body)
	h.processor.output = validProcessingFixture("research")
	result, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "supplemented", result.Status)
	h.drain(t)
	completed := h.article(t, first.EntryID)
	require.False(t, completed.Incomplete)
	_, _, body = readVaultArticle(t, v, completed)
	require.Equal(t, assembleIngestBody(request, nil), string(body))
	require.Zero(t, transport.calls.Load())
}

func TestIngestPendingBodyRestoredFromFileRetainsOriginalAttachment(t *testing.T) {
	h, v := newIngestHarness(t)
	forbidIngestNetwork(t, h)
	url := "https://example.org/pending-file-body"
	first, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: url, Acquisition: "extracted", Missing: []types.IngestMissing{{Type: "content", Reason: "暂缺正文"}}})
	require.NoError(t, err)
	original := "# File restored\n\nAcme supplies the exact measured temperature, its reproducible methods, and the original instrument configuration."
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: url, Acquisition: "verbatim", Publisher: "Acme", File: &types.IngestFile{Name: "original.md", Data: base64.StdEncoding.EncodeToString([]byte(original))}})
	require.NoError(t, err)
	require.Equal(t, "supplemented", result.Status)
	h.drain(t)
	article := h.article(t, first.EntryID)
	require.False(t, article.Incomplete)
	require.Equal(t, "verbatim", article.Acquisition)
	require.Len(t, article.IngestAttachmentRecords(), 1)
	require.NotEmpty(t, article.VaultOriginalPath)
	saved, err := os.ReadFile(filepath.Join(v.root, article.VaultOriginalPath))
	require.NoError(t, err)
	require.Equal(t, original, string(saved))
}

func TestIngestSummaryOnlyIncompleteRetainsMaterialsAndCompletes(t *testing.T) {
	for _, nextType := range []string{"tutorial", "research"} {
		t.Run(nextType, func(t *testing.T) {
			h, v := newIngestHarness(t)
			transport := forbidIngestNetwork(t, h)
			h.processor.output = validProcessingFixture("tutorial")
			source := "https://example.org/summary-figure.png"
			request := ingestMissingImageRequest("https://example.org/summary-completion", source)
			first, err := h.service.Ingest(h.ctx, request)
			require.NoError(t, err)
			h.drain(t)
			before := h.article(t, first.EntryID)
			require.True(t, before.Incomplete)
			require.Equal(t, types.ProcessingModeSummaryOnly, before.ProcessingMode)
			_, _, beforeBody := readVaultArticle(t, v, before)
			require.Empty(t, beforeBody)
			intake, err := before.IntakeMetadata()
			require.NoError(t, err)
			require.NotEmpty(t, intake.OriginalPath)
			raw, err := h.service.readIngestBody(h.ctx, h.kb, before)
			require.NoError(t, err)
			require.Contains(t, raw, source)
			h.processor.output = validProcessingFixture(nextType)
			result, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: request.URL, Acquisition: "extracted", Images: []types.IngestImage{{SourceURL: source, Data: articleImageDataURI(t, 81)}}})
			require.NoError(t, err)
			require.Equal(t, "supplemented", result.Status)
			require.Empty(t, result.Completeness)
			h.drain(t)
			after := h.article(t, first.EntryID)
			require.False(t, after.Incomplete)
			require.Len(t, h.processor.requests, 2)
			require.NotContains(t, string(after.Metadata), "completion_pending")
			waitCorrectionHeader(t, v, after, nextType)
			_, _, body := readVaultArticle(t, v, after)
			if nextType == "tutorial" {
				require.Empty(t, body)
				intake, _ = after.IntakeMetadata()
				require.Empty(t, intake.OriginalPath)
			} else {
				require.Equal(t, types.ProcessingModeFull, after.ProcessingMode)
				refs := articleImagesInOrder(string(body), nil)
				require.Len(t, refs, 1)
				require.Contains(t, refs[0], "../../attachments/")
				require.Equal(t, replaceIngestImageTargets(assembleIngestBody(request, nil), map[string]string{source: refs[0]}), string(body))
				_, err = os.Stat(filepath.Join(v.root, filepath.Clean(filepath.Join(filepath.Dir(after.VaultPath), refs[0]))))
				require.NoError(t, err)
			}
			require.Zero(t, transport.calls.Load())
		})
	}
}

func TestIngestSummaryOnlyAcceptMissingDropsPrivateMaterials(t *testing.T) {
	h, v := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	h.processor.output = validProcessingFixture("tutorial")
	source := "https://example.org/accepted-summary.png"
	raw := "# Original tutorial\n\nAcme describes the instrument calibration procedure with reproducible observations.\n\n![figure](" + source + ")"
	first, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/accepted-summary", Acquisition: "verbatim", Publisher: "Acme", File: &types.IngestFile{Name: "original.md", Data: base64.StdEncoding.EncodeToString([]byte(raw))}})
	require.NoError(t, err)
	h.drain(t)
	before := h.article(t, first.EntryID)
	require.True(t, before.Incomplete)
	require.Len(t, before.IngestAttachmentRecords(), 1)
	intake, err := before.IntakeMetadata()
	require.NoError(t, err)
	require.NotEmpty(t, intake.OriginalPath)
	result, err := h.service.AcceptIncomplete(h.ctx, before.ID)
	require.NoError(t, err)
	require.Empty(t, result.Completeness)
	after := h.article(t, before.ID)
	require.False(t, after.Incomplete)
	require.Empty(t, after.FilePath)
	require.Empty(t, after.IngestAttachmentRecords())
	afterIntake, err := after.IntakeMetadata()
	require.NoError(t, err)
	require.Empty(t, afterIntake.OriginalPath)
	_, _, body := readVaultArticle(t, v, after)
	require.Empty(t, body)
	require.NotContains(t, h.service.fileSvc.(*articleProcessingFiles).saved, intake.OriginalPath)
	require.NotContains(t, h.service.fileSvc.(*articleProcessingFiles).saved, before.FilePath)
	require.Len(t, after.IngestMissing(), 1, "accepted missing reasons remain in durable history")
	require.Zero(t, transport.calls.Load())
}
