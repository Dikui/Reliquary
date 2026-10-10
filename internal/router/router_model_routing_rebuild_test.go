package router

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
)

type routingEmbeddingTrigger struct {
	calls  int
	err    error
	onMark func(context.Context, uint64, string)
}

func (r *routingEmbeddingTrigger) MarkAndEnqueue(ctx context.Context, tenantID uint64, reason string) error {
	r.calls++
	if r.onMark != nil {
		r.onMark(ctx, tenantID, reason)
	}
	return r.err
}
func (*routingEmbeddingTrigger) Enqueue(context.Context, uint64, string) error { return nil }

func TestGlobalEmbeddingConfigurationTriggersCommittedRebuildAndShowsQueueFailure(t *testing.T) {
	db := entityFixtureDB(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Global routing trigger"}).Error)
	require.NoError(t, db.Create(&types.Model{ID: "configured-vector", TenantID: 1, Name: "vector", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive}).Error)
	h := handler.NewModelRoutingHandler(db, nil)
	trigger := &routingEmbeddingTrigger{}
	h.SetNameEmbeddingRebuilder(trigger)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), entityIdentityMiddleware())
	engine.PUT("/api/v1/space/model-tiers", h.Update)
	trigger.onMark = func(_ context.Context, tenantID uint64, reason string) {
		require.Equal(t, uint64(1), tenantID)
		require.Equal(t, "global_embedding_profile_changed", reason)
		var team types.Tenant
		require.NoError(t, db.First(&team, "id = ?", 1).Error)
		require.Equal(t, "configured-vector", team.ModelTiers.Embedding)
	}
	response := entityRequest(t, engine, http.MethodPut, "/api/v1/space/model-tiers", `{"embedding":"configured-vector"}`, 1, entityOwnerID)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, trigger.calls)
	response = entityRequest(t, engine, http.MethodPut, "/api/v1/space/model-tiers", `{"embedding":"configured-vector"}`, 1, entityOwnerID)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, trigger.calls, "unchanged overrides do not schedule another rebuild")
	trigger.err = errors.New("redis queue unavailable")
	trigger.onMark = func(_ context.Context, _ uint64, _ string) {
		var team types.Tenant
		require.NoError(t, db.First(&team, "id = ?", 1).Error)
		require.Empty(t, team.ModelTiers.Embedding)
	}
	response = entityRequest(t, engine, http.MethodPut, "/api/v1/space/model-tiers", `{}`, 1, entityOwnerID)
	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "global settings saved")
	require.Equal(t, 2, trigger.calls)
}
