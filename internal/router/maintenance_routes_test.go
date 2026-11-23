package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/handler"
)

func TestMaintenanceWritePolicyCoversRegisteredMutationRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("LOCAL_STORAGE_BASE_DIR", t.TempDir())
	routes := NewRouter(RouterParams{
		MaintenanceHandler:      &handler.MaintenanceHandler{},
		EntityHandler:           &handler.EntityHandler{},
		VaultHandler:            &handler.VaultHandler{},
		GraphReplicaHandler:     &handler.GraphReplicaHandler{},
		GraphSearchHandler:      &handler.GraphSearchHandler{},
		EvaluationHandler:       &handler.EvaluationHandler{},
		TopicHandler:            &handler.TopicHandler{},
		FollowHandler:           &handler.FollowHandler{},
		ContentCheckHandler:     &handler.ContentCheckHandler{},
		EmbeddingRebuildHandler: &handler.EmbeddingRebuildHandler{},
		ChatGPTHandler:          &handler.ChatGPTHandler{},
		ChatGPTPolicyHandler:    &handler.ChatGPTPolicyHandler{},
		AgentModelHandler:       &handler.AgentModelHandler{},
		SystemHandler:           &handler.SystemHandler{},
	}).Routes()
	allowed := []string{
		"POST /api/v1/ingest/check", "POST /api/v1/entries/batches/preview", "POST /api/v1/chunker/preview",
		"POST /api/v1/search", "POST /api/v1/graph/search", "POST /api/v1/libraries/:id/hybrid-search",
		"POST /api/v1/libraries/:id/shelf/query", "POST /api/v1/libraries/:id/shelf/export",
		"POST /api/v1/libraries/:id/shelf/imports/inspect", "POST /api/v1/follows/feed",
		"POST /api/v1/models", "PUT /api/v1/models/:id", "DELETE /api/v1/models/:id", "POST /api/v1/models/:id/debug",
		"PUT /api/v1/models/:id/credentials", "DELETE /api/v1/models/:id/credentials/:field",
		"POST /api/v1/initialization/ollama/models/check", "POST /api/v1/initialization/ollama/models/download",
		"POST /api/v1/initialization/remote/check", "POST /api/v1/initialization/embedding/test", "POST /api/v1/initialization/rerank/check",
		"POST /api/v1/agent/models/:model_id/responses",
		"POST /api/v1/chatgpt/login", "POST /api/v1/chatgpt/accounts/:account_id/logout", "POST /api/v1/chatgpt/welcome/ack",
		"PUT /api/v1/chatgpt/policy", "POST /api/v1/chatgpt/resume",
		"POST /api/v1/space/api-keys", "PUT /api/v1/space/api-keys/:key_id", "DELETE /api/v1/space/api-keys/:key_id",
		"PUT /api/v1/space", "PUT /api/v1/space/kv/:key", "PUT /api/v1/system/settings/:key", "DELETE /api/v1/system/settings/:key",
		"POST /api/v1/vault/rebuild", "POST /api/v1/maintenance/graph/rebuild", "PUT /api/v1/maintenance/configure",
	}
	classified := append([]string{}, allowed...)
	// The library services classify these requests from their submitted fields.
	classified = append(classified, "PUT /api/v1/library", "PUT /api/v1/libraries/:id", "PATCH /api/v1/libraries/:id")
	for route := range businessWriteRoutes {
		classified = append(classified, route)
	}
	for route := range businessWriteOperations {
		classified = append(classified, route)
	}
	var registered []string
	for _, route := range routes {
		switch route.Method {
		case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
			registered = append(registered, route.Method+" "+route.Path)
		}
	}
	require.ElementsMatch(t, registered, classified)
}
