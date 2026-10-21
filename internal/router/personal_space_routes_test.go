package router

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"reliquary/internal/types"

	"github.com/stretchr/testify/require"
)

func TestLibraryRoutesSupportZeroLibrariesAndCustomLibrarySettings(t *testing.T) {
	f := newPersonalAuthFixture(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID)
	libraries, err := f.params.LibraryService.ListLibraries(ctx)
	require.NoError(t, err)
	for _, library := range libraries {
		response := personalRequest(t, f.router, http.MethodDelete, "/api/v1/libraries/"+library.ID, "", "", false)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
	empty := personalRequest(t, f.router, http.MethodGet, "/api/v1/library", "", "", false)
	require.Equal(t, http.StatusOK, empty.Code, empty.Body.String())
	require.JSONEq(t, `{"success":true,"data":null}`, empty.Body.String())
	created := personalRequest(t, f.router, http.MethodPost, "/api/v1/libraries", `{"name":"New notes","directory":"notes","template":"own"}`, "", false)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	settings := personalRequest(t, f.router, http.MethodGet, "/api/v1/library", "", "", false)
	require.Equal(t, http.StatusOK, settings.Code, settings.Body.String())
	require.Contains(t, settings.Body.String(), `"name":"New notes"`)
	updated := personalRequest(t, f.router, http.MethodPut, "/api/v1/library", `{"name":"Updated notes"}`, "", false)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.Contains(t, updated.Body.String(), `"name":"Updated notes"`)
}

func TestPersonalSpaceRoutesUseAuthenticatedSpace(t *testing.T) {
	f := newPersonalAuthFixture(t)
	paths := []string{"/api/v1/space", "/api/v1/space/api-keys", "/api/v1/space/kv/retrieval-config"}
	for _, path := range paths {
		response := personalRequest(t, f.router, http.MethodGet, path, "", "invalid-key", true)
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
