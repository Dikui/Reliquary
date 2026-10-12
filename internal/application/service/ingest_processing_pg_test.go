package service

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

type ingestNoNetworkTransport struct {
	t     *testing.T
	calls atomic.Int64
}

func (r *ingestNoNetworkTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	r.calls.Add(1)
	r.t.Errorf("unexpected outbound content request: %s %s", request.Method, request.URL)
	return nil, errors.New("outbound content requests are forbidden in ingest tests")
}

func forbidIngestNetwork(t *testing.T, h *articleProcessingHarness) *ingestNoNetworkTransport {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "example.org")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	r := &ingestNoNetworkTransport{t: t}
	previous := http.DefaultTransport
	http.DefaultTransport = r
	t.Cleanup(func() { http.DefaultTransport = previous })
	return r
}

func TestIngestProcessingNeverFetchesRemoteImages(t *testing.T) {
	for _, structured := range []bool{false, true} {
		t.Run(map[bool]string{false: "markdown", true: "image_original_address"}[structured], func(t *testing.T) {
			h, v := newIngestHarness(t)
			transport := forbidIngestNetwork(t, h)
			request := &types.IngestRequest{URL: "https://example.org/no-network", Acquisition: "extracted", ContentTypeHint: "tutorial", Publisher: "Fixture Journal",
				Content: "# Original observations\n\nThe original experiment measures atmospheric pressure and reports its instrument settings."}
			imageURL := "https://example.org/unprovided.png"
			if structured {
				request.Images = []types.IngestImage{{SourceURL: imageURL, Caption: "Original figure"}}
				request.Missing = []types.IngestMissing{{Type: "image", SourceURL: imageURL, Reason: "来源拒绝提供图片文件"}}
			} else {
				request.Content += "\n\n![figure](" + imageURL + ")"
			}
			result, err := h.service.Ingest(h.ctx, request)
			require.NoError(t, err)
			require.Equal(t, "accepted", result.Status)
			h.drain(t)
			article := h.article(t, result.EntryID)
			require.Equal(t, types.ParseStatusCompleted, article.ParseStatus, article.ErrorMessage)
			require.True(t, article.Incomplete)
			require.Len(t, article.IngestMissing(), 1)
			require.Equal(t, imageURL, article.IngestMissing()[0].SourceURL)
			if !structured {
				require.Equal(t, "未提供图片文件", article.IngestMissing()[0].Reason)
			}
			_, _, body := readVaultArticle(t, v, article)
			require.Contains(t, string(body), imageURL)
			require.Empty(t, h.processor.requests[0][1].Images)
			require.Contains(t, h.processor.requests[0][0].Content, "仅作参考输入，不替代你的判断")
			require.Contains(t, h.processor.requests[0][1].Content, `"content_type_hint":"tutorial"`)
			require.Equal(t, "event", article.ContentType, "the model still determines the content type")
			require.Zero(t, transport.calls.Load())
		})
	}
}

func TestIngestContentTypeCorrectionRequestsBodyWithoutFetching(t *testing.T) {
	h, v := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	h.processor.output = validProcessingFixture("tutorial")
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/tutorial", Acquisition: "verbatim", Publisher: "Fixture Journal", Content: "# Instrument instructions\n\nCalibrate the pressure instrument, record its range, and verify readings using the published reference."})
	require.NoError(t, err)
	h.drain(t)
	article := h.article(t, result.EntryID)
	require.Equal(t, types.ProcessingModeSummaryOnly, article.ProcessingMode)
	queued := len(h.queue.tasks)
	_, err = h.service.ReparseEntry(h.ctx, article.ID, nil)
	require.NoError(t, err)
	require.True(t, h.article(t, article.ID).Incomplete)
	require.Len(t, h.queue.tasks, queued)
	calls := len(h.processor.requests)
	updated, err := h.service.CorrectContentType(h.ctx, article.ID, "research", false)
	require.NoError(t, err)
	require.True(t, updated.Incomplete)
	require.False(t, updated.ExternalCompletionAttempted)
	require.Equal(t, types.ProcessingModeFull, updated.ProcessingMode)
	require.Len(t, updated.IngestMissing(), 1)
	require.Equal(t, "content", updated.IngestMissing()[0].Type)
	require.Equal(t, "修正为全文保存，需要正文", updated.IngestMissing()[0].Reason)
	require.Equal(t, calls, len(h.processor.requests))
	require.Zero(t, transport.calls.Load())
	waitCorrectionHeader(t, v, updated, "research")
	_, _, body := readVaultArticle(t, v, updated)
	require.Empty(t, body)
	h.processor.output = validProcessingFixture("research")
	fullBody := "# Instrument instructions\n\nCalibrate the pressure instrument, record its range, and verify readings using the published reference."
	supplemented, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/tutorial", Acquisition: "verbatim", Publisher: "Fixture Journal", Content: fullBody})
	require.NoError(t, err)
	require.Equal(t, "supplemented", supplemented.Status)
	require.Empty(t, supplemented.Completeness)
	h.drain(t)
	final := h.article(t, article.ID)
	require.False(t, final.Incomplete)
	require.Equal(t, types.ParseStatusCompleted, final.ParseStatus)
	require.Equal(t, calls+1, len(h.processor.requests))
	_, header, saved := readVaultArticle(t, v, final)
	require.Contains(t, string(header), "content_type: \"research\"")
	require.Equal(t, fullBody+"\n", string(saved))
	require.Zero(t, transport.calls.Load())
}

func TestIngestOwnedFieldsSurviveBackgroundSnapshots(t *testing.T) {
	h, _ := newIngestHarness(t)
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/ownership", Acquisition: "model_written", Content: "A glacier observation records mass balance and physical sampling procedures.", Missing: []types.IngestMissing{{Type: "file", SourceURL: "https://example.org/original.pdf", Reason: "原始文件暂不可用"}}})
	require.NoError(t, err)
	article := h.article(t, result.EntryID)
	stale := *article
	stale.Incomplete, stale.ExternalCompletionAttempted, stale.Acquisition = false, true, ""
	stale.MissingMaterials, stale.IngestAPIKeyName = types.JSON(`[]`), "stale"
	stale.Description = "background summary"
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, &stale))
	current := h.article(t, article.ID)
	require.True(t, current.Incomplete)
	require.False(t, current.ExternalCompletionAttempted)
	require.Equal(t, "model_written", current.Acquisition)
	require.Equal(t, article.IngestAPIKeyName, current.IngestAPIKeyName)
	require.Equal(t, article.MissingMaterials, current.MissingMaterials)
	require.Equal(t, "background summary", strings.TrimSpace(current.Description))
}
