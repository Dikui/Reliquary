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
	"gorm.io/gorm"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

const entityOwnerID = "entity-owner"

func TestMain(m *testing.M)                 { os.Exit(pgtest.Run(m)) }
func entityFixtureDB(t *testing.T) *gorm.DB { t.Helper(); return pgtest.Open(t) }
func entityContext(spaceID uint64, userID string) context.Context {
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, spaceID)
	return context.WithValue(ctx, types.UserIDContextKey, userID)
}
func entityIdentityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		spaceID := uint64(1)
		if raw := c.GetHeader("X-Test-Space"); raw != "" {
			_, _ = fmt.Sscanf(raw, "%d", &spaceID)
		}
		userID := c.GetHeader("X-Test-User")
		if userID == "" {
			userID = entityOwnerID
		}
		c.Set(types.SpaceIDContextKey.String(), spaceID)
		c.Request = c.Request.WithContext(entityContext(spaceID, userID))
		c.Next()
	}
}
func entityRequest(t *testing.T, engine *gin.Engine, method, target, body string, spaceID uint64, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Space", fmt.Sprint(spaceID))
	req.Header.Set("X-Test-User", userID)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

type entityTaskQueue struct{ tasks []*taskqueue.Task }

func (q *entityTaskQueue) Enqueue(task *taskqueue.Task, _ ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &taskqueue.TaskInfo{ID: task.Type()}, nil
}

func (q *entityTaskQueue) EnqueueTx(ctx context.Context, tx *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	info, err := taskqueue.NewClient(tx).EnqueueTx(ctx, tx, task, opts...)
	if err == nil {
		q.tasks = append(q.tasks, task)
	}
	return info, err
}

type entityEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   any             `json:"error"`
}
