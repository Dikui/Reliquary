package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func newMultimodalTestEmbedder(t *testing.T, handler http.HandlerFunc) Embedder {
	t.Helper()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	model := &types.Model{ID: "shared-space", Name: "clip-fixture", Source: types.ModelSourceRemote,
		Parameters: types.ModelParameters{BaseURL: server.URL, APIKey: "fixture-key",
			EmbeddingParameters: types.EmbeddingParameters{Dimension: 3, SupportsImages: true, MultimodalFormat: "jina"}}}
	embedder, err := NewEmbedder(ConfigFromModel(model), nil, nil)
	require.NoError(t, err)
	return embedder
}

func TestMultimodalEmbeddingUsesJinaInputsAndSharedModel(t *testing.T) {
	requests := []map[string]any{}
	embedder := newMultimodalTestEmbedder(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/embeddings", r.URL.Path)
		require.Equal(t, "Bearer fixture-key", r.Header.Get("Authorization"))
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		if len(request["input"].([]any)) == 2 {
			_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[0,1,0]},{"index":0,"embedding":[1,0,0]}]}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0,0]}]}`))
		}
	})
	images, ok := embedder.(ImageEmbedder)
	require.True(t, ok)
	require.True(t, images.SupportsImages(), "concurrency decorator keeps image capability")
	vectors, err := embedder.BatchEmbed(context.Background(), []string{"first", "second"})
	require.NoError(t, err)
	require.Equal(t, [][]float32{{1, 0, 0}, {0, 1, 0}}, vectors, "response indexes preserve text input order")
	const dataURI = "data:image/png;base64,iVBORw0KGgo="
	vector, err := images.EmbedImage(context.Background(), dataURI)
	require.NoError(t, err)
	require.Equal(t, vectors[0], vector)
	require.Equal(t, "shared-space", embedder.GetModelID())
	require.Equal(t, "clip-fixture", requests[0]["model"])
	require.Equal(t, requests[0]["model"], requests[1]["model"])
	require.Equal(t, []any{map[string]any{"text": "first"}, map[string]any{"text": "second"}}, requests[0]["input"])
	require.Equal(t, []any{map[string]any{"image": dataURI}}, requests[1]["input"])
	require.NotContains(t, requests[0], "dimensions", "fixed dimension models retain their default dimensions")
	debug := &debugEmbedder{inner: embedder}
	require.True(t, debug.SupportsImages())
	_, err = debug.EmbedImage(context.Background(), dataURI)
	require.NoError(t, err)
}

func TestMultimodalEmbeddingRejectsInvalidResponseAndInput(t *testing.T) {
	for _, test := range []struct {
		name, body, message string
		status              int
	}{
		{"http", `{"error":"unavailable"}`, "HTTP 503", http.StatusServiceUnavailable},
		{"json", `not json`, "decode", http.StatusOK},
		{"missing", `{"data":[]}`, "count mismatch", http.StatusOK},
		{"dimension", `{"data":[{"index":0,"embedding":[1,0]}]}`, "dimension mismatch", http.StatusOK},
		{"index", `{"data":[{"index":1,"embedding":[1,0,0]}]}`, "invalid or duplicate index", http.StatusOK},
		{"zero", `{"data":[{"index":0,"embedding":[0,0,0]}]}`, "zero length", http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			embedder := newMultimodalTestEmbedder(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			})
			_, err := embedder.(ImageEmbedder).EmbedImage(context.Background(), "data:image/png;base64,iVBORw0KGgo=")
			require.ErrorContains(t, err, test.message)
		})
	}
	embedder := newMultimodalTestEmbedder(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid image must not call upstream") })
	for _, image := range []string{"https://example.org/image.png", "data:text/plain;base64,eA==", "data:image/png;base64,??", "data:image/png;base64,"} {
		_, err := embedder.(ImageEmbedder).EmbedImage(context.Background(), image)
		require.Error(t, err)
	}
	_, err := embedder.Embed(context.Background(), " ")
	require.ErrorContains(t, err, "must not be empty")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = embedder.Embed(ctx, "cancelled")
	require.ErrorIs(t, err, context.Canceled)
}

func TestMultimodalEmbeddingConfigurationRejectsUnsupportedTransport(t *testing.T) {
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	_, err := NewEmbedder(Config{Source: types.ModelSourceLocal, SupportsImages: true}, nil, nil)
	require.ErrorContains(t, err, "remote HTTP")
	_, err = NewEmbedder(Config{Source: types.ModelSourceRemote, SupportsImages: true, MultimodalFormat: "unknown"}, nil, nil)
	require.ErrorContains(t, err, "unsupported multimodal")
	e, err := NewOpenAIEmbedder("", "https://example.org/v1", "text-only", 0, 3, "text", nil)
	require.NoError(t, err)
	_, err = e.EmbedImage(context.Background(), "data:image/png;base64,eA==")
	require.ErrorContains(t, err, "does not support")
}
