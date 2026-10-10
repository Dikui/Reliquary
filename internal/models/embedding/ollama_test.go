package embedding

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"reliquary/internal/models/utils/ollama"
	"reliquary/internal/types"
)

func TestOllamaEmbeddingPreservesLocalAPI(t *testing.T) {
	for _, override := range []bool{false, true} {
		name := "default-dimensions"
		if override {
			name = "override-dimensions"
		}
		t.Run(name, func(t *testing.T) {
			requests := make(chan map[string]any, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/":
					w.WriteHeader(http.StatusOK)
				case "/api/tags":
					_, _ = w.Write([]byte(`{"models":[{"name":"nomic-embed-text:latest"}]}`))
				case "/api/embed":
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode request: %v", err)
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					requests <- body
					_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2],[0.3,0.4]]}`))
				default:
					t.Errorf("unexpected Ollama path: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			t.Setenv("OLLAMA_BASE_URL", server.URL)
			t.Setenv("OLLAMA_OPTIONAL", "false")
			service, err := ollama.GetOllamaService()
			if err != nil {
				t.Fatal(err)
			}
			embedder, err := NewEmbedder(Config{
				Source:                    types.ModelSourceLocal,
				ModelName:                 "nomic-embed-text",
				Dimensions:                256,
				SupportsDimensionOverride: override,
				TruncatePromptTokens:      512,
			}, nil, service)
			if err != nil {
				t.Fatalf("NewEmbedder: %v", err)
			}
			got, err := embedder.BatchEmbed(t.Context(), []string{"hello", "world"})
			if err != nil {
				t.Fatalf("BatchEmbed: %v", err)
			}
			if !reflect.DeepEqual(got, [][]float32{{0.1, 0.2}, {0.3, 0.4}}) {
				t.Fatalf("embeddings = %v", got)
			}
			body := <-requests
			if body["model"] != "nomic-embed-text" || !reflect.DeepEqual(body["input"], []any{"hello", "world"}) {
				t.Fatalf("unexpected Ollama input: %v", body)
			}
			if body["truncate"] != true || !reflect.DeepEqual(body["options"], map[string]any{"num_ctx": float64(512)}) {
				t.Fatalf("unexpected Ollama truncation parameters: %v", body)
			}
			if override {
				if body["dimensions"] != float64(256) {
					t.Fatalf("dimensions = %v, want 256", body["dimensions"])
				}
			} else if _, ok := body["dimensions"]; ok {
				t.Fatalf("dimensions must be omitted by default: %v", body)
			}
		})
	}
}
