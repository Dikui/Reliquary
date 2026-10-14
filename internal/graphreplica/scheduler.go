package graphreplica

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/queue"
	"reliquary/internal/types"
)

// Sweep translates durable notifications into ikb-03 operations. Redis is only
// a wakeup transport. Failed or cancelled operations leave notifications intact.
func (s *Service) Sweep(ctx context.Context, now time.Time) error {
	if s.queue == nil {
		return errors.New("graph task queue unavailable")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var paused bool
		if err := tx.Raw("SELECT paused AND pause_mode='snapshot' FROM maintenance_state WHERE id=true FOR SHARE").Scan(&paused).Error; err != nil {
			return err
		}
		if paused {
			return nil
		}
		if err := lock(tx, "reliquary:graphreplica:sweep", false); err != nil {
			return err
		}
		var pending []notification
		if err := tx.Table("graph_pending_sync").Select("space_id,entity_id").Where("completed_at IS NULL").Group("space_id,entity_id").Scan(&pending).Error; err != nil {
			return err
		}
		for _, n := range pending {
			n.JobID = "graph:" + n.EntityID
			var active int64
			if err := tx.Model(&queue.Job{}).Where("task_type=? AND object_id=? AND state IN ?", TypeSync, n.JobID, []queue.TaskState{queue.TaskStatePending, queue.TaskStateDispatched, queue.TaskStateActive, queue.TaskStateScheduled, queue.TaskStateRetry}).Count(&active).Error; err != nil {
				return err
			}
			if active > 0 {
				continue
			}
			payload, _ := json.Marshal(n)
			if _, err := s.queue.EnqueueTx(ctx, tx, queue.NewTask(TypeSync, payload), queue.Queue(types.QueueMaintenance), queue.Timeout(30*time.Second), queue.Retention(7*24*time.Hour)); err != nil {
				return err
			}
		}
		payload, _ := json.Marshal(map[string]any{"job_id": "graph:check"})
		// One durable daily check across process restarts, using the server timezone.
		_, err := s.queue.EnqueueTx(ctx, tx, queue.NewTask(TypeCheck, payload), queue.TaskID("graph:check:"+now.Format("2006-01-02")), queue.Queue(types.QueueMaintenance), queue.Timeout(30*time.Minute), queue.Retention(7*24*time.Hour))
		if err != nil && !errors.Is(err, queue.ErrTaskIDConflict) {
			return err
		}
		return tx.Where("completed_at < ?", now.Add(-7*24*time.Hour)).Delete(&types.GraphPendingSync{}).Error
	})
	if err == nil {
		s.queue.Wake()
	}
	return err
}
func (s *Service) EnqueueRebuild(ctx context.Context) (*queue.TaskInfo, error) {
	payload, _ := json.Marshal(map[string]any{"job_id": "graph:rebuild"})
	return s.queue.Enqueue(queue.NewTask(TypeRebuild, payload), queue.Queue(types.QueueMaintenance), queue.Timeout(30*time.Minute), queue.Retention(7*24*time.Hour))
}
func (s *Service) Start(interval time.Duration) func() error {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			sweepCtx, stop := context.WithTimeout(ctx, 30*time.Second)
			if err := s.Sweep(sweepCtx, time.Now()); err != nil && ctx.Err() == nil {
				log.Printf("graph replica notification sweep: %v", err)
			}
			stop()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() error { cancel(); wg.Wait(); return nil }
}
