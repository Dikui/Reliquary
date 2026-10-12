package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	taskqueue "reliquary/internal/queue"

	"reliquary/internal/logger"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// Scheduler enqueues periodic work at absolute cron times. PostgreSQL stores
// the sync log and task in one transaction; a deterministic task ID suppresses
// duplicate ticks. The same cron runner can schedule future maintenance jobs.
type Scheduler struct {
	db           *gorm.DB
	cron         *cron.Cron
	dsRepo       interfaces.DataSourceRepository
	syncLogRepo  interfaces.SyncLogRepository
	taskEnqueuer interfaces.TaskEnqueuer

	mu      sync.Mutex
	entries map[string]cron.EntryID // dataSourceID → cron entry ID
}

// NewScheduler creates a new Scheduler.
func NewScheduler(
	dsRepo interfaces.DataSourceRepository,
	syncLogRepo interfaces.SyncLogRepository,
	taskEnqueuer interfaces.TaskEnqueuer,
) *Scheduler {
	return &Scheduler{
		cron: cron.New(cron.WithSeconds(), cron.WithChain(
			cron.Recover(cron.DefaultLogger),
		)),
		dsRepo:       dsRepo,
		syncLogRepo:  syncLogRepo,
		taskEnqueuer: taskEnqueuer,
		entries:      make(map[string]cron.EntryID),
	}
}

// Start loads all active data sources from the database and registers their
// cron schedules. Then starts the cron runner in the background.
func (s *Scheduler) Start(ctx context.Context) error {
	dataSources, err := s.dsRepo.FindActive(ctx)
	if err != nil {
		return fmt.Errorf("load active data sources: %w", err)
	}

	for _, ds := range dataSources {
		if ds.SyncSchedule == "" {
			continue
		}
		if err := s.addEntry(ds); err != nil {
			logger.Warnf(ctx, "[Scheduler] failed to register cron for ds=%s schedule=%q: %v",
				ds.ID, ds.SyncSchedule, err)
		}
	}

	s.cron.Start()
	logger.Infof(ctx, "[Scheduler] started with %d cron entries", len(s.entries))
	return nil
}

// Stop gracefully stops the cron runner and waits for running jobs to finish.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

// AddOrUpdate registers (or re-registers) a cron entry for the given data source.
func (s *Scheduler) AddOrUpdate(ds *types.DataSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.entries[ds.ID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, ds.ID)
	}

	if ds.Status != types.DataSourceStatusActive || ds.SyncSchedule == "" {
		return nil
	}

	return s.addEntryLocked(ds)
}

// Remove removes the cron entry for a data source.
func (s *Scheduler) Remove(dataSourceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.entries[dataSourceID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, dataSourceID)
	}
}

func (s *Scheduler) addEntry(ds *types.DataSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addEntryLocked(ds)
}

