package rerank

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
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
