package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

const wakeTaskType = "operation:wake"

type wakePayload struct {
	ID string `json:"operation_id"`
}

func RedisOptions() asynq.RedisClientOpt {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	db := 0
	if raw := os.Getenv("REDIS_DB"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			panic("REDIS_DB must be a non-negative integer")
		}
		db = n
	}
	return asynq.RedisClientOpt{Addr: addr, Password: os.Getenv("REDIS_PASSWORD"), DB: db, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second}
}
func deliveryID(j *Job) string { return fmt.Sprintf("%s:%d:%d", j.ID, j.Generation, j.Delivery) }

// DispatchPending only sees committed operations. Redis errors never undo the
// business transaction. Expired deliveries receive a new fencing number so a
// Redis archive or lost acknowledgement cannot suppress durable work forever.
func (c *Client) DispatchPending(ctx context.Context) error {
	return c.dispatchPending(ctx, false)
}

func (c *Client) dispatchPending(ctx context.Context, recovery bool) error {
	c.dispatchMu.Lock()
	defer c.dispatchMu.Unlock()
	c.mu.Lock()
	cfg := c.config.withDefaults()
	transport := c.transport
	c.mu.Unlock()
	var paused bool
	if err := c.db.WithContext(ctx).Raw("SELECT EXISTS(SELECT 1 FROM maintenance_state WHERE paused AND pause_mode='snapshot')").Scan(&paused).Error; err != nil {
		return err
	}
	if paused && !recovery {
		return nil
	}
	now := time.Now().UTC()
	if err := c.db.WithContext(ctx).Model(&Job{}).Where("state=? AND dispatched_at < ? AND NOT EXISTS(SELECT 1 FROM maintenance_state WHERE paused AND pause_mode='snapshot')", TaskStateDispatched, now.Add(-cfg.DeliveryTimeout)).Updates(map[string]any{"state": TaskStatePending, "dispatched_at": nil, "delivery": gorm.Expr("delivery+1"), "updated_at": now}).Error; err != nil {
		return err
	}
	var ops []Job
	if err := c.db.WithContext(ctx).Where("state IN ? AND process_at <= ?", []TaskState{TaskStatePending, TaskStateScheduled, TaskStateRetry}, now).Order("process_at,created_at,id").Limit(500).Find(&ops).Error; err != nil {
		return err
	}
	if transport == nil {
		transport = asynq.NewClient(c.redisOpt)
		defer transport.Close()
	}
	for _, op := range ops {
		payload, _ := json.Marshal(wakePayload{op.ID})
		_, err := transport.EnqueueContext(ctx, asynq.NewTask(wakeTaskType, payload), asynq.TaskID(deliveryID(&op)), asynq.Queue(c.redisQueue(op.Queue)), asynq.MaxRetry(1000000), asynq.Timeout(25*time.Hour))
		if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
			return err
		}
		// A worker may already have started or finished. Never write its state back.
		if err := c.db.WithContext(ctx).Model(&Job{}).Where("(? OR NOT EXISTS(SELECT 1 FROM maintenance_state WHERE paused AND pause_mode='snapshot')) AND id=? AND generation=? AND delivery=? AND state IN ?", recovery, op.ID, op.Generation, op.Delivery, []TaskState{TaskStatePending, TaskStateScheduled, TaskStateRetry}).Updates(map[string]any{"state": TaskStateDispatched, "dispatched_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
	}
	return nil
}

// WatchCommit provides a best-effort prompt wake for transaction owners that
// cannot expose an after-commit hook. It checks visibility on the root DB, never
// on the caller's transaction. The periodic sweep covers long transactions.
func (c *Client) watchCommit(id string) {
	c.observerMu.Lock()
	if c.closed {
		c.observerMu.Unlock()
		return
	}
	c.observerWG.Add(1)
	parent := c.observerCtx
	c.observerMu.Unlock()
	go func() {
		defer c.observerWG.Done()
		ctx, cancel := context.WithTimeout(parent, time.Second)
		defer cancel()
		for ctx.Err() == nil {
			var count int64
			if c.db.WithContext(ctx).Model(&Job{}).Where("id=?", id).Count(&count).Error == nil && count > 0 {
				c.Wake()
				// A short-lived producer (for example report creation) need not own workers.
				_ = c.DispatchPending(ctx)
				return
			}
			if !waitContext(ctx, 25*time.Millisecond) {
				return
			}
		}
	}()
}

func (c *Client) redisQueue(name string) string { return "reliquary:" + c.namespace + ":" + name }
