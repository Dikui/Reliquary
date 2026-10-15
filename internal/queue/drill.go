package queue

import (
	"context"
	"errors"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/hibiken/asynq"
)

// DrillProbe runs only in a disposable restored database. Its consumer uses a
// separate queue so actual restored work cannot call external models or tools.
func (c *Client) DrillProbe(ctx context.Context) (map[string]any, error) {
	if !regexp.MustCompile(`^reliquary_drill_[a-f0-9]{16}$`).MatchString(c.namespace) {
		return nil, errors.New("queue probe requires an isolated drill database")
	}
	conn, err := c.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseLock(conn)
	var paused bool
	if err := c.db.Raw("SELECT paused AND pause_mode='snapshot' FROM maintenance_state WHERE id=true").Scan(&paused).Error; err != nil {
		return nil, err
	}
	if !paused {
		return nil, errors.New("queue probe requires a snapshot pause")
	}
	queueName := "drill-probe-" + c.namespace
	defer c.db.Exec("DELETE FROM task_queue_counters WHERE queue=?", queueName)
	info, err := c.Enqueue(NewTask("maintenance:drill-probe", nil), Queue(queueName))
	if err != nil {
		return nil, err
	}
	defer c.db.Where("id=?", info.ID).Delete(&Job{})
	if err := c.dispatchPending(ctx, true); err != nil {
		return nil, err
	}
	var calls atomic.Int32
	p := &workerPool{name: "maintenance-drill"}
	server := asynq.NewServer(c.redisOpt, asynq.Config{Concurrency: 1, Queues: map[string]int{c.redisQueue(queueName): 1}, TaskCheckInterval: 50 * time.Millisecond, DelayedTaskCheckInterval: 50 * time.Millisecond, ShutdownTimeout: time.Second, LogLevel: asynq.ErrorLevel, RetryDelayFunc: func(int, error, *asynq.Task) time.Duration { return 50 * time.Millisecond }})
	handler := HandlerFunc(func(context.Context, *Task) error { calls.Add(1); return nil })
	if err := server.Start(asynq.HandlerFunc(func(call context.Context, task *asynq.Task) error { return c.consume(call, handler, p, task) })); err != nil {
		return nil, err
	}
	defer server.Shutdown()
	if !waitContext(ctx, 300*time.Millisecond) {
		return nil, ctx.Err()
	}
	var before Job
	if err := c.db.First(&before, "id=?", info.ID).Error; err != nil {
		return nil, err
	}
	if calls.Load() != 0 || before.State != TaskStateDispatched {
		return nil, errors.New("maintenance pause allowed premature consumption")
	}
	if err := c.db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error; err != nil {
		return nil, err
	}
	defer c.db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		var job Job
		if err := c.db.First(&job, "id=?", info.ID).Error; err != nil {
			return nil, err
		}
		if job.State == TaskStateCompleted && calls.Load() == 1 {
			return map[string]any{"paused_consumptions": 0, "completed_after_resume": 1}, nil
		}
		if !waitContext(ctx, 50*time.Millisecond) {
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("Redis replay probe did not complete after resume")
}
