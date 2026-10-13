package rerank

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLocalRerankFormatsTopNAndTimeout(t *testing.T) {
	withRerankSSRFWhitelist(t, "127.0.0.1")
	for _, format := range []string{"results", "data"} {
		t.Run(format, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/rerank", r.URL.Path)
				var request RerankRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				require.Equal(t, 2, request.TopN)
				require.Equal(t, "question", request.Query)
				_, _ = w.Write([]byte(`{"` + format + `":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.1}]}`))
			}))
			defer server.Close()
			model, err := NewOpenAIReranker(&RerankerConfig{BaseURL: server.URL + "/v1", ModelName: "local"})
			require.NoError(t, err)
			ranks, err := model.Rerank(t.Context(), "question", []string{"a", "b"})
			require.NoError(t, err)
			require.Equal(t, 1, ranks[0].Index)
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer server.Close()
	model, err := NewOpenAIReranker(&RerankerConfig{BaseURL: server.URL + "/v1"})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = model.Rerank(ctx, "q", []string{"a"})
	require.Error(t, err)
}
