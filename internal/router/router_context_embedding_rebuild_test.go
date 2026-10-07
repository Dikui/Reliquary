package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
)

func TestContextNameEmbeddingAdministratorStatusAndTrigger(t *testing.T) {
	db := fusionFixtureDB(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Embedding lifecycle"}).Error)
	entity := types.KnowledgeEntity{ID: "embedding-pending", TenantID: 1, CanonicalName: "名称", NormalizedName: "名称", EntityType: "model", Description: "模型说明", EmbeddingNeedsRebuild: true}
	require.NoError(t, db.Create(&entity).Error)
	queue := &fusionTaskQueue{}
	rebuilder := service.NewContextNameEmbeddingRebuilder(db, nil, queue)
	contextHandler := handler.NewKnowledgeContextHandler(service.NewKnowledgeContextService(db, nil), nil)
	contextHandler.SetNameEmbeddingRebuilder(rebuilder)
	enabled := true
	guards := newRBACGuards(&config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}, nil, nil, nil, nil, nil, nil, nil, nil)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), fusionIdentityMiddleware())
	RegisterKnowledgeContextRoutes(engine.Group("/api/v1"), contextHandler, guards)
	const endpoint = "/api/v1/knowledge-context/name-embeddings/rebuild"
	for _, role := range []types.TenantRole{types.TenantRoleViewer, types.TenantRoleContributor} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := fusionRequest(t, engine, method, endpoint, "{}", 1, fusionOwnerID, role)
			require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		}
	}
	status := fusionRequest(t, engine, http.MethodGet, endpoint, "", 1, fusionOwnerID, types.TenantRoleAdmin)
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	var envelope fusionEnvelope
	require.NoError(t, json.Unmarshal(status.Body.Bytes(), &envelope))
	var data struct {
		PendingCount int `json:"pending_count"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	require.Equal(t, 1, data.PendingCount)
	trigger := fusionRequest(t, engine, http.MethodPost, endpoint, "{}", 1, fusionOwnerID, types.TenantRoleAdmin)
	require.Equal(t, http.StatusOK, trigger.Code, trigger.Body.String())
	require.Len(t, queue.tasks, 1)
	require.Equal(t, types.TypeContextNameEmbeddingRebuild, queue.tasks[0].Type())
	require.Error(t, rebuilder.Handle(fusionContext(1, fusionOwnerID, types.TenantRoleAdmin), queue.tasks[0]))
	failed := fusionRequest(t, engine, http.MethodGet, endpoint, "", 1, fusionOwnerID, types.TenantRoleAdmin)
	require.Equal(t, http.StatusOK, failed.Code, failed.Body.String())
	require.Contains(t, failed.Body.String(), `"status":"failed"`)
	require.Contains(t, failed.Body.String(), `"pending_count":1`)
}

func TestContextEntityDescriptionHTTPAllowsEmptyAndRejectsMissing(t *testing.T) {
	db := fusionFixtureDB(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Entity editor"}).Error)
	entity := types.KnowledgeEntity{ID: "description-object", TenantID: 1, CanonicalName: "对象", NormalizedName: "对象", EntityType: "model", Description: "旧说明", EmbeddingNeedsRebuild: false}
	require.NoError(t, db.Create(&entity).Error)
	h := handler.NewKnowledgeContextHandler(service.NewKnowledgeContextService(db, nil), nil)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), fusionIdentityMiddleware())
	engine.PATCH("/entities/:entity_id/description", h.UpdateEntityDescription)
	missing := fusionRequest(t, engine, http.MethodPatch, "/entities/description-object/description", "{}", 1, fusionOwnerID, types.TenantRoleContributor)
	require.Equal(t, http.StatusBadRequest, missing.Code)
	response := fusionRequest(t, engine, http.MethodPatch, "/entities/description-object/description", `{"description":""}`, 1, fusionOwnerID, types.TenantRoleContributor)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, db.First(&entity, "id=?", entity.ID).Error)
	require.Empty(t, entity.Description)
	require.True(t, entity.EmbeddingNeedsRebuild)
}
