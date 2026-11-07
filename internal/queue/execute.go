package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/types"
)

// consume atomically admits one delivery of an operation. The application
// singleton owns active operations until shutdown, so no lease can expire
// underneath a still-running handler.
func (c *Client) consume(ctx context.Context, h Handler, p *workerPool, wake *asynq.Task) error {
	var payload wakePayload
	if err := json.Unmarshal(wake.Payload(), &payload); err != nil || payload.ID == "" {
		return asynq.SkipRetry
	}
	redisID, _ := asynq.GetTaskID(ctx)
	var op Job
	admitted := false
	objectReserved := false
	err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var paused bool
		if err := tx.Raw("SELECT paused AND pause_mode='snapshot' FROM maintenance_state WHERE id=true FOR SHARE").Scan(&paused).Error; err != nil {
			return err
		}
		if paused {
			return fmt.Errorf("maintenance snapshot paused")
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&op, "id=?", payload.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if redisID != deliveryID(&op) || op.CancelRequested || op.State == TaskStateCancelled || op.State == TaskStateCompleted || op.State == TaskStateArchived || op.State == TaskStateActive {
			return nil
		}
		if op.ProcessAt.After(time.Now()) {
			return nil
		}
		stale, err := c.staleBusiness(tx, &op)
		if err != nil {
			return err
		}
		if stale {
			return tx.Model(&op).Updates(map[string]any{"state": TaskStateCancelled, "cancel_requested": true, "updated_at": time.Now().UTC()}).Error
		}
		c.mu.Lock()
		if c.objects[op.ObjectID] != "" {
			c.mu.Unlock()
			return errors.New("operation object is still running")
		}
		c.objects[op.ObjectID] = op.ID
		objectReserved = true
		c.mu.Unlock()
		admitted = true
		return tx.Model(&op).Updates(map[string]any{"state": TaskStateActive, "started_at": time.Now().UTC(), "updated_at": time.Now().UTC(), "worker_pool": p.name}).Error
	})
	if objectReserved {
		defer func() { c.mu.Lock(); delete(c.objects, op.ObjectID); c.mu.Unlock() }()
	}
	if err != nil || !admitted {
		return err
	}
	p.active.Add(1)
	defer p.active.Add(-1)
	return c.execute(ctx, h, &op, p)
}

// Existing business handlers own their checkpoints. This guard prevents a
// completed or superseded entry attempt from re-entering them after a crash.
func (c *Client) staleBusiness(tx *gorm.DB, op *Job) (bool, error) {
	if op.TaskType != types.TypeDocumentProcess && op.TaskType != types.TypeManualProcess && op.TaskType != types.TypeReportIndex {
		return false, nil
	}
	var row struct {
		ParseStatus string
		DeletedAt   *time.Time
	}
	r := tx.Table("entries").Select("parse_status,deleted_at").Where("id=?", op.ObjectID).Scan(&row)
	if r.Error != nil {
		return false, r.Error
	}
	// Synthetic tasks without an entry are left to the handler's own validation.
	if r.RowsAffected == 0 {
		return false, nil
	}
	if row.DeletedAt != nil || row.ParseStatus == types.ParseStatusCancelled || row.ParseStatus == types.ParseStatusDeleting {
		return true, nil
	}
	if row.ParseStatus == types.ParseStatusCompleted {
		// Treat a committed business result with a lost queue outcome as success.
		return false, nil
	}
	var newer int64
	err := tx.Model(&Job{}).Where("object_id=? AND task_type IN ? AND generation>? AND state<>?", op.ObjectID, []string{types.TypeDocumentProcess, types.TypeManualProcess}, op.Generation, TaskStateCancelled).Count(&newer).Error
	return newer > 0, err
}

