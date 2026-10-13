package rerank

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"reliquary/internal/types"
)

func TestSavedModelRerankerUsesGenericClientForRemovedProviders(t *testing.T) {
	withRerankSSRFWhitelist(t, "127.0.0.1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/rerank" {
			t.Errorf("unexpected rerank request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		want := map[string]any{
			"top_n":     float64(2),
			"model":     "test-rerank",
			"query":     "query",
			"documents": []any{"A", "B"},
		}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("request body = %v, want %v", body, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.2}]}`))
	}))
	defer server.Close()

	for _, name := range []string{"azure_openai", "jina", "lkeap", "retired-provider", "zhipu", "volcengine", "siliconflow"} {
		t.Run(name, func(t *testing.T) {
			model := &types.Model{
				Name:   "test-rerank",
				Source: types.ModelSourceRemote,
				Parameters: types.ModelParameters{
					Provider: name,
					BaseURL:  server.URL + "/v1/",
				},
			}
			reranker, err := NewReranker(ConfigFromModel(model, ""))
			if err != nil {
				t.Fatalf("NewReranker: %v", err)
			}
			got, err := reranker.Rerank(t.Context(), "query", []string{"A", "B"})
			if err != nil {
				t.Fatalf("Rerank: %v", err)
			}
			if len(got) != 2 || got[0].Index != 1 || got[0].RelevanceScore != 0.9 || got[1].Index != 0 || got[1].RelevanceScore != 0.2 {
				t.Fatalf("rerank results = %v", got)
			}
		})
	}
}
