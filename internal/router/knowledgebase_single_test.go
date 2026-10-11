package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestSingleKnowledgeBaseSettingsAndRemovedRoutes(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for _, credential := range []struct {
		value  string
		apiKey bool
	}{{f.token, false}, {f.key, true}} {
		get := personalRequest(t, f.router, http.MethodGet, "/api/v1/knowledge-base", "", credential.value, credential.apiKey)
		require.Equal(t, http.StatusOK, get.Code, get.Body.String())
		var response struct {
			Data types.KnowledgeBase `json:"data"`
		}
		require.NoError(t, json.Unmarshal(get.Body.Bytes(), &response))
		require.NotEmpty(t, response.Data.ID)
		require.Equal(t, types.KnowledgeBaseTypeDocument, response.Data.Type)
		put := personalRequest(t, f.router, http.MethodPut, "/api/v1/knowledge-base", `{"name":"知识库","config":{"chunking_config":{"chunk_size":128,"chunk_overlap":16,"separators":["\n"]}}}`, credential.value, credential.apiKey)
		require.Equal(t, http.StatusOK, put.Code, put.Body.String())
		require.NoError(t, json.Unmarshal(put.Body.Bytes(), &response))
		require.Equal(t, 128, response.Data.ChunkingConfig.ChunkSize)
		require.Equal(t, 16, response.Data.ChunkingConfig.ChunkOverlap)
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/knowledge-bases"},
			{http.MethodPost, "/api/v1/knowledge-bases"},
			{http.MethodGet, "/api/v1/knowledge-bases/" + response.Data.ID},
			{http.MethodPut, "/api/v1/knowledge-bases/" + response.Data.ID},
			{http.MethodDelete, "/api/v1/knowledge-bases/" + response.Data.ID},
			{http.MethodPost, "/api/v1/knowledge-bases/" + response.Data.ID + "/duplicate"},
			{http.MethodGet, "/api/v1/knowledge-bases/" + response.Data.ID + "/move-targets"},
			{http.MethodPut, "/api/v1/knowledge-bases/" + response.Data.ID + "/pin"},
			{http.MethodGet, "/api/v1/knowledge-bases/" + response.Data.ID + "/knowledge/folders"},
			{http.MethodPut, "/api/v1/knowledge-bases/" + response.Data.ID + "/knowledge/folders"},
			{http.MethodPost, "/api/v1/knowledge/folder"},
			{http.MethodPost, "/api/v1/knowledge/move"},
			{http.MethodGet, "/api/v1/knowledge/move/progress/task"},
			{http.MethodGet, "/api/v1/user/favorites"},
			{http.MethodPost, "/api/v1/user/favorites"},
			{http.MethodDelete, "/api/v1/user/favorites/kb"},
		} {
			got := personalRequest(t, f.router, route.method, route.path, `{}`, credential.value, credential.apiKey)
			require.Equal(t, http.StatusNotFound, got.Code, route.method+" "+route.path+" "+got.Body.String())
		}
	}
}

func TestSingleKnowledgeBaseChildResourceMissingIDReturnsNotFound(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/knowledge-bases/missing/knowledge"},
		{http.MethodPost, "/api/v1/knowledge-bases/missing/knowledge/file"},
		{http.MethodPost, "/api/v1/knowledge-bases/missing/knowledge/url"},
		{http.MethodPost, "/api/v1/knowledge-bases/missing/knowledge/manual"},
		{http.MethodPost, "/api/v1/knowledge-bases/missing/hybrid-search"},
	} {
		got := personalRequest(t, f.router, route.method, route.path, `{}`, f.key, true)
		require.Equal(t, http.StatusNotFound, got.Code, got.Body.String())
		require.Contains(t, got.Body.String(), "knowledge base not found")
	}
}

func TestRetrievalAlgorithmSettingsDoNotCreateAnotherRerankModelBinding(t *testing.T) {
	f := newPersonalAuthFixture(t)
	require.NoError(t, f.db.Model(&types.KnowledgeBase{}).Where("tenant_id=?", types.SingleTenantID).Update("model_tiers", types.ModelTierConfig{Rerank: "configured-rerank"}).Error)
	saved := personalRequest(t, f.router, http.MethodPut, "/api/v1/space/kv/retrieval-config", `{"rerank_model_id":"ignored-second-binding","rerank_top_k":7,"rerank_threshold":0.4}`, f.token, false)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	require.NotContains(t, saved.Body.String(), "rerank_model_id")
	var stored string
	require.NoError(t, f.db.Raw("SELECT retrieval_config::text FROM tenants WHERE id=?", types.SingleTenantID).Scan(&stored).Error)
	require.NotContains(t, stored, "rerank_model_id")
	var settings types.RetrievalConfig
	require.NoError(t, json.Unmarshal([]byte(stored), &settings))
	require.Equal(t, 7, settings.RerankTopK)
	require.Equal(t, 0.4, settings.RerankThreshold)
	fetched := personalRequest(t, f.router, http.MethodGet, "/api/v1/knowledge-base", "", f.key, true)
	require.Equal(t, http.StatusOK, fetched.Code, fetched.Body.String())
	var response struct {
		Data types.KnowledgeBase `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fetched.Body.Bytes(), &response))
	require.Equal(t, "configured-rerank", response.Data.ModelTiers.Rerank)
}
