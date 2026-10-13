package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

type ingestFixtureLibrary struct {
	retainedProcessingLibrary
	db *gorm.DB
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
		{"article supplement ID", &types.IngestRequest{Library: "institutional", EntryID: "11111111-1111-4111-8111-111111111111", Acquisition: "verbatim", Content: "Original"}},
		{"invalid document ID", &types.IngestRequest{Library: "mine", EntryID: "invalid", Acquisition: "verbatim", Content: "Original"}},
		{"missing content", &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", URL: "https://x.com/author/status/1"}},
		{"blank content", &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", URL: "https://x.com/author/status/1", Content: " \n "}},
		{"empty after existing cleanup", &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", URL: "https://x.com/author/status/1", Content: "<script>alert(1)</script>"}},
		{"invalid url", &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", URL: "file:///etc/passwd", Content: "Original"}},
		{"image neither source", &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", URL: "https://x.com/a/1", Content: "Original", Images: []types.IngestImage{{Caption: "Missing"}}}},
		{"image both sources", &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", URL: "https://x.com/a/1", Content: "Original", Images: []types.IngestImage{{URL: "https://example.com/a.png", Data: "base64"}}}},
		{"image invalid url", &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", URL: "https://x.com/a/1", Content: "Original", Images: []types.IngestImage{{URL: "file:///image.png"}}}},
		{"thread without content", &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", URL: "https://x.com/a/1", Content: "Original", Thread: []types.IngestThreadItem{{Author: "Ada"}}}},
		{"quote without content", &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", URL: "https://x.com/a/1", Content: "Original", Quotes: []types.IngestQuote{{Author: "Ada"}}}},
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

func TestIngestDuplicateReturnsExistingAndFillsReasonWithoutReprocessing(t *testing.T) {
	h, vault := newIngestHarness(t)
	request := &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", Publisher: "Acme", URL: "https://example.org/ada/status/456?utm_source=grok", Content: "An original result with measurements and complete supported evidence.", Title: "去重测试"}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	h.drain(t)
	article := h.article(t, first.EntryID)
	_, _, originalBody := readVaultArticle(t, vault, article)
	request.URL = "https://example.org/ada/status/456#thread"
	request.CollectReason = "保留做研究"
	request.Content = "Repeated URL arrives with a modified delivery."
	duplicate, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, first.EntryID, duplicate.EntryID)
	require.Equal(t, "exists", duplicate.Status)
	require.Equal(t, "该文章已收录。", duplicate.Message)
	require.Empty(t, duplicate.ImageFailures)
	require.Len(t, h.queue.tasks, 1)
	require.Len(t, h.processor.requests, 1)
	require.Equal(t, request.CollectReason, h.article(t, first.EntryID).CollectReason)
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(vault.root, article.VaultPath))
		if err != nil {
			return false
		}
		header, body, err := ParseVaultArticle(data)
		return err == nil && header.CollectReason == "保留做研究" && string(body) == string(originalBody)
	}, time.Second, 10*time.Millisecond)
	request.CollectReason = "后续理由不能覆盖"
	_, err = h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "保留做研究", h.article(t, first.EntryID).CollectReason)
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	logs, err := NewOperationLogService(h.db).List(h.ctx, "ingest", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 3, logs.Total)
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

func TestIngestDuplicateReasonSurvivesInFlightProcessingAndVaultWrite(t *testing.T) {
	h, vault := newIngestHarness(t)
	paused := &ingestPausedProcessingChat{retainedProcessingChatInterface: h.processor, started: make(chan struct{}), resume: make(chan struct{})}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService = &summaryStateModelService{model: paused, metadata: models.metadata}
	h.service.modelService = models
	request := &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", Publisher: "Acme", URL: "https://example.org/ada/status/concurrent-reason", Content: "Original measurements describe the complete supported product capabilities.", Title: "并发收藏理由"}
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
	require.Equal(t, types.ParseStatusProcessing, stale.ParseStatus)
	require.Empty(t, stale.CollectReason)
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
	request := &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", Publisher: "Acme", URL: "https://example.org/ada/status/501", Content: "A sourced original with measured product observations.", Images: []types.IngestImage{{Data: strings.SplitN(articleImageDataURI(t, 13), ",", 2)[1], Caption: "Same chart"}}}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	canonical, _, _ := messageBodyFingerprint(assembleIngestBody(request, []string{ingestCanonicalImageReference(request.Images[0])}))
	require.Equal(t, canonical, h.article(t, first.EntryID).ContentHash)
	h.drain(t)
	require.Equal(t, canonical, h.article(t, first.EntryID).ContentHash)
	request.URL = "https://example.org/repost"
	request.CollectReason = "原始结果"
	duplicate, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, first.EntryID, duplicate.EntryID)
	require.Equal(t, "exists", duplicate.Status)
	require.Len(t, h.queue.tasks, 1)
	require.Len(t, h.processor.requests, 1)
	reposts, err := loadEntryRepostSources(h.db, h.library.SpaceID, []string{first.EntryID}, nil)
	require.NoError(t, err)
	require.Len(t, reposts, 1)
	require.Equal(t, "content_hash", reposts[0].MatchMethod)
	request.URL = "https://example.org/unrelated"
	request.Content = "Independent researchers found that crystalline minerals form under elevated pressure. The experiment compared rock composition and temperature gradients across six geological samples. Detailed tables describe physical properties, laboratory conditions and the chemical instruments used for each measurement."
	unrelated, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "accepted", unrelated.Status, "shared image bytes cannot dominate textual deduplication")
	require.NotEqual(t, first.EntryID, unrelated.EntryID)
}

func TestIngestAuditFailureRollsBackCollectionAndReason(t *testing.T) {
	h, _ := newIngestHarness(t)
	request := &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", Publisher: "Acme", URL: "https://example.org/audit-existing", Content: "The original article is retained for an audit failure test."}
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
	_, err = h.service.Ingest(h.ctx, request)
	require.ErrorContains(t, err, "audit unavailable")
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.Len(t, h.queue.tasks, 1)
	files := h.service.fileSvc.(*articleProcessingFiles)
	require.Len(t, files.saved, 1, "the original first article remains; failed collection files are removed")
}

func TestAssembleIngestBodyExactFormat(t *testing.T) {
	request := &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", Content: "主内容", Thread: []types.IngestThreadItem{{Author: "作者 A", PublishedAt: "2026-10-01", Content: "推文内容"}}, Quotes: []types.IngestQuote{{Content: "引用一\n引用二"}}, Images: []types.IngestImage{{Caption: "图说明", Text: "图中文字"}}}
	want := "主内容\n\n## 推文串\n\n### 推文 1\n\n作者：作者 A\n\n时间：2026-10-01\n\n推文内容\n\n## 引用\n\n### 引用 1\n\n> 引用一\n> 引用二\n\n## 图片\n\n### 图片 1\n\n![图说明](<local://image.png>)\n\n说明：图说明\n\n图片文字：\n\n> 图中文字\n"
	require.Equal(t, want, assembleIngestBody(request, []string{"local://image.png"}))
	var typed types.IngestRequest
	require.NoError(t, json.Unmarshal([]byte(`{"url":"https://x.com/a/1","content":"Original","thread":[{"author":"Ada","published_at":"2026-10-01","content":"Thread"}],"quotes":[{"content":"Quote","author":"Grace","url":"https://example.org/q"}]}`), &typed))
	require.Equal(t, "2026-10-01", typed.Thread[0].PublishedAt)
}
