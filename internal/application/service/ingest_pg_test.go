package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

type ingestFixtureKB struct{ retainedProcessingKB }

func (s ingestFixtureKB) GetKnowledgeBase(context.Context) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func newIngestHarness(t *testing.T) (*articleProcessingHarness, *VaultService) {
	t.Helper()
	h, vault := newVaultArticleHarness(t)
	h.service.kbService = ingestFixtureKB{retainedProcessingKB{kb: h.kb}}
	h.ctx = types.WithPrincipal(h.ctx, types.Principal{Type: types.PrincipalAPIKey, ID: "grok-test-key"})
	return h, vault
}

func TestIngestRequiredFieldsAndStructuredValidation(t *testing.T) {
	h, _ := newIngestHarness(t)
	for _, test := range []struct {
		name    string
		request *types.IngestRequest
	}{
		{"nil", nil},
		{"missing url", &types.IngestRequest{Content: "Original content"}},
		{"missing content", &types.IngestRequest{URL: "https://x.com/author/status/1"}},
		{"blank content", &types.IngestRequest{URL: "https://x.com/author/status/1", Content: " \n "}},
		{"empty after existing cleanup", &types.IngestRequest{URL: "https://x.com/author/status/1", Content: "<script>alert(1)</script>"}},
		{"invalid url", &types.IngestRequest{URL: "file:///etc/passwd", Content: "Original"}},
		{"image neither source", &types.IngestRequest{URL: "https://x.com/a/1", Content: "Original", Images: []types.IngestImage{{Caption: "Missing"}}}},
		{"image both sources", &types.IngestRequest{URL: "https://x.com/a/1", Content: "Original", Images: []types.IngestImage{{URL: "https://example.com/a.png", Data: "base64"}}}},
		{"image invalid url", &types.IngestRequest{URL: "https://x.com/a/1", Content: "Original", Images: []types.IngestImage{{URL: "file:///image.png"}}}},
		{"thread without content", &types.IngestRequest{URL: "https://x.com/a/1", Content: "Original", Thread: []types.IngestThreadItem{{Author: "Ada"}}}},
		{"quote without content", &types.IngestRequest{URL: "https://x.com/a/1", Content: "Original", Quotes: []types.IngestQuote{{Author: "Ada"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := h.service.Ingest(h.ctx, test.request)
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
	var count int64
	require.NoError(t, h.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.Zero(t, count)
	require.Empty(t, h.queue.tasks)
}

func TestIngestBodyStructureImagesModelReviewAnnotationsAndAudit(t *testing.T) {
	h, vault := newIngestHarness(t)
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	uri := articleImageDataURI(t, 11)
	imageBytes, err := base64.StdEncoding.DecodeString(strings.SplitN(uri, ",", 2)[1])
	require.NoError(t, err)
	downloads := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads[r.URL.Path]++
		if r.URL.Path == "/missing.png" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageBytes)
	}))
	defer server.Close()
	request := &types.IngestRequest{
		URL: "https://example.org/ada/status/123", Content: "# 主内容\n\n这是主文的完整内容。", Title: "Grok 收录样例",
		Author: "Ada", Publisher: "X", PublishedAt: "2026-10-01T08:00:00+08:00", CollectReason: "收藏测试中的测量结果",
		AgentSummary: "agent-secret-summary", AgentTags: []string{"agent-secret-tag"}, SourceName: "agent-secret-source",
		Thread: []types.IngestThreadItem{{Author: "Ada", PublishedAt: "2026-10-01T00:01:00Z", Content: "第一条推文\n\n补充数据。"}},
		Quotes: []types.IngestQuote{{Content: "引用第一行\n引用第二行", Author: "Grace", URL: "https://example.org/quote"}},
		Images: []types.IngestImage{
			{URL: server.URL + "/chart.png", Caption: "测量图", Text: "分数 42\n增幅 10%"},
			{URL: server.URL + "/missing.png", Caption: "失败说明不应出现", Text: "失败文字不应出现"},
			{Data: strings.SplitN(articleImageDataURI(t, 12), ",", 2)[1], Caption: "base64 图"},
			{Data: "invalid base64", Caption: "解码失败说明不应出现"},
		},
	}
	result, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "pending", result.Status)
	require.NotEmpty(t, result.ArticleID)
	require.Len(t, result.ImageFailures, 2)
	require.Equal(t, 1, result.ImageFailures[0].Index)
	require.Equal(t, request.Images[1].URL, result.ImageFailures[0].URL)
	require.Contains(t, result.ImageFailures[0].Reason, "404")
	require.Equal(t, 3, result.ImageFailures[1].Index)
	require.Contains(t, result.ImageFailures[1].Reason, "decode")
	require.Equal(t, 1, downloads["/chart.png"])
	require.Equal(t, 1, downloads["/missing.png"])
	require.Empty(t, h.processor.requests, "processing remains asynchronous")
	queued := string(h.queue.tasks[0].Payload())
	for _, annotation := range []string{request.AgentSummary, request.AgentTags[0], request.SourceName} {
		require.NotContains(t, queued, annotation)
	}
	before := h.article(t, result.ArticleID)
	intake, err := before.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
	h.drain(t)
	article := h.article(t, result.ArticleID)
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	require.Equal(t, request.AgentSummary, article.AgentSummary)
	require.JSONEq(t, `["agent-secret-tag"]`, string(article.AgentTags))
	require.Equal(t, request.SourceName, article.SourceName)
	require.Contains(t, h.processor.stages, "intake_review")
	require.Equal(t, []string{"intake_review", "article_processing"}, h.processor.stages)
	require.Len(t, h.processor.requests, 1)
	data, header, body := readVaultArticle(t, vault, article)
	require.Contains(t, string(header), "collect_reason: \"收藏测试中的测量结果\"")
	require.Contains(t, string(header), "published_at: \"2026-10-01T00:00:00Z\"")
	for _, annotation := range []string{"agent_summary", "agent_tags", "source_name", request.AgentSummary, request.AgentTags[0], request.SourceName} {
		require.NotContains(t, string(data), annotation)
		for _, message := range h.processor.requests[0] {
			require.NotContains(t, message.Content, annotation)
		}
	}
	ordered := []string{"# 主内容", "## 推文串", "### 推文 1", "作者：Ada", "时间：2026-10-01T00:01:00Z", "第一条推文", "## 引用", "### 引用 1", "作者：Grace", "来源：https://example.org/quote", "> 引用第一行\n> 引用第二行", "## 图片", "### 图片 1", "![测量图](<../../attachments/", "说明：测量图", "图片文字：\n\n> 分数 42\n> 增幅 10%", "### 图片 3", "说明：base64 图"}
	previous := -1
	for _, fragment := range ordered {
		position := strings.Index(string(body), fragment)
		require.Greater(t, position, previous, fragment)
		previous = position
	}
	require.NotContains(t, string(body), "失败说明")
	require.NotContains(t, string(body), "失败文字")
	require.NotContains(t, string(body), "### 图片 2")
	require.NotContains(t, string(body), "### 图片 4")
	require.Len(t, h.processor.requests[0][1].Images, 2)
	attachments, err := os.ReadDir(filepath.Join(vault.root, "attachments", article.ID))
	require.NoError(t, err)
	require.Len(t, attachments, 2, "both successfully collected images are retained")
	logs, err := NewOperationLogService(h.db).List(h.ctx, "ingest", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, logs.Total)
	require.Equal(t, "api_key:grok-test-key", logs.Items[0].Actor)
	require.Equal(t, article.ID, logs.Items[0].ObjectID)
}

func TestIngestDuplicateReturnsExistingAndFillsReasonWithoutReprocessing(t *testing.T) {
	h, vault := newIngestHarness(t)
	request := &types.IngestRequest{Publisher: "Acme", URL: "https://example.org/ada/status/456?utm_source=grok", Content: "An original result with measurements and complete supported evidence.", Title: "去重测试"}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	h.drain(t)
	article := h.article(t, first.ArticleID)
	_, _, originalBody := readVaultArticle(t, vault, article)
	request.URL = "https://example.org/ada/status/456#thread"
	request.CollectReason = "保留做研究"
	request.Content = "Repeated URL arrives with a modified delivery."
	duplicate, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, first.ArticleID, duplicate.ArticleID)
	require.Equal(t, "exists", duplicate.Status)
	require.Equal(t, "已存在", duplicate.Message)
	require.Empty(t, duplicate.ImageFailures)
	require.Len(t, h.queue.tasks, 1)
	require.Len(t, h.processor.requests, 1)
	require.Equal(t, request.CollectReason, h.article(t, first.ArticleID).CollectReason)
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
	require.Equal(t, "保留做研究", h.article(t, first.ArticleID).CollectReason)
	var count int64
	require.NoError(t, h.db.Model(&types.Knowledge{}).Count(&count).Error)
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
	request := &types.IngestRequest{Publisher: "Acme", URL: "https://example.org/ada/status/concurrent-reason", Content: "Original measurements describe the complete supported product capabilities.", Title: "并发收藏理由"}
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
	stale := h.article(t, first.ArticleID)
	require.Equal(t, types.ParseStatusProcessing, stale.ParseStatus)
	require.Empty(t, stale.CollectReason)
	request.CollectReason = "保留这次实验的测量结论"
	duplicate, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, first.ArticleID, duplicate.ArticleID)
	require.Equal(t, "exists", duplicate.Status)
	require.Equal(t, request.CollectReason, h.article(t, first.ArticleID).CollectReason)
	resume()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("article processing did not finish")
	}
	article := h.article(t, first.ArticleID)
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	require.Equal(t, request.CollectReason, article.CollectReason)
	_, header, body := readVaultArticle(t, vault, article)
	require.Contains(t, string(header), "collect_reason: \"保留这次实验的测量结论\"")
	require.Equal(t, request.Content, string(body))
	require.NoError(t, vault.RefreshHeader(h.ctx, article.ID))
	_, header, body = readVaultArticle(t, vault, article)
	require.Contains(t, string(header), "collect_reason: \"保留这次实验的测量结论\"")
	require.Equal(t, request.Content, string(body))
	require.Len(t, h.processor.requests, 1)
	require.Len(t, h.queue.tasks, 1)
}

