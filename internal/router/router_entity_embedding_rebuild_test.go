package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
)

func TestEntityNameEmbeddingAdministratorStatusAndTrigger(t *testing.T) {
	db := entityFixtureDB(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Embedding lifecycle"}).Error)
	require.NoError(t, db.Create(&types.Library{ID: "entity-library", SpaceID: 1, Name: "Entity library"}).Error)
	entity := types.Entity{ID: "embedding-pending", SpaceID: 1, LibraryID: "entity-library", CanonicalName: "名称", NormalizedName: "名称", EntityType: "product", Description: "模型说明", EmbeddingNeedsRebuild: true}
	require.NoError(t, db.Create(&entity).Error)
	queue := &entityTaskQueue{}
	rebuilder := service.NewEntityNameEmbeddingRebuilder(db, nil, queue)
	entityHandler := handler.NewEntityHandler(service.NewEntityService(db, nil))
	entityHandler.SetNameEmbeddingRebuilder(rebuilder)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), entityIdentityMiddleware())
	RegisterEntityRoutes(engine.Group("/api/v1"), entityHandler)
	const endpoint = "/api/v1/entities/name-embeddings/rebuild"
	status := entityRequest(t, engine, http.MethodGet, endpoint, "", 1, entityOwnerID)
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	var envelope entityEnvelope
	require.NoError(t, json.Unmarshal(status.Body.Bytes(), &envelope))
	var data struct {
		PendingCount int `json:"pending_count"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	require.Equal(t, 1, data.PendingCount)
	trigger := entityRequest(t, engine, http.MethodPost, endpoint, "{}", 1, entityOwnerID)
	require.Equal(t, http.StatusOK, trigger.Code, trigger.Body.String())
	require.Len(t, queue.tasks, 1)
	require.Equal(t, types.TypeEntityNameEmbeddingRebuild, queue.tasks[0].Type())
	require.Error(t, rebuilder.Handle(entityContext(1, entityOwnerID), queue.tasks[0]))
	failed := entityRequest(t, engine, http.MethodGet, endpoint, "", 1, entityOwnerID)
	require.Equal(t, http.StatusOK, failed.Code, failed.Body.String())
	require.Contains(t, failed.Body.String(), `"status":"failed"`)
	require.Contains(t, failed.Body.String(), `"pending_count":1`)
}

func TestEntityDescriptionHTTPAllowsEmptyAndRejectsMissing(t *testing.T) {
	db := entityFixtureDB(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Entity editor"}).Error)
	require.NoError(t, db.Create(&types.Library{ID: "entity-library", SpaceID: 1, Name: "Entity library"}).Error)
	entity := types.Entity{ID: "description-object", SpaceID: 1, LibraryID: "entity-library", CanonicalName: "实体", NormalizedName: "实体", EntityType: "product", Description: "旧说明", EmbeddingNeedsRebuild: false}
	require.NoError(t, db.Create(&entity).Error)
	h := handler.NewEntityHandler(service.NewEntityService(db, nil))
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), entityIdentityMiddleware())
	engine.PATCH("/entities/:entity_id/description", h.UpdateEntityDescription)
	missing := entityRequest(t, engine, http.MethodPatch, "/entities/description-object/description", "{}", 1, entityOwnerID)
	require.Equal(t, http.StatusBadRequest, missing.Code)
	response := entityRequest(t, engine, http.MethodPatch, "/entities/description-object/description", `{"description":""}`, 1, entityOwnerID)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, db.First(&entity, "id=?", entity.ID).Error)
	require.Empty(t, entity.Description)
	require.True(t, entity.EmbeddingNeedsRebuild)
}
