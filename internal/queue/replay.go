package queue

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type ReplayResult struct {
	Dispatched int64 `json:"dispatched"`
	Deferred   int64 `json:"deferred"`
}

// ReplayPaused is an offline recovery operation. It holds the executor lock,
// preserves retry budgets and future process_at values, and only publishes
// wake messages. Consumers still honor the PG snapshot admission gate.
func (c *Client) ReplayPaused(ctx context.Context) (ReplayResult, error) {
	result := ReplayResult{}
	conn, err := c.acquire(ctx)
	if err != nil {
		return result, err
	}
	defer releaseLock(conn)
	err = c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var paused bool
		if err := tx.Raw("SELECT paused AND pause_mode='snapshot' FROM maintenance_state WHERE id=true FOR SHARE").Scan(&paused).Error; err != nil {
			return err
		}
		if !paused {
			return errors.New("offline queue replay requires a snapshot pause")
		}
		if err := c.recoverJobs(ctx, true); err != nil {
			return err
		}
		for {
			var count int64
			if err := c.db.WithContext(ctx).Model(&Job{}).Where("state IN ? AND process_at<=?", []TaskState{TaskStatePending, TaskStateScheduled, TaskStateRetry}, time.Now()).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				break
			}
			if err := c.dispatchPending(ctx, true); err != nil {
				return err
			}
		}
		if err := c.db.WithContext(ctx).Model(&Job{}).Where("state=?", TaskStateDispatched).Count(&result.Dispatched).Error; err != nil {
			return err
		}
		return c.db.WithContext(ctx).Model(&Job{}).Where("state IN ? AND process_at>?", []TaskState{TaskStatePending, TaskStateScheduled, TaskStateRetry}, time.Now()).Count(&result.Deferred).Error
	})
	return result, err
}