func TestIngestContentDuplicatePreservesCanonicalImageFingerprint(t *testing.T) {
	h, _ := newIngestHarness(t)
	request := &types.IngestRequest{Publisher: "Acme", URL: "https://example.org/ada/status/501", Content: "A sourced original with measured product observations.", Images: []types.IngestImage{{Data: strings.SplitN(articleImageDataURI(t, 13), ",", 2)[1], Caption: "Same chart"}}}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	canonical, _, _ := messageBodyFingerprint(assembleIngestBody(request, []string{ingestCanonicalImageReference(request.Images[0])}))
	require.Equal(t, canonical, h.article(t, first.ArticleID).ContentHash)
	h.drain(t)
	require.Equal(t, canonical, h.article(t, first.ArticleID).ContentHash)
	request.URL = "https://example.org/repost"
	request.CollectReason = "原始结果"
	duplicate, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, first.ArticleID, duplicate.ArticleID)
	require.Equal(t, "exists", duplicate.Status)
	require.Len(t, h.queue.tasks, 1)
	require.Len(t, h.processor.requests, 1)
	reposts, err := loadKnowledgeRepostSources(h.db, h.kb.TenantID, []string{first.ArticleID}, nil)
	require.NoError(t, err)
	require.Len(t, reposts, 1)
	require.Equal(t, "content_hash", reposts[0].MatchMethod)
	request.URL = "https://example.org/unrelated"
	request.Content = "Independent researchers found that crystalline minerals form under elevated pressure. The experiment compared rock composition and temperature gradients across six geological samples. Detailed tables describe physical properties, laboratory conditions and the chemical instruments used for each measurement."
	unrelated, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "pending", unrelated.Status, "shared image bytes cannot dominate textual deduplication")
	require.NotEqual(t, first.ArticleID, unrelated.ArticleID)
}

