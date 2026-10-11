package router

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/handler"
	"reliquary/internal/queue"
	"reliquary/internal/types"
)

func TestRuntimeHTTPReportsReconnectAndAlertUntilLockReleased(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	client := inspector.client
	config := queue.Config{Pools: types.WorkerPoolConcurrency{Core: 1}, ReconnectInitialDelay: 20 * time.Millisecond, ReconnectMaxDelay: 50 * time.Millisecond, ReconnectAlertAfter: 50 * time.Millisecond}
	require.NoError(t, client.Start(context.Background(), queue.HandlerFunc(func(context.Context, *queue.Task) error { return nil }), config))
	pool, err := inspector.db.DB()
	require.NoError(t, err)
	blocker, err := pool.Conn(context.Background())
	require.NoError(t, err)
	defer blocker.Close()
	defer blocker.ExecContext(context.Background(), "SELECT pg_advisory_unlock(714140014)")
	var pid int
	require.NoError(t, inspector.db.Raw("SELECT pid FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=714140014 AND granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database())").Scan(&pid).Error)
	_, err = blocker.ExecContext(context.Background(), "SELECT pg_terminate_backend($1)", pid)
	require.NoError(t, err)
	_, err = blocker.ExecContext(context.Background(), "SELECT pg_advisory_lock(714140014)")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return client.ConnectionStatus().State == "reconnecting" && client.ConnectionStatus().Alert
	}, 5*time.Second, 10*time.Millisecond)
	h := handler.NewSystemHandler(nil, nil, nil, nil, nil, inspector, nil)
	r := gin.New()
	r.GET("/runtime", h.GetRuntimeQueues)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest("GET", "/runtime", nil))
	require.Equal(t, 200, recorder.Code)
	var response handler.RuntimeQueuesResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "reconnecting", response.QueueConnection.State)
	require.NotEmpty(t, response.QueueConnection.LastError)
	require.True(t, response.QueueConnection.Alert)
	require.False(t, response.Available)
	require.Equal(t, "reconnecting", h.QueueConnectionHealth().State)
	_, err = blocker.ExecContext(context.Background(), "SELECT pg_advisory_unlock(714140014)")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return client.ConnectionStatus().State == "active" }, 5*time.Second, 10*time.Millisecond)
}
