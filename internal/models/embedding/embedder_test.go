package embedding

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"reliquary/internal/models/provider"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

func TestAliyunEmbeddingUsesPresetDefaultWithOpenAIRequest(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,dashscope.aliyuncs.com")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/compatible-mode/v1/embeddings" {
			t.Errorf("unexpected embedding request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("X-Gateway") != "local" {
			t.Errorf("unexpected embedding headers: %v", r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2],"index":0}]}`))
	}))
	defer server.Close()

	embedder, err := newEmbedder(Config{
		Source:        types.ModelSourceRemote,
		Provider:      string(provider.ProviderAliyun),
		APIKey:        "test-key",
		ModelName:     "text-embedding-v3",
		Dimensions:    1024,
		CustomHeaders: map[string]string{"X-Gateway": "local"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("newEmbedder: %v", err)
	}
	compatible, ok := embedder.(*OpenAIEmbedder)
	if !ok {
		t.Fatalf("expected OpenAI-compatible embedder, got %T", embedder)
	}
	defaultURL := provider.GetOrDefault(provider.ProviderAliyun).Info().GetDefaultURL(types.ModelTypeEmbedding)
	if defaultURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" || compatible.baseURL != defaultURL {
		t.Fatalf("embedding default = %q, resolved URL = %q", defaultURL, compatible.baseURL)
	}

	// Keep the resolved upstream URL and send its request to a local HTTP server.
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	compatible.httpClient = &http.Client{Transport: embeddingRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "dashscope.aliyuncs.com" {
			t.Errorf("request did not use the preset address: %s", r.URL)
		}
		forwarded := r.Clone(r.Context())
		forwarded.URL.Scheme = target.Scheme
		forwarded.URL.Host = target.Host
		return server.Client().Transport.RoundTrip(forwarded)
	})}
	got, err := embedder.BatchEmbed(t.Context(), []string{"hello"})
	if err != nil {
		t.Fatalf("BatchEmbed: %v", err)
	}
	if !reflect.DeepEqual(got, [][]float32{{0.1, 0.2}}) {
		t.Fatalf("embedding = %v", got)
	}
	body := <-requests
	want := map[string]any{
		"model":                  "text-embedding-v3",
		"input":                  []any{"hello"},
		"encoding_format":        "float",
		"truncate_prompt_tokens": float64(511),
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("request body = %v, want %v", body, want)
	}
}

func TestSavedModelEmbeddingUsesGenericClientForRemovedProviders(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compatible-mode/v1/embeddings" {
			t.Errorf("unexpected embedding path: %s", r.URL.Path)
		}
		var body OpenAIEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.Model != "test-embedding" || !reflect.DeepEqual(body.Input, []string{"hello"}) {
			t.Errorf("unexpected OpenAI-compatible request: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2],"index":0}]}`))
	}))
	defer server.Close()

	for _, name := range []string{"azure_openai", "jina", "lkeap", "retired-provider", "aliyun", "zhipu", "siliconflow"} {
		t.Run(name, func(t *testing.T) {
			model := &types.Model{
				Name:   "test-embedding",
				Source: types.ModelSourceRemote,
				Parameters: types.ModelParameters{
					Provider: name,
					BaseURL:  server.URL + "/compatible-mode/v1/",
				},
			}
			embedder, err := NewEmbedder(ConfigFromModel(model), nil, nil)
			if err != nil {
				t.Fatalf("NewEmbedder: %v", err)
			}
			got, err := embedder.Embed(t.Context(), "hello")
			if err != nil {
				t.Fatalf("Embed: %v", err)
			}
			if !reflect.DeepEqual(got, []float32{0.1, 0.2}) {
				t.Fatalf("embedding = %v", got)
			}
		})
	}
}

type embeddingRoundTripFunc func(*http.Request) (*http.Response, error)

func (f embeddingRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
