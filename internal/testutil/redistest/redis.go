// Package redistest isolates real Redis tests by their PostgreSQL database name.
package redistest

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"reliquary/internal/queue"
)

func NewClient(t testing.TB, db *gorm.DB) *queue.Client {
	t.Helper()
	addr := os.Getenv("RELIQUARY_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set RELIQUARY_TEST_REDIS_ADDR to run real Redis integration tests")
	}
	c := queue.NewClientWithRedis(db, asynq.RedisClientOpt{Addr: addr, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second})
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		r := redis.NewClient(&redis.Options{Addr: addr})
		defer r.Close()
		ctx := context.Background()
		var cursor uint64
		for {
			keys, next, err := r.Scan(ctx, cursor, "asynq:*"+name+"*", 1000).Result()
			if err != nil {
				t.Error(err)
				return
			}
			if len(keys) > 0 {
				if err = r.Del(ctx, keys...).Err(); err != nil {
					t.Error(err)
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: addr})
		queues, err := inspector.Queues()
		if err != nil {
			t.Error(err)
		}
		for _, q := range queues {
			if strings.HasPrefix(q, "reliquary:"+name+":") {
				if err := inspector.DeleteQueue(q, true); err != nil {
					t.Error(err)
				}
			}
		}
		_ = inspector.Close()

	})
	return c
}
