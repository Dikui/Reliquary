package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/diagnostics"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

type ingestFixtureLibrary struct {
	retainedProcessingLibrary
	db *gorm.DB
}

func (s ingestFixtureLibrary) GetLibraryByID(ctx context.Context, id string) (*types.Library, error) {
	return repository.NewLibraryRepository(s.db).GetLibraryByID(ctx, id)
}

func (s ingestFixtureLibrary) ListLibraries(ctx context.Context) ([]*types.Library, error) {
	return repository.NewLibraryRepository(s.db).ListLibraries(ctx, s.library.SpaceID)
}
func NewLibraryServiceForIngestTest(h *articleProcessingHarness) ingestFixtureLibrary {
	return ingestFixtureLibrary{retainedProcessingLibrary: retainedProcessingLibrary{library: h.library}, db: h.db}
}

func (s ingestFixtureLibrary) GetSystemLibrary(context.Context, ...string) (*types.Library, error) {
	return s.library, nil
}

func newIngestHarness(t *testing.T) (*articleProcessingHarness, *VaultService) {
	t.Helper()
	h, vault := newVaultArticleHarness(t)
	require.NoError(t, EnsureSystemLibraries(h.ctx, h.db))
	require.NoError(t, h.db.First(h.library, "id=?", h.library.ID).Error)
	// Each library shares deterministic local processing models in this harness.
	require.NoError(t, h.db.Model(&types.Library{}).Where("space_id=?", h.library.SpaceID).Update("model_tiers", h.library.ModelTiers).Error)
	h.service.libraryService = NewLibraryServiceForIngestTest(h)
	space := h.library.SpaceID
	require.NoError(t, h.db.Create(&types.APIKey{ID: 1, SpaceID: &space, Name: "Fixture collector", KeyHash: "fixture-ingest-key"}).Error)
	h.ctx = types.WithPrincipal(h.ctx, types.Principal{Type: types.PrincipalAPIKey, ID: "1"})
	return h, vault
}

