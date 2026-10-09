package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/robfig/cron/v3"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/types"
)

// Resume restarts a run paused because its workflow was disabled while queued or running.
func (s *KnowledgeWorkflowService) Resume(ctx context.Context, kbID, runID string) (*types.KnowledgeWorkflowRun, error) {
	run, err := s.getRun(ctx, kbID, runID)
	if err != nil {
		return nil, err
	}
	wf, err := s.getWorkflow(ctx, kbID, run.WorkflowID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeWorkflow(ctx, wf); err != nil {
		return nil, err
	}
	if !wf.Enabled {
		return nil, ErrWorkflowDisabled
	}
	if run.Status != types.WorkflowStatusPaused {
		return nil, fmt.Errorf("run cannot be resumed from status %q", run.Status)
	}
	ownerCtx := context.WithValue(ctx, types.UserIDContextKey, run.OwnerID)
	err = s.validateActor(ownerCtx, run.TenantID, run.OwnerID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&types.KnowledgeWorkflowRun{}).Where("id = ? AND status = ?", runID, types.WorkflowStatusPaused).Updates(map[string]any{
		"status": types.WorkflowStatusQueued, "error": "", "finished_at": nil, "updated_at": now,
	})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("run cannot be resumed from status %q", run.Status)
	}
	payload := types.KnowledgeWorkflowTaskPayload{RunID: runID, Initiator: types.TaskInitiator{UserID: run.OwnerID}}
	if _, err := s.enqueueExisting(ctx, run, payload); err != nil {
		return nil, err
	}
	return s.GetRun(ctx, kbID, runID)
}

// Retry queues a failed run from its first incomplete step. Succeeded outputs
// stay committed and are skipped by Handle.
func (s *KnowledgeWorkflowService) Retry(ctx context.Context, kbID, runID string) (*types.KnowledgeWorkflowRun, error) {
	run, err := s.getRun(ctx, kbID, runID)
	if err != nil {
		return nil, err
	}
	wf, err := s.getWorkflow(ctx, kbID, run.WorkflowID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeWorkflow(ctx, wf); err != nil {
		return nil, err
	}
	if run.Status != types.WorkflowStatusFailed {
		return nil, fmt.Errorf("run cannot be retried from status %q", run.Status)
	}
	if !wf.Enabled {
		return nil, ErrWorkflowDisabled
	}
	roleCtx := context.WithValue(ctx, types.UserIDContextKey, run.OwnerID)
	err = s.validateActor(roleCtx, run.TenantID, run.OwnerID)
	if err != nil {
		return nil, err
	}
	result := s.db.WithContext(ctx).Model(&types.KnowledgeWorkflowRun{}).Where("id = ? AND status = ?", runID, types.WorkflowStatusFailed).Updates(map[string]any{
		"status": types.WorkflowStatusQueued, "error": "", "finished_at": nil, "updated_at": time.Now(),
	})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, errors.New("workflow run is no longer failed")
	}
	payload := types.KnowledgeWorkflowTaskPayload{RunID: runID, Initiator: types.TaskInitiator{UserID: run.OwnerID}}
	if _, err := s.enqueueExisting(ctx, run, payload); err != nil {
		return nil, err
	}
	return s.GetRun(ctx, kbID, runID)
}

