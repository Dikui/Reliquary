package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"reliquary/internal/types"

	"github.com/stretchr/testify/require"
)

func TestPersonalSpaceRoutesUseAuthenticatedSpace(t *testing.T) {
	f := newPersonalAuthFixture(t)
	paths := []string{"/api/v1/space", "/api/v1/space/api-keys", "/api/v1/space/kv/retrieval-config"}
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
		removed := personalRequest(t, f.router, http.MethodGet, "/api/v1/space/kv/parser-engine-config", "", actor.credential, actor.apiKey)
		require.Equal(t, http.StatusNotFound, removed.Code)
		updated := personalRequest(t, f.router, http.MethodPut, "/api/v1/space", `{"name":"My space","description":"Personal library"}`, actor.credential, actor.apiKey)
		require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
		var payload struct {
			Data types.Space `json:"data"`
		}
		require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &payload))
		require.Equal(t, types.SingleSpaceID, payload.Data.ID)
		require.Equal(t, "My space", payload.Data.Name)
		require.NotContains(t, updated.Body.String(), "storage_quota")
		kv := personalRequest(t, f.router, http.MethodPut, "/api/v1/space/kv/retrieval-config", `{}`, actor.credential, actor.apiKey)
		require.Equal(t, http.StatusOK, kv.Code, kv.Body.String())
		tiers := personalRequest(t, f.router, http.MethodPut, "/api/v1/library", `{"name":"库","model_tiers":{}}`, actor.credential, actor.apiKey)
		require.Equal(t, http.StatusOK, tiers.Code, tiers.Body.String())
	}
}

func TestPersonalOldManagementRoutesReturnNotFound(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for _, path := range []string{
		"/api/v1/space/model-tiers", "/api/v1/model-routing", "/api/v1/spaces/1", "/api/v1/spaces/1/api-keys", "/api/v1/spaces/1/api-keys/1", "/api/v1/spaces/kv/retrieval-config", "/api/v1/spaces/model-tiers",
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

func TestPersonalLibraryExistenceGuardReturnsNotFound(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/libraries/missing/entries", ""},
	} {
		response := personalRequest(t, f.router, route.method, route.path, route.body, f.token, false)
		require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), "library not found")
	}
}