func (c *Client) execute(workerCtx context.Context, h Handler, j *Job, p *workerPool) error {
	ctx, cancel := context.WithCancel(workerCtx)
	defer cancel()
	if j.TimeoutNS > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, time.Duration(j.TimeoutNS))
		defer stop()
	}
	if j.Deadline != nil {
		var stop context.CancelFunc
		ctx, stop = context.WithDeadline(ctx, *j.Deadline)
		defer stop()
	}
	ctx = WithExecution(ctx, j.Info(), p.name)
	c.mu.Lock()
	c.active[j.ID] = cancel
	if c.suspending[j.ID] {
		cancel()
	}
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.active, j.ID); delete(c.suspending, j.ID); c.mu.Unlock() }()
	task := NewTaskWithHeaders(j.TaskType, j.Payload, j.Info().Headers)
	var taskErr error
	// Cancellation can commit between admission and registration in active.
	var current Job
	if err := c.db.WithContext(ctx).First(&current, "id=?", j.ID).Error; err != nil {
		taskErr = err
	} else if current.State == TaskStateCancelled || current.Delivery != j.Delivery {
		cancel()
		taskErr = context.Canceled
	} else if j.Deadline != nil && !j.Deadline.After(time.Now()) {
		taskErr = fmt.Errorf("task absolute deadline elapsed: %w", SkipRetry)
	} else {
		done := false
		if j.TaskType == types.TypeDocumentProcess || j.TaskType == types.TypeManualProcess || j.TaskType == types.TypeReportIndex {
			query := c.db.WithContext(ctx).Table("entries").Select("COUNT(*)>0").Where("id=? AND parse_status=?", j.ObjectID, types.ParseStatusCompleted)
			if j.TaskType == types.TypeManualProcess || j.TaskType == types.TypeDocumentProcess {
				query = query.Where("summary_status=?", types.SummaryStatusCompleted)
			}
			if err := query.Scan(&done).Error; err != nil {
				taskErr = err
			}
		}
		if taskErr == nil && !done {
			func() {
				defer func() {
					if v := recover(); v != nil {
						taskErr = fmt.Errorf("task handler panic: %v", v)
					}
				}()
				taskErr = h.ProcessTask(ctx, task)
			}()
		}
		if taskErr == nil && ctx.Err() != nil {
			taskErr = ctx.Err()
		}
	}

	for {
		persistCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		c.mu.Lock()
		suspended := c.suspending[j.ID]
		c.mu.Unlock()
		err := c.finish(persistCtx, j, task, taskErr, workerCtx.Err() != nil || suspended)
		stop()
		if err == nil {
			c.Wake()
			return nil
		}
		if !waitContext(workerCtx, c.config.CheckInterval) {
			return err
		}
	}
}

func (c *Client) finish(ctx context.Context, j *Job, t *Task, taskErr error, interrupted bool) error {
	archived := false
	err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Job
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id=? AND state=? AND generation=? AND delivery=?", j.ID, TaskStateActive, j.Generation, j.Delivery).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		now := time.Now().UTC()
		stale, err := c.staleBusiness(tx, &current)
		if err != nil {
			return err
		}
		if stale {
			return tx.Model(&current).Updates(map[string]any{"state": TaskStateCancelled, "updated_at": now}).Error
		}
		updates := map[string]any{"worker_pool": "", "updated_at": now, "dispatched_at": nil}
		if current.CancelRequested {
			updates["state"] = TaskStateCancelled
			return tx.Model(&current).Updates(updates).Error
		}
		if interrupted {
			updates["state"] = TaskStatePending
			updates["delivery"] = current.Delivery + 1
			return tx.Model(&current).Updates(updates).Error
		}
		if taskErr == nil {
			if err := updateCounter(tx, current.Queue, now, true); err != nil {
				return err
			}
			updates["state"] = TaskStateCompleted
			updates["completed_at"] = now
			return tx.Model(&current).Updates(updates).Error
		}
		var deferred types.DeferredOperation
		if errors.As(taskErr, &deferred) {
			updates["state"] = TaskStateScheduled
			updates["process_at"] = maxTime(now.Add(time.Minute), deferred.DeferredUntil())
			updates["last_error"] = NormalizeError(taskErr.Error())
			updates["delivery"] = current.Delivery + 1
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
			updates["delivery"] = current.Delivery + 1
		}
		if current.TaskType == types.TypeShelfParse {
			var payload types.ShelfParsePayload
			if json.Unmarshal(current.Payload, &payload) == nil && payload.VersionID != "" && payload.SpaceID == types.SingleSpaceID && payload.Attempt > 0 {
				status := "retrying"
				if archived {
					status = "failed"
				}
				if err := tx.Model(&types.ShelfDocumentVersion{}).
					Where("id=? AND space_id=? AND parse_attempt=? AND parse_status IN ?", payload.VersionID, payload.SpaceID, payload.Attempt, []string{"pending", "parsing", "retrying"}).
					Updates(map[string]any{"parse_status": status, "parse_error": NormalizeError(taskErr.Error())}).Error; err != nil {
					return err
				}
			}
		}
		return tx.Model(&current).Updates(updates).Error
	})
	if err == nil && archived && c.config.OnDeadLetter != nil {
		c.config.OnDeadLetter(context.Background(), t, taskErr)
	}
	return err
}
func updateCounter(tx *gorm.DB, queue string, now time.Time, success bool) error {
	failed := 0
	if !success {
		failed = 1
	}
	return tx.Exec(`INSERT INTO task_queue_counters(queue,day,processed,failed) VALUES (?,?::date,1,?) ON CONFLICT(queue,day) DO UPDATE SET processed=task_queue_counters.processed+1,failed=task_queue_counters.failed+EXCLUDED.failed`, queue, now.Format("2006-01-02"), failed).Error
}