func (s *KnowledgeWorkflowService) enqueueExisting(ctx context.Context, run *types.KnowledgeWorkflowRun, payload types.KnowledgeWorkflowTaskPayload) (*types.KnowledgeWorkflowRun, error) {
	if s.tasks == nil {
		err := errors.New("workflow task enqueuer is unavailable")
		_ = s.db.WithContext(ctx).Model(&types.KnowledgeWorkflowRun{}).Where("id = ?", run.ID).Updates(map[string]any{
			"status": types.WorkflowStatusFailed, "error": err.Error(), "finished_at": time.Now(), "updated_at": time.Now(),
		}).Error
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	_, err = s.tasks.Enqueue(asynq.NewTask(TypeKnowledgeWorkflowRun, body), asynq.Queue(types.QueueMaintenance), asynq.TaskID("workflow-resume:"+run.ID+":"+uuid.NewString()))
	if err != nil {
		_ = s.db.WithContext(ctx).Model(&types.KnowledgeWorkflowRun{}).Where("id = ?", run.ID).Updates(map[string]any{
			"status": types.WorkflowStatusFailed, "error": "enqueue resume: " + err.Error(), "finished_at": time.Now(), "updated_at": time.Now(),
		}).Error
		return nil, fmt.Errorf("enqueue workflow resume: %w", err)
	}
	return run, nil
}

// Start reloads enabled workflow definitions on each minute tick. This keeps
// schedule edits and disables effective without a cache or Redis dependency.
func (s *KnowledgeWorkflowService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.cron != nil {
		s.mu.Unlock()
		return nil
	}
	runner := cron.New(cron.WithChain(cron.Recover(cron.DefaultLogger)))
	if _, err := runner.AddFunc("@every 1m", func() {
		if err := s.Tick(ctx); err != nil {
			log.Printf("knowledge workflow scheduler: %v", err)
		}
	}); err != nil {
		s.mu.Unlock()
		return err
	}
	s.cron = runner
	s.mu.Unlock()
	runner.Start()
	return s.Tick(ctx)
}

func (s *KnowledgeWorkflowService) Stop() {
	s.mu.Lock()
	runner := s.cron
	s.cron = nil
	s.mu.Unlock()
	if runner != nil {
		<-runner.Stop().Done()
	}
}

// Tick is also exposed for deterministic scheduler checks and operators.
// The run idempotency key includes the UTC minute, so concurrent replicas and
// repeated ticks converge on one run row before task enqueueing.
func (s *KnowledgeWorkflowService) Tick(ctx context.Context) error {
	var workflows []types.KnowledgeWorkflow
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&workflows).Error; err != nil {
		return err
	}
	now := time.Now()
	var firstErr error
	for i := range workflows {
		wf := &workflows[i]
		if strings.TrimSpace(wf.Schedule.Cron) == "" {
			continue
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		schedule, err := parser.Parse(wf.Schedule.Cron)
		if err != nil {
			firstErr = errors.Join(firstErr, fmt.Errorf("workflow %s has invalid cron: %w", wf.ID, err))
			continue
		}
		loc := time.UTC
		if wf.Schedule.Timezone != "" {
			loc, err = time.LoadLocation(wf.Schedule.Timezone)
			if err != nil {
				firstErr = errors.Join(firstErr, fmt.Errorf("workflow %s timezone: %w", wf.ID, err))
				continue
			}
		}
		localMinute := now.In(loc).Truncate(time.Minute)
		next := schedule.Next(localMinute.Add(-time.Minute))
		if next.After(now.In(loc)) {
			continue
		}
		key := "schedule:" + wf.ID + ":" + localMinute.UTC().Format("20060102T1504Z")
		var existing types.KnowledgeWorkflowRun
		lookupErr := s.db.WithContext(ctx).Where("idempotency_key = ?", key).First(&existing).Error
		if lookupErr == nil {
			continue
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			firstErr = errors.Join(firstErr, lookupErr)
			continue
		}
		tenantCtx := context.WithValue(ctx, types.TenantIDContextKey, wf.TenantID)
		tenantCtx = context.WithValue(tenantCtx, types.UserIDContextKey, wf.OwnerID)
		roleErr := s.validateActor(tenantCtx, wf.TenantID, wf.OwnerID)
		if roleErr != nil {
			firstErr = errors.Join(firstErr, fmt.Errorf("workflow %s owner authorization: %w", wf.ID, roleErr))
		}
		run := &types.KnowledgeWorkflowRun{
			ID: uuid.NewString(), TenantID: wf.TenantID, KnowledgeBaseID: wf.KnowledgeBaseID,
			WorkflowID: wf.ID, OwnerID: wf.OwnerID, Trigger: "scheduled", IdempotencyKey: key, Status: types.WorkflowStatusQueued,
			WorkflowSnapshot: normalizeWorkflowScopes(wf.Steps),
		}
		payload := types.KnowledgeWorkflowTaskPayload{RunID: run.ID, Initiator: types.TaskInitiator{UserID: wf.OwnerID}}
		if _, createErr := s.createScheduledRun(ctx, run, payload); createErr != nil {
			firstErr = errors.Join(firstErr, createErr)
		}
	}
	return errors.Join(firstErr, s.recoverWorkflowRuns(ctx, now))
}

func (s *KnowledgeWorkflowService) recoverWorkflowRuns(ctx context.Context, now time.Time) error {
	staleBefore := now.Add(-2 * time.Minute)
	var runs []types.KnowledgeWorkflowRun
	if err := s.db.WithContext(ctx).Where(
		"(status = ? AND updated_at < ?) OR (status = ? AND updated_at < ?)",
		types.WorkflowStatusQueued, staleBefore, types.WorkflowStatusRunning, staleBefore,
	).Order("updated_at ASC").Limit(100).Find(&runs).Error; err != nil {
		return err
	}
	var firstErr error
	for i := range runs {
		run := &runs[i]
		actorCtx := context.WithValue(ctx, types.TenantIDContextKey, run.TenantID)
		actorCtx = context.WithValue(actorCtx, types.UserIDContextKey, run.OwnerID)
		err := s.validateActor(actorCtx, run.TenantID, run.OwnerID)
		if err != nil {
			firstErr = errors.Join(firstErr, fmt.Errorf("workflow run %s owner authorization: %w", run.ID, err))
			_ = s.db.WithContext(ctx).Model(&types.KnowledgeWorkflowRun{}).Where("id = ? AND updated_at = ? AND status = ?", run.ID, run.UpdatedAt, run.Status).
				Updates(map[string]any{"status": types.WorkflowStatusFailed, "error": "workflow owner is no longer authorized", "finished_at": now, "updated_at": now}).Error
			continue
		}
		updates := map[string]any{"updated_at": now}
		query := s.db.WithContext(ctx).Model(&types.KnowledgeWorkflowRun{}).Where("id = ? AND updated_at = ? AND status = ?", run.ID, run.UpdatedAt, run.Status)
		switch run.Status {
		case types.WorkflowStatusQueued:
		case types.WorkflowStatusRunning:
			updates["status"] = types.WorkflowStatusQueued
			updates["lease_token"] = ""
			updates["error"] = "recovered stale worker lease"
		default:
			continue
		}
		result := query.Updates(updates)
		if result.Error != nil {
			firstErr = errors.Join(firstErr, result.Error)
			continue
		}
		if result.RowsAffected != 1 {
			continue
		}
		if s.tasks == nil {
			err := errors.New("workflow task enqueuer is unavailable")
			_ = s.failUnclaimedRun(ctx, run, err.Error())
			firstErr = errors.Join(firstErr, err)
			continue
		}
		payload, err := json.Marshal(types.KnowledgeWorkflowTaskPayload{
			RunID: run.ID, Initiator: types.TaskInitiator{UserID: run.OwnerID},
		})
		if err != nil {
			firstErr = errors.Join(firstErr, err)
			continue
		}
		_, err = s.tasks.Enqueue(asynq.NewTask(TypeKnowledgeWorkflowRun, payload), asynq.Queue(types.QueueMaintenance),
			asynq.TaskID("workflow-recovery:"+run.ID+":"+uuid.NewString()))
		if err != nil {
			failure := "recovery enqueue: " + err.Error()
			_ = s.db.WithContext(ctx).Model(&types.KnowledgeWorkflowRun{}).Where("id = ? AND status = ? AND updated_at = ?", run.ID, types.WorkflowStatusQueued, now).
				Updates(map[string]any{"status": types.WorkflowStatusFailed, "error": failure, "finished_at": now, "updated_at": time.Now()}).Error
			firstErr = errors.Join(firstErr, fmt.Errorf("enqueue recovered workflow %s: %w", run.ID, err))
		}
	}
	return firstErr
}

func (s *KnowledgeWorkflowService) createScheduledRun(ctx context.Context, run *types.KnowledgeWorkflowRun, payload types.KnowledgeWorkflowTaskPayload) (*types.KnowledgeWorkflowRun, error) {
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "idempotency_key"}}, DoNothing: true}).Create(run).Error; err != nil {
		return nil, err
	}
	var stored types.KnowledgeWorkflowRun
	if err := s.db.WithContext(ctx).Where("idempotency_key = ?", run.IdempotencyKey).First(&stored).Error; err != nil {
		return nil, err
	}
	// A no-op insert means another tick or replica owns this scheduled run.
	if stored.ID != run.ID {
		return &stored, nil
	}
	if s.tasks == nil {
		err := errors.New("workflow task enqueuer is unavailable")
		_ = s.db.WithContext(ctx).Model(&types.KnowledgeWorkflowRun{}).Where("id = ?", run.ID).Updates(map[string]any{
			"status": types.WorkflowStatusFailed, "error": err.Error(), "finished_at": time.Now(), "updated_at": time.Now(),
		}).Error
		return &stored, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	_, err = s.tasks.Enqueue(asynq.NewTask(TypeKnowledgeWorkflowRun, body), asynq.Queue(types.QueueMaintenance), asynq.TaskID("workflow-scheduled:"+run.ID))
	if err != nil {
		_ = s.db.WithContext(ctx).Model(&types.KnowledgeWorkflowRun{}).Where("id = ?", run.ID).Updates(map[string]any{
			"status": types.WorkflowStatusFailed, "error": "enqueue: " + err.Error(), "finished_at": time.Now(), "updated_at": time.Now(),
		}).Error
		return nil, fmt.Errorf("enqueue scheduled workflow %s: %w", run.WorkflowID, err)
	}
	return &stored, nil
}

func workflowActor(ctx context.Context) string {
	userID, _ := types.UserIDFromContext(ctx)
	return userID
}