func TestIngestRequiredFieldsAndStructuredValidation(t *testing.T) {
	h, _ := newIngestHarness(t)
	for _, test := range []struct {
		name    string
		request *types.IngestRequest
	}{
		{"nil", nil},
		{"unknown kind", &types.IngestRequest{Library: "unknown", Acquisition: "verbatim", Content: "Original"}},
		{"missing content", &types.IngestRequest{Library: "official", Acquisition: "verbatim", URL: "https://x.com/author/status/1"}},
		{"blank content", &types.IngestRequest{Library: "official", Acquisition: "verbatim", URL: "https://x.com/author/status/1", Content: " \n "}},
		{"empty after existing cleanup", &types.IngestRequest{Library: "official", Acquisition: "verbatim", URL: "https://x.com/author/status/1", Content: "<script>alert(1)</script>"}},
		{"invalid url", &types.IngestRequest{Library: "official", Acquisition: "verbatim", URL: "file:///etc/passwd", Content: "Original"}},
		{"image neither source", &types.IngestRequest{Library: "official", Acquisition: "verbatim", URL: "https://x.com/a/1", Content: "Original", Images: []types.IngestImage{{Caption: "Missing"}}}},
		{"image both sources", &types.IngestRequest{Library: "official", Acquisition: "verbatim", URL: "https://x.com/a/1", Content: "Original", Images: []types.IngestImage{{URL: "https://example.com/a.png", Data: "base64"}}}},
		{"image invalid url", &types.IngestRequest{Library: "official", Acquisition: "verbatim", URL: "https://x.com/a/1", Content: "Original", Images: []types.IngestImage{{URL: "file:///image.png"}}}},
		{"thread without content", &types.IngestRequest{Library: "official", Acquisition: "verbatim", URL: "https://x.com/a/1", Content: "Original", Thread: []types.IngestThreadItem{{Author: "Ada"}}}},
		{"quote without content", &types.IngestRequest{Library: "official", Acquisition: "verbatim", URL: "https://x.com/a/1", Content: "Original", Quotes: []types.IngestQuote{{Author: "Ada"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := h.service.Ingest(h.ctx, test.request)
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Count(&count).Error)
	require.Zero(t, count)
	require.Empty(t, h.queue.tasks)
}

func TestIngestSameURLReplacesBodyAndMetadata(t *testing.T) {
	h, _ := newIngestHarness(t)
	request := &types.IngestRequest{Library: "official", URL: "https://example.org/result?utm_source=feed", Content: "Original measurements record 42 pascals.", Title: "Measurements"}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	h.drain(t)
	request.URL = "https://example.org/result#details"
	request.Content = "Corrected measurements record 43 pascals."
	request.CollectReason = "保留做研究"
	changed, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "accepted", changed.Status)
	require.Equal(t, first.EntryID, changed.EntryID)
	require.Contains(t, h.article(t, first.EntryID).VaultBody, "43 pascals")
	require.Equal(t, request.CollectReason, h.article(t, first.EntryID).CollectReason)
	require.Len(t, h.queue.tasks, 2)
	h.drain(t)
	unchanged, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "exists", unchanged.Status)
	require.Len(t, h.queue.tasks, 2)
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

type ingestPausedProcessingChat struct {
	retainedProcessingChatInterface
	started chan struct{}
	resume  chan struct{}
}

func (m *ingestPausedProcessingChat) Chat(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if stage == "article_processing" {
		close(m.started)
		select {
		case <-m.resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.retainedProcessingChatInterface.Chat(ctx, messages, options)
}

func TestIngestExactRetrySurvivesInFlightProcessing(t *testing.T) {
	h, vault := newIngestHarness(t)
	paused := &ingestPausedProcessingChat{retainedProcessingChatInterface: h.processor, started: make(chan struct{}), resume: make(chan struct{})}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService = &summaryStateModelService{model: paused, metadata: models.metadata}
	h.service.modelService = models
	request := &types.IngestRequest{Library: "official", Acquisition: "verbatim", Publisher: "Acme", URL: "https://example.org/ada/status/concurrent-reason", Content: "Original measurements describe the complete supported product capabilities.", Title: "并发收藏理由", CollectReason: "保留这次实验的测量结论"}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	result := make(chan error, 1)
	completed := make(chan struct{})
	var release sync.Once
	resume := func() { release.Do(func() { close(paused.resume) }) }
	t.Cleanup(func() {
		resume()
		select {
		case <-completed:
		case <-time.After(5 * time.Second):
			t.Error("article worker did not finish after releasing the model substitute")
		}
	})
	go func() {
		result <- h.service.ProcessManualUpdate(h.ctx, h.queue.tasks[0])
		close(completed)
	}()
	select {
	case <-paused.started:
	case <-time.After(5 * time.Second):
		t.Fatal("article processing did not reach the model substitute")
	}
	stale := h.article(t, first.EntryID)
	require.Equal(t, types.ParseStatusCompleted, stale.ParseStatus)
	require.Equal(t, request.CollectReason, stale.CollectReason)
	request.CollectReason = "保留这次实验的测量结论"
	duplicate, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, first.EntryID, duplicate.EntryID)
	require.Equal(t, "exists", duplicate.Status)
	require.Equal(t, request.CollectReason, h.article(t, first.EntryID).CollectReason)
	resume()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("article processing did not finish")
	}
	article := h.article(t, first.EntryID)
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	require.Equal(t, request.CollectReason, article.CollectReason)
	_, header, body := readVaultArticle(t, vault, article)
	require.Contains(t, string(header), "collect_reason: \"保留这次实验的测量结论\"")
	require.Equal(t, assembleIngestBody(request, nil), string(body))
	require.NoError(t, vault.RefreshHeader(h.ctx, article.ID))
	_, header, body = readVaultArticle(t, vault, article)
	require.Contains(t, string(header), "collect_reason: \"保留这次实验的测量结论\"")
	require.Equal(t, assembleIngestBody(request, nil), string(body))
	require.Len(t, h.processor.requests, 1)
	require.Len(t, h.queue.tasks, 1)
}

func TestIngestContentDuplicatePreservesCanonicalImageFingerprint(t *testing.T) {
	h, _ := newIngestHarness(t)
	request := &types.IngestRequest{Library: "official", Acquisition: "verbatim", Publisher: "Acme", URL: "https://example.org/ada/status/501", Content: "A sourced original with measured product observations.", Images: []types.IngestImage{{Data: strings.SplitN(articleImageDataURI(t, 13), ",", 2)[1], Caption: "Same chart"}}}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	canonical := h.article(t, first.EntryID).ContentHash
	require.Equal(t, canonical, h.article(t, first.EntryID).ContentHash)
	h.drain(t)
	require.Equal(t, canonical, h.article(t, first.EntryID).ContentHash)
	request.URL = "https://example.org/ada/status/501#retry"
	duplicate, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, first.EntryID, duplicate.EntryID)
	require.Equal(t, "exists", duplicate.Status)
	require.Len(t, h.queue.tasks, 1)
	require.Len(t, h.processor.requests, 1)
	reposts, err := loadEntryRepostSources(h.db, h.library.SpaceID, []string{first.EntryID}, nil)
	require.NoError(t, err)
	require.Empty(t, reposts)
	request.URL = "https://example.org/unrelated"
	request.Content = "Independent researchers found that crystalline minerals form under elevated pressure. The experiment compared rock composition and temperature gradients across six geological samples. Detailed tables describe physical properties, laboratory conditions and the chemical instruments used for each measurement."
	unrelated, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "accepted", unrelated.Status, "shared image bytes cannot dominate textual deduplication")
	require.NotEqual(t, first.EntryID, unrelated.EntryID)
}

func TestIngestAuditFailureRollsBackCollectionAndReason(t *testing.T) {
	h, _ := newIngestHarness(t)
	diagnosticRoot := t.TempDir()
	diag, err := diagnostics.New(diagnosticRoot, diagnostics.DefaultRetention, diagnostics.DefaultMaxBytes)
	require.NoError(t, err)
	diag.SetVersion("fixture@revision")
	closeDiagnostics := diag.Start(context.Background())
	t.Cleanup(func() { require.NoError(t, closeDiagnostics()) })
	h.service.SetIngestDiagnostics(diag)
	request := &types.IngestRequest{Library: "official", Acquisition: "verbatim", Publisher: "Acme", URL: "https://example.org/audit-existing", Content: "The original article is retained for an audit failure test."}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.NoError(t, h.db.Exec(`CREATE FUNCTION fail_ingest_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER fail_ingest_audit BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION fail_ingest_audit()`).Error)
	request.CollectReason = "不应提交"
	_, err = h.service.Ingest(h.ctx, request)
	require.ErrorContains(t, err, "audit unavailable")
	require.Empty(t, h.article(t, first.EntryID).CollectReason)
	request.URL = "https://example.org/audit-new"
	request.Content = "A totally distinct second paper studies different physical properties and their relationships."
	request.Images = []types.IngestImage{{Data: strings.SplitN(articleImageDataURI(t, 14), ",", 2)[1]}}
	require.NoError(t, h.db.Exec(`CREATE OR REPLACE FUNCTION fail_ingest_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.after_value->>'collect_reason' = '不应提交' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$`).Error)
	success := types.IngestRequest{Library: "official", Acquisition: "verbatim", Publisher: "Independent laboratory", URL: "https://example.org/independent-geology", Content: "Independent researchers found that crystalline minerals form under elevated pressure. The experiment compared rock composition and temperature gradients across six geological samples. Detailed tables describe physical properties, laboratory conditions and the chemical instruments used for each measurement."}
	failed, err := h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{success, {Content: "Refused sibling must never be retained"}, *request}})
	require.NoError(t, err)
	require.Equal(t, "accepted", failed.Items[0].Status)
	require.Nil(t, failed.Items[0].Diagnostics)
	require.Equal(t, "invalid", failed.Items[1].Status)
	require.Nil(t, failed.Items[1].Diagnostics)
	require.Equal(t, "invalid", failed.Items[2].Status)
	require.NotNil(t, failed.Items[2].Diagnostics)
	failure, err := diag.Get(h.ctx, failed.Items[2].Diagnostics.FailureID)
	require.NoError(t, err)
	require.Contains(t, failure.Reason, "audit unavailable")
	require.Equal(t, "rolled_back", failure.Outcome)
	require.Equal(t, "available", failure.PackageStatus)
	require.Len(t, failure.Materials, 2)
	require.Equal(t, 2, failure.ItemIndex)
	_, original, err := diag.Read(h.ctx, failure.ID, failure.Materials[0].ID)
	require.NoError(t, err)
	var recovered types.IngestRequest
	require.NoError(t, json.Unmarshal(original, &recovered))
	require.Equal(t, request.Content, recovered.Content)
	require.NotContains(t, string(original), success.Content)
	require.NotContains(t, string(original), "Refused sibling")
	_, image, err := diag.Read(h.ctx, failure.ID, failure.Materials[1].ID)
	require.NoError(t, err)
	wantImage, err := decodeIngestInline(request.Images[0].Data)
	require.NoError(t, err)
	require.Equal(t, wantImage, image)
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.Len(t, h.queue.tasks, 2)
	files := h.service.fileSvc.(*articleProcessingFiles)
	require.Empty(t, files.saved, "text remains in PostgreSQL; failed collection files are removed")
	require.Equal(t, success.Content+"\n", h.article(t, failed.Items[0].EntryID).VaultBody)
	require.NoError(t, h.db.Exec(`DROP TRIGGER fail_ingest_audit ON operation_logs`).Error)
	recovered.RecoveryFailureID = failure.ID
	// Missing provenance does not block the repaired submission.
	recovered.URL, recovered.Publisher = "", ""
	// A failed repair creates a child failure; rejecting its eventual repair
	// must clear both snapshots, including the ancestor without a business row.
	require.NoError(t, h.db.Exec(`CREATE OR REPLACE FUNCTION fail_ingest_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER fail_ingest_audit BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION fail_ingest_audit()`).Error)
	second, err := h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{recovered}})
	require.NoError(t, err)
	require.NotNil(t, second.Items[0].Diagnostics)
	childID := second.Items[0].Diagnostics.FailureID
	child, err := diag.Get(h.ctx, childID)
	require.NoError(t, err)
	require.Equal(t, failure.ID, child.RecoveryFailureID)
	require.NoError(t, h.db.Exec(`DROP TRIGGER fail_ingest_audit ON operation_logs`).Error)
	recovered.RecoveryFailureID = childID
	repaired, err := h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{recovered}})
	require.NoError(t, err)
	require.Equal(t, "accepted", repaired.Items[0].Status)
	child, err = diag.Get(h.ctx, childID)
	require.NoError(t, err)
	require.Equal(t, repaired.Items[0].EntryID, child.RecoveryEntryID)

}

