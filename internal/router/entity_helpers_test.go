package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

const entityOwnerID = "entity-owner"

func TestMain(m *testing.M)                 { os.Exit(pgtest.Run(m)) }
func entityFixtureDB(t *testing.T) *gorm.DB { t.Helper(); return pgtest.Open(t) }
func entityContext(tenantID uint64, userID string) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
	return context.WithValue(ctx, types.UserIDContextKey, userID)
}
func entityIdentityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := uint64(1)
		if raw := c.GetHeader("X-Test-Tenant"); raw != "" {
			_, _ = fmt.Sscanf(raw, "%d", &tenantID)
		}
		userID := c.GetHeader("X-Test-User")
		if userID == "" {
			userID = entityOwnerID
		}
		c.Set(types.TenantIDContextKey.String(), tenantID)
		c.Request = c.Request.WithContext(entityContext(tenantID, userID))
		c.Next()
	}
}
func entityRequest(t *testing.T, engine *gin.Engine, method, target, body string, tenantID uint64, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Tenant", fmt.Sprint(tenantID))
	req.Header.Set("X-Test-User", userID)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

type entityTaskQueue struct{ tasks []*asynq.Task }

func (q *entityTaskQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{ID: task.Type()}, nil
}

type entityEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   any             `json:"error"`
}