func (s *Scheduler) addEntryLocked(ds *types.DataSource) error {
	dsID := ds.ID
	spaceID := ds.SpaceID

	entryID, err := s.cron.AddFunc(ds.SyncSchedule, func() {
		s.triggerSync(dsID, spaceID)
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", ds.SyncSchedule, err)
	}

	s.entries[dsID] = entryID
	return nil
}

// SetDatabase binds the production scheduler to transactional PostgreSQL writes.
func (s *Scheduler) SetDatabase(db *gorm.DB) { s.db = db }

// triggerSync persists one scheduled synchronization request.
func (s *Scheduler) triggerSync(dataSourceID string, spaceID uint64) {
	ctx := context.Background()
	if s.db != nil {
		if err := s.triggerSyncTx(ctx, dataSourceID, spaceID); err != nil && err != taskqueue.ErrTaskIDConflict {
			logger.Errorf(ctx, "[Scheduler] sync enqueue failed for ds=%s: %v", dataSourceID, err)
		}
		return
	}

	ds, err := s.dsRepo.FindByID(ctx, dataSourceID)
	if err != nil || ds == nil || ds.Status != types.DataSourceStatusActive {
		logger.Infof(ctx, "[Scheduler] skipping sync for ds=%s (not active or not found)", dataSourceID)
		return
	}
	if missing, err := ds.MissingMaintenanceFields(); err != nil || len(missing) > 0 {
		logger.Infof(ctx, "[Scheduler] skipping sync for ds=%s until maintenance configuration is complete", dataSourceID)
		return
	}

	// Layer 1: prevent overlap with a still-running sync
	if running, _ := s.syncLogRepo.HasRunningSync(ctx, dataSourceID); running {
		logger.Infof(ctx, "[Scheduler] skipping sync for ds=%s (previous sync still running)", dataSourceID)
		return
	}

	syncLog := &types.SyncLog{
		DataSourceID: dataSourceID,
		SpaceID:      spaceID,
		Status:       types.SyncLogStatusRunning,
		StartedAt:    time.Now().UTC(),
	}
	if err := s.syncLogRepo.Create(ctx, syncLog); err != nil {
		logger.Errorf(ctx, "[Scheduler] failed to create sync log for ds=%s: %v", dataSourceID, err)
		return
	}

	payload := &types.DataSourceSyncPayload{
		DataSourceID: dataSourceID,
		SpaceID:      spaceID,
		SyncLogID:    syncLog.ID,
		ForceFull:    false,
		Trigger:      "schedule",
	}

	payloadJSON, _ := json.Marshal(payload)
	task := taskqueue.NewTask(types.TypeDataSourceSync, payloadJSON)

	// A stable ID prevents duplicate scheduled tasks within the same minute.
	taskID := fmt.Sprintf("dssync:%s:%s", dataSourceID, time.Now().UTC().Truncate(time.Minute).Format("200601021504"))

	_, err = s.taskEnqueuer.Enqueue(task,
		taskqueue.Queue(types.QueueSync),
		taskqueue.MaxRetry(5),
		taskqueue.Timeout(2*time.Hour),
		taskqueue.TaskID(taskID),
	)
	if err != nil {
		if err == taskqueue.ErrTaskIDConflict {
			logger.Infof(ctx, "[Scheduler] sync already enqueued for ds=%s", dataSourceID)
			syncLog.Status = types.SyncLogStatusCanceled
			now := time.Now().UTC()
			syncLog.FinishedAt = &now
			syncLog.ErrorMessage = "deduplicated: another instance enqueued first"
			_ = s.syncLogRepo.Update(ctx, syncLog)
			return
		}
		logger.Errorf(ctx, "[Scheduler] failed to enqueue sync task for ds=%s: %v", dataSourceID, err)
		syncLog.Status = types.SyncLogStatusFailed
		now := time.Now().UTC()
		syncLog.FinishedAt = &now
		syncLog.ErrorMessage = fmt.Sprintf("enqueue failed: %v", err)
		_ = s.syncLogRepo.Update(ctx, syncLog)
		return
	}

	logger.Infof(ctx, "[Scheduler] sync task enqueued for ds=%s syncLog=%s", dataSourceID, syncLog.ID)
}

// EntryCount returns the number of active cron entries (for testing/monitoring).
func (s *Scheduler) EntryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

func (s *Scheduler) triggerSyncTx(ctx context.Context, id string, spaceID uint64) error {
	enqueuer, ok := s.taskEnqueuer.(interfaces.TransactionalTaskEnqueuer)
	if !ok {
		return fmt.Errorf("scheduler requires a transactional task enqueuer")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := maintenanceops.CheckCollectionTx(ctx, tx); err != nil {
			if maintenanceops.IsPausedError(err) {
				return nil
			}
			return err
		}
		var ds types.DataSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&ds).Error; err != nil {
			return err
		}
		if ds.Status != types.DataSourceStatusActive {
			return nil
		}
		if missing, err := ds.MissingMaintenanceFields(); err != nil || len(missing) > 0 {
			return err
		}
		var running int64
		if err := tx.Model(&types.SyncLog{}).Where("data_source_id = ? AND status = ?", id, types.SyncLogStatusRunning).Count(&running).Error; err != nil {
			return err
		}
		if running > 0 {
			return nil
		}
		log := &types.SyncLog{DataSourceID: id, SpaceID: spaceID, Status: types.SyncLogStatusRunning, StartedAt: time.Now().UTC()}
		if err := tx.Create(log).Error; err != nil {
			return err
		}
		payload, err := json.Marshal(&types.DataSourceSyncPayload{DataSourceID: id, SpaceID: spaceID, SyncLogID: log.ID, Trigger: "schedule"})
		if err != nil {
			return err
		}
		taskID := fmt.Sprintf("dssync:%s:%s", id, time.Now().UTC().Truncate(time.Minute).Format("200601021504"))
		_, err = enqueuer.EnqueueTx(ctx, tx, taskqueue.NewTask(types.TypeDataSourceSync, payload), taskqueue.Queue(types.QueueSync), taskqueue.MaxRetry(5), taskqueue.Timeout(2*time.Hour), taskqueue.TaskID(taskID))
		return err
	})
}
