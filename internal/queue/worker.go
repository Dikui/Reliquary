package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"reliquary/internal/types"
)

type Config struct {
	Pools                                                         types.WorkerPoolConcurrency
	CheckInterval, LeaseDuration, HeartbeatInterval               time.Duration
	ReconnectInitialDelay, ReconnectMaxDelay, ReconnectAlertAfter time.Duration
	RetryDelayFunc                                                func(int, error, *Task) time.Duration
	OnDeadLetter                                                  func(context.Context, *Task, error)
}

func (c Config) withDefaults() Config {
	if c.ReconnectInitialDelay <= 0 {
		c.ReconnectInitialDelay = time.Second
	}
	if c.ReconnectMaxDelay < c.ReconnectInitialDelay {
		c.ReconnectMaxDelay = 30 * time.Second
	}
	if c.ReconnectAlertAfter <= 0 {
		c.ReconnectAlertAfter = 5 * time.Minute
	}
	if c.Pools == (types.WorkerPoolConcurrency{}) {
		c.Pools = types.DefaultWorkerPoolConcurrency()
	}
	if c.CheckInterval <= 0 {
		c.CheckInterval = time.Second
	}
	if c.LeaseDuration <= 0 {
		c.LeaseDuration = 30 * time.Second
	}
	if c.HeartbeatInterval <= 0 || c.HeartbeatInterval >= c.LeaseDuration {
		c.HeartbeatInterval = c.LeaseDuration / 3
	}
	if c.RetryDelayFunc == nil {
		c.RetryDelayFunc = DefaultRetryDelayFunc
	}
	return c
}

type workerPool struct {
	name        string
	concurrency int
	weights     map[string]int
	order       []string
	next        atomic.Uint64
	active      atomic.Int32
}

func (c Config) workerPools() []*workerPool {
	pools := []*workerPool{
		{name: types.WorkerPoolCore, concurrency: c.Pools.Core, weights: types.QueueWeightsForPool(types.WorkerPoolCore)},
		{name: types.WorkerPoolEnrichment, concurrency: c.Pools.Enrichment, weights: types.QueueWeightsForPool(types.WorkerPoolEnrichment)},
		{name: types.WorkerPoolMaintenance, concurrency: c.Pools.Maintenance, weights: types.QueueWeightsForPool(types.WorkerPoolMaintenance)},
		{name: types.WorkerPoolShared, concurrency: c.Pools.Shared, weights: types.QueueWeightsForSharedPool()},
	}
	for _, p := range pools {
		keys := make([]string, 0, len(p.weights))
		for q := range p.weights {
			keys = append(keys, q)
		}
		sort.Strings(keys)
		for _, q := range keys {
			for i := 0; i < p.weights[q]; i++ {
				p.order = append(p.order, q)
			}
		}
	}
	return pools
}

// DefaultRetryDelayFunc retains the previous retry schedule: n^4 + 15 seconds
// with random jitter from 0 through 29*(n+1) seconds.
func DefaultRetryDelayFunc(n int, _ error, _ *Task) time.Duration {
	return time.Duration(n*n*n*n+15+rand.IntN(30)*(n+1)) * time.Second
}

