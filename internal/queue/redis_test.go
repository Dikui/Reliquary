package queue

import (
	"context"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"os"
	"strings"
	"testing"
)

func testClient(t *testing.T, db *gorm.DB) *Client {
	t.Helper()
	addr := os.Getenv("RELIQUARY_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set RELIQUARY_TEST_REDIS_ADDR to run real Redis integration tests")
	}
	c := NewClientWithRedis(db, asynq.RedisClientOpt{Addr: addr})
	t.Cleanup(func() { _ = c.Close(); cleanupRedis(t, c) })
	return c
}
func cleanupRedis(t *testing.T, c *Client) {
	t.Helper()
	r := redis.NewClient(&redis.Options{Addr: c.redisOpt.Addr, DB: c.redisOpt.DB})
	defer r.Close()
	ctx := context.Background()
	var cursor uint64
	for {
		keys, next, err := r.Scan(ctx, cursor, "asynq:*"+c.namespace+"*", 1000).Result()
		require.NoError(t, err)
		if len(keys) > 0 {
			require.NoError(t, r.Del(ctx, keys...).Err())
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	inspector := asynq.NewInspector(c.redisOpt)
	queues, err := inspector.Queues()
	require.NoError(t, err)
	for _, q := range queues {
		if strings.HasPrefix(q, "reliquary:"+c.namespace+":") {
			require.NoError(t, inspector.DeleteQueue(q, true))
		}
	}
	_ = inspector.Close()

}
