package service

import (
	"errors"
	"net/http"
	"reliquary/internal/types"
	"reliquary/internal/utils"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
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
			request := &types.IngestRequest{Library: "institutional", URL: "https://example.org/no-network", Acquisition: "extracted", ContentTypeHint: "tutorial", Publisher: "Fixture Journal",
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

func TestIngestOwnedFieldsSurviveBackgroundSnapshots(t *testing.T) {
	h, _ := newIngestHarness(t)
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "institutional", URL: "https://example.org/ownership", Acquisition: "model_written", Content: "A glacier observation records mass balance and physical sampling procedures.", Missing: []types.IngestMissing{{Type: "image", SourceURL: "https://example.org/figure.png", Reason: "图片暂不可用"}}})
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
