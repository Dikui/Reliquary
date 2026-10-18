package rerank

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"reliquary/internal/types"
)

func TestAliyunRerankerPreservesNativeRequestAndScores(t *testing.T) {
	withRerankSSRFWhitelist(t, "127.0.0.1")
	requests := make(chan AliyunRerankRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/services/rerank/text-rerank/text-rerank" {
			t.Errorf("unexpected rerank request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("X-Gateway") != "local" {
			t.Errorf("unexpected rerank headers: %v", r.Header)
		}
		var body AliyunRerankRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"results":[{"index":1,"document":{"text":"B"},"relevance_score":0.9},{"index":0,"document":{"text":"A"},"relevance_score":0.2}]},"usage":{"total_tokens":42}}`))
	}))
	defer server.Close()

	reranker, err := NewReranker(&RerankerConfig{
		Provider:      "aliyun",
		BaseURL:       server.URL + "/api/v1/services/rerank/text-rerank/text-rerank",
		ModelName:     "qwen3-rerank",
		APIKey:        "test-key",
		CustomHeaders: map[string]string{"X-Gateway": "local"},
	})
	if err != nil {
		t.Fatalf("NewReranker: %v", err)
	}
	got, err := reranker.Rerank(t.Context(), "query", []string{"A", "B"})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	want := []RankResult{
		{Index: 1, Document: DocumentInfo{Text: "B"}, RelevanceScore: 0.9},
		{Index: 0, Document: DocumentInfo{Text: "A"}, RelevanceScore: 0.2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rerank results = %v, want %v", got, want)
	}
	body := <-requests
	if body.Model != "qwen3-rerank" || body.Input.Query != "query" || !reflect.DeepEqual(body.Input.Documents, []string{"A", "B"}) {
		t.Fatalf("unexpected Aliyun rerank input: %+v", body)
	}
	if !body.Parameters.ReturnDocuments || body.Parameters.TopN != 2 {
		t.Fatalf("unexpected Aliyun rerank parameters: %+v", body.Parameters)
	}
}

func TestAliyunMultimodalRerankerPreservesImageAndScores(t *testing.T) {
	withRerankSSRFWhitelist(t, "127.0.0.1")
	const image = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aJ1sAAAAASUVORK5CYII="
	documents := []RerankDocument{{Text: "A"}, {Image: image}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rerank" || r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("X-Gateway") != "local" {
			t.Errorf("unexpected multimodal request method, path or headers")
		}
		var body struct {
			Model string `json:"model"`
			Input struct {
				Query     string           `json:"query"`
				Documents []RerankDocument `json:"documents"`
			} `json:"input"`
			Parameters AliyunRerankParameters `json:"parameters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if body.Model != "qwen3-vl-rerank" || body.Input.Query != "query" || !reflect.DeepEqual(body.Input.Documents, documents) {
			t.Error("multimodal model, query or original image changed")
		}
		if body.Parameters.ReturnDocuments || body.Parameters.TopN != 2 {
			t.Errorf("unexpected multimodal parameters: %+v", body.Parameters)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"results":[{"index":1,"relevance_score":0.95},{"index":0,"relevance_score":0.15}]},"usage":{"total_tokens":42}}`))
	}))
	defer server.Close()
	r, err := NewReranker(&RerankerConfig{
		Provider: "aliyun", BaseURL: server.URL + "/rerank", ModelName: "qwen3-vl-rerank", ModelID: "multimodal-test",
		APIKey: "test-key", CustomHeaders: map[string]string{"X-Gateway": "local"}, MaxConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Exercise both wrappers independently of the process-wide debug log setting.
	governed := r.(*governedReranker)
	governed.inner = &debugReranker{inner: governed.inner}
	multimodal, ok := r.(MultimodalReranker)
	if !ok || !multimodal.SupportsImages() {
		t.Fatal("factory wrappers lost image support")
	}
	ctx, collector := types.WithModelCallCollector(t.Context())
	got, err := multimodal.RerankMultimodal(ctx, "query", documents)
	if err != nil {
		t.Fatal(err)
	}
	want := []RankResult{{Index: 1, RelevanceScore: 0.95}, {Index: 0, RelevanceScore: 0.15}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("multimodal results = %+v, want %+v", got, want)
	}
	calls := collector.Snapshot()
	if len(calls) != 1 || calls[0].Calls != 1 || calls[0].Tier != types.ModelTierRerank || calls[0].ModelID != "multimodal-test" {
		t.Fatalf("multimodal call accounting = %+v", calls)
	}
	if previews := strings.Join(multimodalDocumentPreviews(documents), " "); strings.Contains(previews, image) || !strings.Contains(previews, "image data URI:") {
		t.Fatal("multimodal debug preview must describe images without their payload")
	}
}

func TestAliyunMultimodalRerankerRejectsTextModel(t *testing.T) {
	r, err := NewReranker(&RerankerConfig{Provider: "aliyun", ModelName: "qwen3-rerank"})
	if err != nil {
		t.Fatal(err)
	}
	multimodal, ok := r.(MultimodalReranker)
	if !ok || multimodal.SupportsImages() {
		t.Fatal("text model unexpectedly supports images")
	}
	if _, err := multimodal.RerankMultimodal(t.Context(), "query", []RerankDocument{{Image: "data:image/png;base64,aGVsbG8="}}); err == nil || !strings.Contains(err.Error(), "does not support images") {
		t.Fatalf("unsupported model error = %v", err)
	}
}
