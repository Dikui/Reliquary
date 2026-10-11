package queue

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log"
	"sync"
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
		return nil, errors.New("another task worker process owns the PostgreSQL queue")
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
		c.mu.Lock()
		c.connection.State = "stopped"
		c.mu.Unlock()
	}()
	for root.Err() == nil {
		generation, cancel := context.WithCancel(root)
		c.mu.Lock()
		c.workerCtx, c.lock = generation, conn
		c.connection = types.QueueConnectionStatus{State: "active"}
		c.mu.Unlock()
		var workers sync.WaitGroup
		for _, p := range c.pools {
			for i := 0; i < p.concurrency; i++ {
				workers.Add(1)
				go func(p *workerPool) { defer workers.Done(); c.work(generation, handler, p) }(p)
			}
		}
		err := c.maintain(generation, conn)
		cancel()
		if root.Err() == nil {
			c.reconnecting(err, c.config.ReconnectInitialDelay)
			log.Printf("task queue paused; reconnecting after singleton connection lost: %v", err)
		}
		// Drain the preceding generation before recovering its active rows or
		// starting new slots. Handlers and result writes cannot overlap rounds.
		workers.Wait()
		releaseLock(conn)
		conn = nil
		if root.Err() != nil {
			return
		}
		delay := c.config.ReconnectInitialDelay
		for root.Err() == nil {
			if !waitContext(root, delay) {
				return
			}
			conn, err = c.acquire(root)
			if err == nil {
				recoverCtx, stop := context.WithTimeout(root, 5*time.Second)
				err = c.recoverJobs(recoverCtx, true)
				stop()
				if err != nil {
					releaseLock(conn)
					conn = nil
				}
			}
			if err == nil {
				log.Printf("task queue PostgreSQL connection and advisory lock restored; resuming workers")
				break
			}
			delay = min(delay*2, c.config.ReconnectMaxDelay)
			c.reconnecting(err, delay)
			log.Printf("task queue reconnect failed; retrying in %s: %v", delay, err)
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
	c.connection.LastError = NormalizeError(err.Error())
	next := time.Now().UTC().Add(delay)
	c.connection.NextRetryAt = &next
}