func TestAssembleIngestBodyExactFormat(t *testing.T) {
	request := &types.IngestRequest{Library: "official", Acquisition: "verbatim", Content: "主内容", Thread: []types.IngestThreadItem{{Author: "作者 A", PublishedAt: "2026-10-01", Content: "推文内容"}}, Quotes: []types.IngestQuote{{Content: "引用一\n引用二"}}, Images: []types.IngestImage{{Caption: "图说明", Text: "图中文字"}}}
	want := "主内容\n\n## 推文串\n\n### 推文 1\n\n作者：作者 A\n\n时间：2026-10-01\n\n推文内容\n\n## 引用\n\n### 引用 1\n\n> 引用一\n> 引用二\n\n## 图片\n\n### 图片 1\n\n![图说明](<local://image.png>)\n\n说明：图说明\n\n图片文字：\n\n> 图中文字\n"
	require.Equal(t, want, assembleIngestBody(request, []string{"local://image.png"}))
	var typed types.IngestRequest
	require.NoError(t, json.Unmarshal([]byte(`{"url":"https://x.com/a/1","content":"Original","thread":[{"author":"Ada","published_at":"2026-10-01","content":"Thread"}],"quotes":[{"content":"Quote","author":"Grace","url":"https://example.org/q"}]}`), &typed))
	require.Equal(t, "2026-10-01", typed.Thread[0].PublishedAt)
}

func TestIngestQueueRollbackDoesNotCallModels(t *testing.T) {
	h, _ := newIngestHarness(t)
	h.service.task = failingReparseTaskEnqueuer{err: context.DeadlineExceeded}
	req := &types.IngestRequest{Library: "official", URL: "https://example.org/rollback", Content: "Original measurements record 42 pascals."}
	_, err := h.service.Ingest(h.ctx, req)
	require.Error(t, err)
	require.Empty(t, h.processor.requests)
	require.Empty(t, h.processor.stages)
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Count(&count).Error)
	require.Zero(t, count)
	h.service.task = h.queue
	repaired, err := h.service.Ingest(h.ctx, req)
	require.NoError(t, err)
	require.Equal(t, "accepted", repaired.Status)
	require.Empty(t, h.processor.stages)
	h.drain(t)
	require.NotEmpty(t, h.processor.requests)
}