func (c *Client) claim(ctx context.Context, p *workerPool) (*Job, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c.lockMu.Lock()
	defer c.lockMu.Unlock()
	// Claims use the session that owns the advisory lock. Once that session
	// breaks, another pool connection cannot keep claiming before detection.
	db := c.db.WithContext(ctx)
	c.mu.Lock()
	conn := c.lock
	running := c.cancel != nil
	c.mu.Unlock()
	if running {
		if conn == nil {
			return nil, errors.New("task queue singleton connection is unavailable")
		}
		db.Statement.ConnPool = conn
	}
	start := int((p.next.Add(1) - 1) % uint64(len(p.order)))
	seen := map[string]bool{}
	for offset := 0; offset < len(p.order); offset++ {
		q := p.order[(start+offset)%len(p.order)]
		if seen[q] {
			continue
		}
		seen[q] = true
		var j Job
		now := time.Now().UTC()
		token := uuid.NewString()
		r := db.Raw(`WITH next AS (
		 SELECT id FROM task_jobs WHERE queue = ? AND state IN ('pending','scheduled','retry')
		 AND NOT EXISTS (SELECT 1 FROM maintenance_state WHERE paused AND pause_mode='snapshot')
		 AND process_at <= ? ORDER BY process_at, created_at, id FOR UPDATE SKIP LOCKED LIMIT 1
		) UPDATE task_jobs AS j SET state='active',started_at=?,updated_at=?,lease_until=?,claim_token=?,worker_pool=?
		 FROM next WHERE j.id=next.id RETURNING j.*`, q, now, now, now, now.Add(c.config.LeaseDuration), token, p.name).Scan(&j)
		if r.Error != nil {
			return nil, r.Error
		}
		if r.RowsAffected > 0 {
			return &j, nil
		}
	}
	return nil, nil
}

func (c *Client) work(ctx context.Context, h Handler, p *workerPool) {
	for ctx.Err() == nil {
		j, err := c.claim(ctx, p)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("task queue claim %s: %v", p.name, err)
			}
			if !waitContext(ctx, c.config.CheckInterval) {
				return
			}
			continue
		}
		if j == nil {
			if !waitContext(ctx, c.config.CheckInterval) {
				return
			}
			continue
		}
		p.active.Add(1)
		c.execute(ctx, h, j, p)
		p.active.Add(-1)
	}
}

func (c *Client) execute(workerCtx context.Context, h Handler, j *Job, p *workerPool) {
	ctx, cancel := context.WithCancel(workerCtx)
	if j.TimeoutNS > 0 {
		next, stop := context.WithTimeout(ctx, time.Duration(j.TimeoutNS))
		defer stop()
		ctx = next
	}
	if j.Deadline != nil {
		next, stop := context.WithDeadline(ctx, *j.Deadline)
		defer stop()
		ctx = next
	}
	ctx = WithExecution(ctx, j.Info(), p.name)
	c.mu.Lock()
	c.active[j.ID] = cancel
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.active, j.ID)
		c.mu.Unlock()
	}()
	heartbeatCtx, stopHeartbeat := context.WithCancel(workerCtx)
	heartbeatDone := make(chan struct{})
	go c.heartbeat(heartbeatCtx, cancel, j, heartbeatDone)
	defer func() { stopHeartbeat(); <-heartbeatDone }()
	t := NewTaskWithHeaders(j.TaskType, j.Payload, j.Info().Headers)
	var taskErr error
	if j.CancelRequested {
		cancel()
		taskErr = context.Canceled
	} else if j.Deadline != nil && !j.Deadline.After(time.Now()) {
		taskErr = fmt.Errorf("task absolute deadline has elapsed: %w", SkipRetry)
	} else {
		func() {
			defer func() {
				if v := recover(); v != nil {
					taskErr = fmt.Errorf("task handler panic: %v", v)
				}
			}()
			taskErr = h.ProcessTask(ctx, t)
		}()
		if taskErr == nil && ctx.Err() != nil {
			taskErr = ctx.Err()
		}
	}
	cancel()
	// Persist an outcome even when the worker context has been cancelled.
	// On a transient database error the leased active row remains recoverable.
	for {
		persistCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		err := c.finish(persistCtx, j, t, taskErr, workerCtx.Err() != nil)
		stop()
		if err == nil {
			return
		}
		log.Printf("task queue finish %s: %v", j.ID, err)
		if workerCtx.Err() != nil {
			return
		}
		if !waitContext(workerCtx, c.config.CheckInterval) {
			return
		}
	}
}

