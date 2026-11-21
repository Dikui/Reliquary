package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

const (
	wakeTaskType      = "operation:wake"
	dispatchBatchSize = 500
)

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
	if err := c.db.WithContext(ctx).Where("state IN ? AND process_at <= ?", []TaskState{TaskStatePending, TaskStateScheduled, TaskStateRetry}, now).Order("process_at,created_at,id").Limit(dispatchBatchSize).Find(&ops).Error; err != nil {
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

// The producer may still hold a transaction and must never wait for an observer
// connection. One observer per client checks committed IDs in batches on the root DB.
func (c *Client) watchCommit(id string) {
	c.observerMu.Lock()
	defer c.observerMu.Unlock()
	if c.closed {
		return
	}
	if c.pendingCommits == nil {
		c.pendingCommits = make(map[string]time.Time)
		c.observerWG.Add(1)
		go c.observeCommits(c.observerCtx)
	}
	c.pendingCommits[id] = time.Time{}
}

func (c *Client) commitIDs(ctx context.Context) []string {
	c.observerMu.Lock()
	defer c.observerMu.Unlock()
	if ctx.Err() != nil {
		c.pendingCommits = nil
		return nil
	}
	now := time.Now()
	ids := make([]string, 0, len(c.pendingCommits))
	for id, until := range c.pendingCommits {
		if until.IsZero() {
			// Give arrivals during a previous dispatch their first visibility check.
			c.pendingCommits[id] = now.Add(time.Second)
		} else if !now.Before(until) {
			delete(c.pendingCommits, id)
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		c.pendingCommits = nil
	}
	return ids
}

func (c *Client) observeCommits(ctx context.Context) {
	defer c.observerWG.Done()
	for {
		ids := c.commitIDs(ctx)
		if len(ids) == 0 {
			return
		}
		for batch := range slices.Chunk(ids, dispatchBatchSize) {
			probe, cancel := context.WithTimeout(ctx, time.Second)
			var visible []string
			err := c.db.WithContext(probe).Model(&Job{}).Where("id IN ?", batch).Pluck("id", &visible).Error
			cancel()
			if err != nil {
				break
			}
			if len(visible) == 0 {
				continue
			}
			c.observerMu.Lock()
			for _, id := range visible {
				delete(c.pendingCommits, id)
			}
			c.observerMu.Unlock()
			c.Wake()
			c.mu.Lock()
			hasWorkers := c.cancel != nil
			c.mu.Unlock()
			if !hasWorkers {
				// A producer-only client has no local worker to receive its wake.
				dispatch, stop := context.WithTimeout(ctx, 5*time.Second)
				_ = c.DispatchPending(dispatch)
				stop()
			}
		}
		waitContext(ctx, 25*time.Millisecond)
	}
}

func (c *Client) redisQueue(name string) string { return "reliquary:" + c.namespace + ":" + name }
