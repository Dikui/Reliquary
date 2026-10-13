package queue

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"log"
	"time"

	"reliquary/internal/types"
)

func (c *Client) acquire(ctx context.Context) (*sql.Conn, error) {
	pool, err := c.db.DB()
	if err != nil {
		return nil, err
	}
	attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := pool.Conn(attempt)
	if err != nil {
		return nil, err
	}
	var locked bool
	if err = conn.QueryRowContext(attempt, "SELECT pg_try_advisory_lock(714140014)").Scan(&locked); err != nil || !locked {
		// Discard the session, including an uncertain lock after a timeout.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("another task worker process owns the operation executor")
	}
	return conn, nil
}

func releaseLock(conn *sql.Conn) {
	// Closing a sql.Conn normally returns it to the pool and retains session
	// locks. Discard this dedicated session so ownership cannot survive it.
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

func (c *Client) supervise(root context.Context, handler Handler, conn *sql.Conn) {
	defer func() {
		if conn != nil {
			releaseLock(conn)
		}
	}()
	for root.Err() == nil {
		generation, cancel := context.WithCancel(root)
		c.mu.Lock()
		c.workerCtx = generation
		c.lock = conn
		c.mu.Unlock()
		servers := make([]*asynq.Server, 0, len(c.pools))
		for _, pool := range c.pools {
			if pool.concurrency <= 0 {
				continue
			}
			p := pool
			server := asynq.NewServer(c.redisOpt, asynq.Config{Concurrency: p.concurrency, Queues: c.redisWeights(p.weights), BaseContext: func() context.Context { return generation }, ShutdownTimeout: 5 * time.Second, TaskCheckInterval: 100 * time.Millisecond, RetryDelayFunc: func(int, error, *asynq.Task) time.Duration { return time.Second }, LogLevel: asynq.ErrorLevel})
			if err := server.Start(asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error { return c.consume(ctx, handler, p, t) })); err != nil {
				log.Printf("start Redis consumer: %v", err)
			} else {
				servers = append(servers, server)
			}
		}
		err := c.maintain(generation, conn)
		cancel()
		for _, server := range servers {
			server.Shutdown()
		}
		// Shutdown can reach its timeout while a handler is unwinding. Do not recover
		// that generation or relinquish the singleton until its business writes end.
		for {
			busy := false
			for _, p := range c.pools {
				if p.active.Load() > 0 {
					busy = true
				}
			}
			if !busy {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if root.Err() != nil {
			return
		}
		c.reconnecting(err, c.config.ReconnectInitialDelay)
		releaseLock(conn)
		conn = nil
		delay := c.config.ReconnectInitialDelay
		for root.Err() == nil {
			if !waitContext(root, delay) {
				return
			}
			var acquireErr error
			conn, acquireErr = c.acquire(root)
			if acquireErr == nil {
				acquireErr = c.recoverJobs(root, true)
				if acquireErr != nil {
					releaseLock(conn)
					conn = nil
				}
			}
			if acquireErr == nil {
				break
			}
			delay = min(delay*2, c.config.ReconnectMaxDelay)
			c.reconnecting(acquireErr, delay)
		}
	}
}
func (c *Client) reconnecting(err error, delay time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connection.State != "reconnecting" {
		now := time.Now().UTC()
		c.connection.Since = &now
	}
	c.connection.State = "reconnecting"
	if err != nil {
		c.connection.LastError = NormalizeError(err.Error())
	}
	next := time.Now().UTC().Add(delay)
	c.connection.NextRetryAt = &next
}
func (c *Client) recoverJobs(ctx context.Context, all bool) error {
	if !all {
		return nil
	}
	return c.db.WithContext(ctx).Model(&Job{}).Where("state IN ?", []TaskState{TaskStateActive, TaskStateDispatched}).Updates(map[string]any{"state": TaskStatePending, "delivery": gorm.Expr("delivery+1"), "dispatched_at": nil, "worker_pool": "", "updated_at": time.Now().UTC()}).Error
}
func (c *Client) maintenanceCycle(ctx context.Context) error {
	return c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var paused bool
		if err := tx.Raw("SELECT paused AND pause_mode='snapshot' FROM maintenance_state WHERE id=true FOR SHARE").Scan(&paused).Error; err != nil {
			return err
		}
		if paused {
			return nil
		}
		return tx.Exec(`DELETE FROM task_pending_ops WHERE state='completed' AND retention_ns>0 AND completed_at + retention_ns / 1000.0 * interval '1 microsecond' < now()`).Error
	})
}
func (c *Client) maintain(ctx context.Context, conn *sql.Conn) error {
	dispatch := func() {
		call, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		err := c.DispatchPending(call)
		if err == nil {
			err = c.transport.Ping()
		}
		if err != nil {
			c.reconnecting(err, c.config.CheckInterval)
		} else {
			c.mu.Lock()
			c.connection = types.QueueConnectionStatus{State: "active"}
			c.mu.Unlock()
		}
	}
	dispatch()
	sweep := time.NewTicker(c.config.CheckInterval)
	defer sweep.Stop()
	ping := time.NewTicker(time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.wake:
			dispatch()
		case <-sweep.C:
			dispatch()
			if err := c.maintenanceCycle(ctx); err != nil {
				log.Printf("operation maintenance: %v", err)
			}
		case <-ping.C:
			check, stop := context.WithTimeout(ctx, 5*time.Second)
			err := conn.PingContext(check)
			stop()
			if err != nil {
				return err
			}
		}
	}
}

func (c *Client) redisWeights(weights map[string]int) map[string]int {
	out := map[string]int{}
	for q, w := range weights {
		out[c.redisQueue(q)] = w
	}
	return out
}