func (c *Client) heartbeat(ctx context.Context, cancel context.CancelFunc, j *Job, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var cancelled bool
			r := c.db.WithContext(ctx).Raw(`UPDATE task_jobs SET lease_until=?,updated_at=? WHERE id=? AND state='active' AND claim_token=? RETURNING cancel_requested`, time.Now().UTC().Add(c.config.LeaseDuration), time.Now().UTC(), j.ID, j.ClaimToken).Scan(&cancelled)
			if r.Error != nil {
				if ctx.Err() == nil {
					log.Printf("task queue heartbeat %s: %v", j.ID, r.Error)
				}
				continue
			}
			if r.RowsAffected == 0 || cancelled {
				cancel()
				return
			}
		}
	}
}

func (c *Client) finish(ctx context.Context, j *Job, t *Task, taskErr error, interrupted bool) error {
	archived := false
	err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Job
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ? AND state = ? AND claim_token = ?", j.ID, TaskStateActive, j.ClaimToken).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		now := time.Now().UTC()
		updates := map[string]interface{}{"claim_token": "", "worker_pool": "", "lease_until": nil, "updated_at": now}
		if current.CancelRequested {
			return tx.Delete(&current).Error
		}
		if interrupted {
			updates["state"] = TaskStatePending
			updates["process_at"] = now
			return tx.Model(&current).Updates(updates).Error
		}
		if taskErr == nil {
			if err := updateCounter(tx, current.Queue, now, true); err != nil {
				return err
			}
			if current.RetentionNS == 0 {
				return tx.Delete(&current).Error
			}
			updates["state"] = TaskStateCompleted
			updates["completed_at"] = now
			return tx.Model(&current).Updates(updates).Error
		}
		var deferred *types.ChatGPTDeferredError
		if errors.As(taskErr, &deferred) {
			updates["state"] = TaskStateScheduled
			updates["process_at"] = maxTime(now.Add(time.Minute), deferred.RetryAt)
			updates["last_error"] = NormalizeError(taskErr.Error())
			return tx.Model(&current).Updates(updates).Error
		}
		updates["last_failed_at"] = now
		updates["last_error"] = NormalizeError(taskErr.Error())
		updates["fail_count"] = current.FailCount + 1
		if err := updateCounter(tx, current.Queue, now, false); err != nil {
			return err
		}
		permanent := errors.Is(taskErr, SkipRetry) || types.IsChatGPTPermanentError(taskErr) || (current.Deadline != nil && !current.Deadline.After(now))
		if permanent || current.Retried >= current.MaxRetry {
			updates["state"] = TaskStateArchived
			if err := tx.Create(BuildDeadLetter(t, taskErr, current.FailCount+1)).Error; err != nil {
				return err
			}
			archived = true
		} else {
			updates["state"] = TaskStateRetry
			updates["retried"] = current.Retried + 1
			updates["process_at"] = now.Add(c.config.RetryDelayFunc(current.Retried, taskErr, t))
		}
		return tx.Model(&current).Updates(updates).Error
	})
	if err == nil && archived && c.config.OnDeadLetter != nil {
		func() {
			defer func() {
				if v := recover(); v != nil {
					log.Printf("task queue dead-letter callback %s: %v", j.ID, v)
				}
			}()
			c.config.OnDeadLetter(context.Background(), t, taskErr)
		}()
	}
	return err
}

func updateCounter(tx *gorm.DB, queue string, now time.Time, success bool) error {
	processed, failed := 1, 0
	if !success {
		failed = 1
	}
	return tx.Exec(`INSERT INTO task_queue_counters(queue,day,processed,failed) VALUES (?,?::date,?,?)
	 ON CONFLICT(queue,day) DO UPDATE SET processed=task_queue_counters.processed+EXCLUDED.processed,failed=task_queue_counters.failed+EXCLUDED.failed`, queue, now.Format("2006-01-02"), processed, failed).Error
}

func (c *Client) recoverJobs(ctx context.Context, all bool) error {
	return c.recoverJobsWithDB(ctx, c.db, all)
}

