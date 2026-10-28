package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type rebuildRouteModel struct {
	interfaces.ModelService
	embedder *rebuildRouteEmbedder
}

func (m rebuildRouteModel) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.embedder, nil
}

type rebuildRouteEmbedder struct{ fail bool }

func (e *rebuildRouteEmbedder) GetModelID() string   { return "http-embedding" }
func (e *rebuildRouteEmbedder) GetModelName() string { return "HTTP embedding" }
func (e *rebuildRouteEmbedder) GetDimensions() int   { return 2 }
func (e *rebuildRouteEmbedder) Embed(context.Context, string) ([]float32, error) {
	if e.fail {
		return nil, errors.New("provider offline")
	}
	return []float32{1, 0}, nil
}
func (e *rebuildRouteEmbedder) BatchEmbed(context.Context, []string) ([][]float32, error) {
	return nil, nil
}
func (e *rebuildRouteEmbedder) BatchEmbedWithPool(context.Context, embedding.Embedder, []string) ([][]float32, error) {
	return nil, nil
}

func TestEmbeddingRebuildHTTPProgressPauseResumeAndRetry(t *testing.T) {
	db := entityFixtureDB(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, EmbeddingModelID: "http-embedding", Name: "Embedding owner"}).Error)
	require.NoError(t, db.Create(&types.Library{ID: "http-library", SpaceID: 1, Name: "HTTP", ModelTiers: types.ModelTierConfig{Embedding: "http-embedding"}}).Error)
	require.NoError(t, db.Create(&types.Model{ID: "http-embedding", SpaceID: 1, Name: "HTTP embedding", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive,
		Parameters: types.ModelParameters{EmbeddingParameters: types.EmbeddingParameters{Dimension: 2}}}).Error)
	require.NoError(t, db.Create(&types.Entity{ID: "rebuild-http-entity", SpaceID: 1, LibraryID: "http-library", CanonicalName: "HTTP", NormalizedName: "http", EntityType: "product", EmbeddingNeedsRebuild: true}).Error)
	vault, err := service.NewVaultService(db, &config.Config{Vault: &config.VaultConfig{Root: t.TempDir()}})
	require.NoError(t, err)
	embedder := &rebuildRouteEmbedder{fail: true}
	queue := &entityTaskQueue{}
	rebuilder := service.NewEmbeddingRebuilder(db, rebuildRouteModel{embedder: embedder}, queue, vault)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), entityIdentityMiddleware())
	RegisterEmbeddingRebuildRoutes(engine.Group("/api/v1"), handler.NewEmbeddingRebuildHandler(rebuilder))
	const path = "/api/v1/embeddings/rebuild"
	request := func(method, body string) *types.EmbeddingRebuildStatus {
		response := entityRequest(t, engine, method, path, body, 1, entityOwnerID)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var envelope entityEnvelope
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
		var status types.EmbeddingRebuildStatus
		require.NoError(t, json.Unmarshal(envelope.Data, &status))
		return &status
	}
	require.Nil(t, request(http.MethodGet, "").LatestRun)
	start := request(http.MethodPost, `{"action":"start"}`)
	require.EqualValues(t, 1, start.LatestRun.TotalCount)
	require.Equal(t, "pending", start.LatestRun.Status)
	runID := start.LatestRun.ID
	paused := request(http.MethodPost, `{"action":"pause","run_id":"`+runID+`"}`)
	require.Equal(t, "paused", paused.LatestRun.Status)
	request(http.MethodPost, `{"action":"resume","run_id":"`+runID+`"}`)
	require.NoError(t, rebuilder.Handle(entityContext(1, entityOwnerID), queue.tasks[len(queue.tasks)-1]))
	failed := request(http.MethodGet, "")
	require.EqualValues(t, 1, failed.LatestRun.FailedCount)
	require.Equal(t, "completed_with_errors", failed.LatestRun.Status)
	require.Contains(t, failed.FailedItems[0].Error, "provider offline")
	embedder.fail = false
	request(http.MethodPost, `{"action":"retry_failed","run_id":"`+runID+`"}`)
	require.NoError(t, rebuilder.Handle(entityContext(1, entityOwnerID), queue.tasks[len(queue.tasks)-1]))
	completed := request(http.MethodGet, "")
	require.Equal(t, "completed", completed.LatestRun.Status)
	require.EqualValues(t, 1, completed.LatestRun.CompletedCount)
	require.Empty(t, completed.FailedItems)
	for _, body := range []string{`{}`, `{"action":"other"}`, `{"action":"resume"}`} {
		bad := entityRequest(t, engine, http.MethodPost, path, body, 1, entityOwnerID)
		require.Equal(t, http.StatusBadRequest, bad.Code, bad.Body.String())
	}
}
