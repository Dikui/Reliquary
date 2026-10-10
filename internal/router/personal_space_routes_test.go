package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestPersonalSpaceRoutesUseAuthenticatedSpace(t *testing.T) {
	f := newPersonalAuthFixture(t)
	paths := []string{"/api/v1/space", "/api/v1/space/api-keys", "/api/v1/space/kv/retrieval-config", "/api/v1/space/kv/parser-engine-config", "/api/v1/space/kv/prompt-templates", "/api/v1/space/model-tiers"}
	for _, path := range paths {
		response := personalRequest(t, f.router, http.MethodGet, path, "", "", false)
		require.Equal(t, http.StatusUnauthorized, response.Code, path+" "+response.Body.String())
	}
	for _, actor := range []struct {
		credential string
		apiKey     bool
	}{{f.token, false}, {f.key, true}} {
		for _, path := range paths {
			response := personalRequest(t, f.router, http.MethodGet, path, "", actor.credential, actor.apiKey)
			require.Equal(t, http.StatusOK, response.Code, path+" "+response.Body.String())
		}
		updated := personalRequest(t, f.router, http.MethodPut, "/api/v1/space", `{"name":"My space","description":"Personal library"}`, actor.credential, actor.apiKey)
		require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
		var payload struct {
			Data types.Tenant `json:"data"`
		}
		require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &payload))
		require.Equal(t, types.SingleTenantID, payload.Data.ID)
		require.Equal(t, "My space", payload.Data.Name)
		require.NotContains(t, updated.Body.String(), "storage_quota")
		kv := personalRequest(t, f.router, http.MethodPut, "/api/v1/space/kv/retrieval-config", `{}`, actor.credential, actor.apiKey)
		require.Equal(t, http.StatusOK, kv.Code, kv.Body.String())
		tiers := personalRequest(t, f.router, http.MethodPut, "/api/v1/space/model-tiers", `{}`, actor.credential, actor.apiKey)
		require.Equal(t, http.StatusOK, tiers.Code, tiers.Body.String())
	}
}

func TestPersonalOldManagementRoutesReturnNotFound(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for _, path := range []string{
		"/api/v1/tenants/1", "/api/v1/tenants/1/api-keys", "/api/v1/tenants/1/api-keys/1", "/api/v1/tenants/kv/retrieval-config", "/api/v1/tenants/model-tiers",
		"/api/v1/system/admin/settings", "/api/v1/system/admin/settings/file.max_size_mb", "/api/v1/system/admin/runtime/queues",
		"/api/v1/system/admin/runtime/queues/default/tasks", "/api/v1/system/admin/runtime/queues/default/archived",
		"/api/v1/system/admin/runtime/queues/default/tasks/task/actions/delete",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
			response := personalRequest(t, f.router, method, path, `{}`, f.token, false)
			require.Equal(t, http.StatusNotFound, response.Code, method+" "+path+" "+response.Body.String())
		}
	}
}

func TestPersonalKnowledgeBaseExistenceGuardReturnsNotFound(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/knowledge-bases/missing/knowledge", ""},
		{http.MethodPost, "/api/v1/knowledge-bases/missing/knowledge/manual", `{}`},
		{http.MethodPost, "/api/v1/knowledge-bases/missing/knowledge/url", `{}`},
	} {
		response := personalRequest(t, f.router, route.method, route.path, route.body, f.token, false)
		require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), "knowledge base not found")
	}
}