func (c *Client) recoverJobsWithDB(ctx context.Context, db *gorm.DB, all bool) error {
	q := db.WithContext(ctx).Model(&Job{}).Where("state = ?", TaskStateActive)
	if !all {
		q = q.Where("lease_until < ?", time.Now().UTC())
		// The singleton lock means every live handler belongs to this process.
		// A delayed heartbeat must not let a second slot execute its business
		// writes concurrently. Startup recovery has no live handlers to exclude.
		c.mu.Lock()
		live := make([]string, 0, len(c.active))
		for id := range c.active {
			live = append(live, id)
		}
		c.mu.Unlock()
		if len(live) > 0 {
			q = q.Where("id NOT IN ?", live)
		}
	}
	return q.Updates(map[string]interface{}{"state": TaskStatePending, "process_at": time.Now().UTC(), "updated_at": time.Now().UTC(), "claim_token": "", "worker_pool": "", "lease_until": nil}).Error
}

func (c *Client) maintenanceCycle(ctx context.Context) error {
	return c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var paused bool
		// Holding this row lock until the writes finish also prevents a pause
		// from crossing an already admitted maintenance cycle.
		if err := tx.Raw(`SELECT paused AND pause_mode='snapshot' FROM maintenance_state WHERE id=true FOR SHARE`).Scan(&paused).Error; err != nil {
			return err
		}
		if paused {
			return nil
		}
		if err := c.recoverJobsWithDB(ctx, tx, false); err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM task_jobs WHERE state='completed' AND completed_at + retention_ns / 1000.0 * interval '1 microsecond' < now()`).Error
	})
}

func (c *Client) maintain(ctx context.Context, conn *sql.Conn) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			c.lockMu.Lock()
			err := conn.PingContext(pingCtx)
			c.lockMu.Unlock()
			cancel()
			if err != nil {
				return err
			}
			maintenanceCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			if err := c.maintenanceCycle(maintenanceCtx); err != nil && ctx.Err() == nil {
				log.Printf("task queue maintenance: %v", err)
			}
			stop()
		}
	}
}

// BuildDeadLetter keeps the existing archive scope and diagnostic fields.
func BuildDeadLetter(t *Task, taskErr error, attempts int) *types.TaskDeadLetter {
	if t == nil {
		return nil
	}
	var p struct {
		SpaceID   uint64 `json:"tenant_id"`
		LibraryID string `json:"library_id"`
		EntryID   string `json:"knowledge_id"`
	}
	_ = types.UnmarshalSpacePayload(t.Payload(), &p)
	scope, id := types.TaskScopeUnknown, ""
	if p.LibraryID != "" {
		scope, id = types.TaskScopeLibrary, p.LibraryID
	} else if p.EntryID != "" {
		scope, id = types.TaskScopeEntry, p.EntryID
	} else if p.SpaceID != 0 {
		scope, id = types.TaskScopeSpace, strconv.FormatUint(p.SpaceID, 10)
	}
	payload := t.Payload()
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	message := NormalizeError(taskErr.Error())
	return &types.TaskDeadLetter{SpaceID: types.SingleSpaceID, TaskType: t.Type(), Scope: scope, ScopeID: id, RelatedID: p.EntryID, Payload: payload, LastError: message, FailCount: attempts, FailedAt: time.Now().UTC()}
}

// NormalizeError keeps diagnostic text within the archive's 8KB limit and
// valid for PostgreSQL UTF-8 text columns, including errors from providers.
func NormalizeError(message string) string {
	message = strings.ReplaceAll(strings.ToValidUTF8(message, "\uFFFD"), "\x00", "")
	const limit, suffix = 8192, "...(truncated)"
	if len(message) <= limit {
		return message
	}
	end := limit - len(suffix)
	for !utf8.ValidString(message[:end]) {
		end--
	}
	return message[:end] + suffix
}
func waitContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
