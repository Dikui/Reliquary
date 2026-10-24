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
			request := &types.IngestRequest{
				Library:     "official",
				URL:         "https://example.org/no-network",
				Acquisition: "extracted",
				Publisher:   "Fixture Journal",
				Content: "# Original observations\n\nThe original experiment measures atmospheri" +
					"c pressure and reports its instrument settings.",
			}
			imageURL := "https://example.org/unprovided.png"
			if structured {
				request.Images = []types.IngestImage{{SourceURL: imageURL, Caption: "Original figure"}}
			} else {
				request.Content += "\n\n![figure](" + imageURL + ")"
			}
			result, err := h.service.Ingest(h.ctx, request)
			require.NoError(t, err)
			require.Equal(t, "accepted", result.Status)
			h.drain(t)
			article := h.article(t, result.EntryID)
			require.Equal(t, types.ParseStatusCompleted, article.ParseStatus, article.ErrorMessage)
			_, _, body := readVaultArticle(t, v, article)
			require.Contains(t, string(body), imageURL)
			require.Empty(t, h.processor.requests[0][1].Images)
			require.NotContains(t, h.processor.requests[0][0].Content, "content_type")
			require.NotContains(t, h.processor.requests[0][1].Content, "content_type_hint")

			require.Zero(t, transport.calls.Load())
		})
	}
}

func TestIngestOwnedFieldsSurviveBackgroundSnapshots(t *testing.T) {
	h, _ := newIngestHarness(t)
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "official", URL: "https://example.org/ownership", Acquisition: "model_written", Content: "A glacier observation records mass balance and physical sampling procedures."})
	require.NoError(t, err)
	article := h.article(t, result.EntryID)
	stale := *article
	stale.Acquisition = ""
	stale.IngestAPIKeyName = "stale"
	stale.Description = "background summary"
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, &stale))
	current := h.article(t, article.ID)
	require.Equal(t, "model_written", current.Acquisition)
	require.Equal(t, article.IngestAPIKeyName, current.IngestAPIKeyName)
	require.Equal(t, "background summary", strings.TrimSpace(current.Description))
}