func TestIngestImageSSRFRejectionIsSkippedAndReported(t *testing.T) {
	h, vault := newIngestHarness(t)
	t.Setenv("SSRF_WHITELIST", "")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Publisher: "Acme", URL: "https://example.org/ada/status/unsafe-image", Content: "The sourced article remains usable when its supporting image is unreachable.", Images: []types.IngestImage{{URL: server.URL + "/image.png", Caption: "Blocked attachment"}}})
	require.NoError(t, err)
	require.Equal(t, "pending", result.Status)
	require.Len(t, result.ImageFailures, 1)
	require.Contains(t, result.ImageFailures[0].Reason, "SSRF")
	require.Zero(t, requests, "the existing SSRF protected downloader must reject the private target before HTTP")
	h.drain(t)
	article := h.article(t, result.ArticleID)
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	_, _, body := readVaultArticle(t, vault, article)
	require.NotContains(t, string(body), "## 图片")
	require.NotContains(t, string(body), "Blocked attachment")
}

func TestIngestFailedImageFingerprintMatchesSameBodyWithoutImage(t *testing.T) {
	h, _ := newIngestHarness(t)
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	request := &types.IngestRequest{Publisher: "Acme", URL: "https://example.org/ada/status/failed-image", Content: "An original announcement includes measured results.", Images: []types.IngestImage{{URL: server.URL + "/missing.png", Caption: "Unavailable chart", Text: "Unavailable measurements"}}}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Len(t, first.ImageFailures, 1)
	require.Contains(t, first.ImageFailures[0].Reason, "404")
	h.drain(t)
	article := h.article(t, first.ArticleID)
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	request.URL = "https://example.org/same-announcement"
	request.Images = nil
	canonical, _, _ := messageBodyFingerprint(assembleIngestBody(request, nil))
	require.Equal(t, canonical, article.ContentHash)
	duplicate, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, first.ArticleID, duplicate.ArticleID)
	require.Equal(t, "exists", duplicate.Status)
	require.Len(t, h.queue.tasks, 1)
	require.Len(t, h.processor.requests, 1)
	reposts, err := loadKnowledgeRepostSources(h.db, h.kb.TenantID, []string{article.ID}, nil)
	require.NoError(t, err)
	require.Len(t, reposts, 1)
	require.Equal(t, "content_hash", reposts[0].MatchMethod)
}

