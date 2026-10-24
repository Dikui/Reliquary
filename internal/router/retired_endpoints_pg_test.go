package router

import (
	"context"
	"net/http"
	"reliquary/internal/handler"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestRetiredEndpointsAreNotFound(t *testing.T) {
	t.Setenv("LOCAL_STORAGE_BASE_DIR", t.TempDir())
	f := newPersonalAuthFixture(t)
	f.params.MaintenanceHandler = &handler.MaintenanceHandler{}
	r := NewRouter(f.params)
	for _, endpoint := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/topics", ""},
		{http.MethodPost, "/api/v1/topics", `{}`},
		{http.MethodPut, "/api/v1/topics/missing", `{}`},
		{http.MethodDelete, "/api/v1/topics/missing", ""},
		{http.MethodGet, "/api/v1/topics/missing/backfill", ""},
		{http.MethodPost, "/api/v1/topics/missing/backfill/retry", `{}`},
		{http.MethodPut, "/api/v1/entries/missing/topics", `{}`},
		{http.MethodPut, "/api/v1/entries/missing/content-type", `{}`},
		{http.MethodGet, "/api/v1/diagnostics/content-checks", ""},
		{http.MethodGet, "/api/v1/diagnostics/content-checks/settings", ""},
		{http.MethodPut, "/api/v1/diagnostics/content-checks/settings", `{}`},
		{http.MethodPost, "/api/v1/diagnostics/content-checks", `{}`},
		{http.MethodGet, "/api/v1/diagnostics/content-checks/missing", ""},
		{http.MethodPost, "/api/v1/diagnostics/content-checks/missing/recheck", `{}`},
		{http.MethodGet, "/api/v1/entities/missing/relations", ""},
		{http.MethodDelete, "/api/v1/relations/missing", ""},
		{http.MethodPost, "/api/v1/libraries/missing/relation-types/merge", `{}`},
		{http.MethodPost, "/api/v1/graph/search", `{}`},
		{http.MethodPost, "/api/v1/maintenance/graph/rebuild", `{}`},
	} {
		t.Run(endpoint.method+endpoint.path, func(t *testing.T) {
			response := personalRequest(t, r, endpoint.method, endpoint.path, endpoint.body, f.key, true)
			require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
		})
	}
}

func TestRetiredToolsAreNotFound(t *testing.T) {
	f := newMCPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session := f.connect(t, "2025-03-26")
	listed, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	for _, name := range []string{
		"graph_search",
		"list_relations",
		"delete_relation",
		"merge_relation_types",
		"list_topics",
		"create_topic",
		"update_topic",
	} {
		for _, tool := range listed.Tools {
			require.NotEqual(t, name, tool.Name)
		}
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		require.ErrorContains(t, err, "unknown tool")
	}
}
