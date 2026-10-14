package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/graphreplica"
	"reliquary/internal/middleware"
	"reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestGraphRebuildRequiresWebUserAndEnqueuesDurableOperation(t *testing.T) {
	db := pgtest.Open(t)
	client := queue.NewClientWithRedis(db, asynq.RedisClientOpt{Addr: "127.0.0.1:19398", DB: 9})
	defer client.Close()
	h := NewGraphReplicaHandler(graphreplica.NewService(db, nil, client))
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.POST("/maintenance/graph/rebuild", h.Rebuild)
	for _, principal := range []types.Principal{{}, {Type: types.PrincipalAPIKey, ID: "caller"}, {Type: types.PrincipalWebUser, ID: "owner"}} {
		req := httptest.NewRequest(http.MethodPost, "/maintenance/graph/rebuild", nil).WithContext(types.WithPrincipal(context.Background(), principal))
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if principal.Type != types.PrincipalWebUser {
			require.Equal(t, http.StatusForbidden, res.Code)
			continue
		}
		require.Equal(t, http.StatusAccepted, res.Code, res.Body.String())
		var value struct {
			Data struct {
				OperationID string `json:"operation_id"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &value))
		require.NotEmpty(t, value.Data.OperationID)
		var job queue.Job
		require.NoError(t, db.First(&job, "id=?", value.Data.OperationID).Error)
		require.Equal(t, graphreplica.TypeRebuild, job.TaskType)
		require.Equal(t, types.QueueMaintenance, job.Queue)
	}
}