func TestIngestAuditFailureRollsBackCollectionAndReason(t *testing.T) {
	h, _ := newIngestHarness(t)
	request := &types.IngestRequest{Publisher: "Acme", URL: "https://example.org/audit-existing", Content: "The original article is retained for an audit failure test."}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.NoError(t, h.db.Exec(`CREATE FUNCTION fail_ingest_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER fail_ingest_audit BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION fail_ingest_audit()`).Error)
	request.CollectReason = "不应提交"
	_, err = h.service.Ingest(h.ctx, request)
	require.ErrorContains(t, err, "audit unavailable")
	require.Empty(t, h.article(t, first.ArticleID).CollectReason)
	request.URL = "https://example.org/audit-new"
	request.Content = "A totally distinct second paper studies different physical properties and their relationships."
	request.Images = []types.IngestImage{{Data: strings.SplitN(articleImageDataURI(t, 14), ",", 2)[1]}}
	_, err = h.service.Ingest(h.ctx, request)
	require.ErrorContains(t, err, "audit unavailable")
	var count int64
	require.NoError(t, h.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.Len(t, h.queue.tasks, 1)
	files := h.service.fileSvc.(*articleProcessingFiles)
	require.Len(t, files.saved, 1, "the original first article remains; failed collection files are removed")
}

func TestAssembleIngestBodyExactFormat(t *testing.T) {
	request := &types.IngestRequest{Content: "主内容", Thread: []types.IngestThreadItem{{Author: "作者 A", PublishedAt: "2026-10-01", Content: "推文内容"}}, Quotes: []types.IngestQuote{{Content: "引用一\n引用二"}}, Images: []types.IngestImage{{Caption: "图说明", Text: "图中文字"}}}
	want := "主内容\n\n## 推文串\n\n### 推文 1\n\n作者：作者 A\n\n时间：2026-10-01\n\n推文内容\n\n## 引用\n\n### 引用 1\n\n> 引用一\n> 引用二\n\n## 图片\n\n### 图片 1\n\n![图说明](<local://image.png>)\n\n说明：图说明\n\n图片文字：\n\n> 图中文字\n"
	require.Equal(t, want, assembleIngestBody(request, []string{"local://image.png"}))
	var typed types.IngestRequest
	require.NoError(t, json.Unmarshal([]byte(`{"url":"https://x.com/a/1","content":"Original","thread":[{"author":"Ada","published_at":"2026-10-01","content":"Thread"}],"quotes":[{"content":"Quote","author":"Grace","url":"https://example.org/q"}]}`), &typed))
	require.Equal(t, "2026-10-01", typed.Thread[0].PublishedAt)
}
