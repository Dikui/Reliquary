package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/embedding"
	"reliquary/internal/models/rerank"
	"reliquary/internal/sourceloc"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
	"reliquary/internal/utils/ollama"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type ikb08Models struct {
	interfaces.ModelService
	embedder embedding.Embedder
	ranker   rerank.Reranker
}

func (m ikb08Models) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.embedder, nil
}

func (m ikb08Models) GetRerankModel(context.Context, string) (rerank.Reranker, error) {
	return m.ranker, nil
}

func TestIKB08RequiredLibrariesAndCandidateScope(t *testing.T) {
	h := newEntrySearchFixture(t)
	_, err := h.svc.Search(h.ctx, types.EntrySearchRequest{Query: "searchfixture"})
	require.ErrorContains(t, err, "libraries")

	other := &types.Library{ID: "other", SpaceID: 1, Name: "Other", Directory: "other", Template: types.TemplateOwn, IndexingStrategy: h.library.IndexingStrategy}
	require.NoError(t, h.db.Create(other).Error)
	for i := 0; i < 65; i++ {
		h.add(t, fmt.Sprintf("distractor-%03d", i), "article", "2026-10-01", "event", "semanticlaunch", 1, other.ID)
	}
	for i := 0; i < 7; i++ {
		h.add(t, fmt.Sprintf("selected-%d", i), "article", "2026-10-01", "event", "semanticlaunch", 1)
	}
	for _, query := range []string{"semanticlaunch", "searchfixture"} {
		response, err := h.svc.Search(
			h.ctx,
			types.EntrySearchRequest{Query: query, Libraries: []string{"chosen"}, Limit: 7},
		)
		require.NoError(t, err)
		require.Len(t, response.Results, 7)
		for _, hit := range response.Results {
			require.Equal(t, h.library.ID, hit.LibraryID)
			require.Equal(t, "chosen", hit.LibraryDirectory)
		}
	}
	response, err := h.svc.Search(
		h.ctx,
		types.EntrySearchRequest{Query: "semanticlaunch", Libraries: []string{h.library.ID, other.ID}, Limit: 100},
	)
	require.NoError(t, err)
	require.Len(t, response.Results, 72)
	_, err = h.svc.Search(h.ctx, types.EntrySearchRequest{Query: "q", Libraries: []string{"unknown"}})
	require.Error(t, err)
}

func TestIKB08SearchRerankHTTPAndFallback(t *testing.T) {
	h := newEntrySearchFixture(t)
	a := h.add(t, "a", "article", "2026-10-01", "event", "semanticlaunch", 1)
	h.add(t, "b", "article", "2026-10-01", "event", "semanticlaunch", 1)
	body := "# 来源 😀\n\nsearchfixture semanticlaunch body passage 0"
	var c types.Chunk
	require.NoError(t, h.db.First(&c, "id=?", "a-chunk-000").Error)
	c.StartAt = len([]rune("# 来源 😀\n\n"))
	c.EndAt = len([]rune(body))
	sourceloc.Assign(body, []*types.Chunk{&c})
	require.NoError(t, h.db.Save(&c).Error)
	require.NoError(t, h.db.Model(a).Update("vault_body", body).Error)
	response, err := h.svc.Search(h.ctx, types.EntrySearchRequest{Query: "semanticlaunch", Libraries: []string{h.library.ID}})
	require.NoError(t, err)
	require.Equal(t, "unconfigured", response.RerankStatus)
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	mode := "ok"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == "error" {
			http.Error(w, "offline", 503)
			return
		}
		if mode == "timeout" {
			time.Sleep(300 * time.Millisecond)
			return
		}
		var input rerank.RerankRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
		require.Equal(t, 2, input.TopN)
		require.Equal(t, "/v1/rerank", r.URL.Path)
		_, _ = w.Write([]byte(`{"results":[{"index":0,"relevance_score":0.1},{"index":1,"relevance_score":0.9}]}`))
	}))
	defer server.Close()
	ranker, err := rerank.NewOpenAIReranker(&rerank.RerankerConfig{BaseURL: server.URL + "/v1", ModelName: "stub"})
	require.NoError(t, err)
	h.library.ModelTiers.Rerank = "local-rerank"
	require.NoError(t, h.db.Save(h.library).Error)
	svc := NewSearchService(entrySearchFixtureBases{library: h.library}, nil, ikb08Models{ranker: ranker}, h.db)
	response, err = svc.Search(
		h.ctx,
		types.EntrySearchRequest{Query: "semanticlaunch", Libraries: []string{h.library.ID}},
	)
	require.NoError(t, err)
	require.Equal(t, "applied", response.RerankStatus)
	require.Equal(t, "b", response.Results[0].EntryID)
	require.NotEmpty(t, response.Results[1].Snippets[0].Locations)
	mode = "error"
	response, err = svc.Search(h.ctx, types.EntrySearchRequest{Query: "semanticlaunch", Libraries: []string{h.library.ID}})
	require.NoError(t, err)
	require.Equal(t, "unavailable", response.RerankStatus)
	require.NotEmpty(t, response.Results)
	mode = "timeout"
	ctx, cancel := context.WithTimeout(h.ctx, 200*time.Millisecond)
	defer cancel()
	// Use the rerank helper so the test timeout cannot cancel PostgreSQL recall.
	results, status := svc.(*searchService).rerankEntryResults(ctx, h.library, "query", response.Results)
	require.Equal(t, "unavailable", status)
	require.NotEmpty(t, results)
}

func TestIKB08OllamaUnavailableSearchAndRecovery(t *testing.T) {
	h := newEntrySearchFixture(t)
	h.add(t, "a", "article", "2026-10-01", "event", "semanticlaunch", 1)
	offline := true
	vectorJSON := "[1,0,0]"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline {
			http.Error(w, "offline", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen3-embedding:0.6b"}]}`))
		case "/api/embed":
			_, _ = w.Write([]byte(`{"embeddings":[` + vectorJSON + `]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("OLLAMA_BASE_URL", server.URL)
	local, err := ollama.GetOllamaService()
	require.NoError(t, err)
	embedder, err := embedding.NewEmbedder(embedding.Config{Source: types.ModelSourceLocal, ModelName: "qwen3-embedding:0.6b", ModelID: "fixture-model", Dimensions: 3}, nil, local)
	require.NoError(t, err)
	models := ikb08Models{embedder: embedder}
	bases := &libraryService{db: h.db, repo: repository.NewLibraryRepository(h.db), modelService: models}
	svc := NewSearchService(bases, nil, models, h.db)
	response, err := svc.Search(h.ctx, types.EntrySearchRequest{Query: "searchfixture", Libraries: []string{h.library.ID}})
	require.NoError(t, err)
	require.True(t, response.VectorUnavailable)
	require.Len(t, response.Results, 1)
	offline = false
	response, err = svc.Search(h.ctx, types.EntrySearchRequest{Query: "searchfixture", Libraries: []string{h.library.ID}})
	require.NoError(t, err)
	require.False(t, response.VectorUnavailable)
	for _, value := range []string{"[]", "[1,0]", "[0,0,0]"} {
		vectorJSON = value
		response, err = svc.Search(h.ctx, types.EntrySearchRequest{Query: "searchfixture", Libraries: []string{h.library.ID}})
		require.NoError(t, err)
		require.True(t, response.VectorUnavailable)
		require.Len(t, response.Results, 1)
	}
}
