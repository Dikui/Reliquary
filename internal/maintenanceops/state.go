// Package maintenanceops coordinates collection pauses and host maintenance jobs.
// Snapshots are taken after the application has stopped, so long-running model
// jobs keep their PostgreSQL checkpoints without delaying a backup.
package maintenanceops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apperrors "reliquary/internal/errors"
	taskqueue "reliquary/internal/queue"
)

const collectionLock int64 = 714150015

func IsPausedError(err error) bool {
	var app *apperrors.AppError
	return errors.As(err, &app) && app.Code == apperrors.ErrServiceUnavailable
}

type State struct {
	ID               bool       `json:"-"`
	Paused           bool       `json:"paused"`
	PauseMode        string     `json:"pause_mode"`
	PauseReason      string     `json:"pause_reason"`
	PausedAt         *time.Time `json:"paused_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
	BackupAt         *time.Time `json:"backup_at,omitempty"`
	BackupStatus     string     `json:"backup_status"`
	BackupPath       string     `json:"backup_path"`
	BackupSizeBytes  int64      `json:"backup_size_bytes"`
	BackupError      string     `json:"backup_error"`
	DrillAt          *time.Time `json:"drill_at,omitempty"`
	DrillStatus      string     `json:"drill_status"`
	DrillError       string     `json:"drill_error"`
	BackupSchedule   string     `json:"backup_schedule"`
	DrillSchedule    string     `json:"drill_schedule"`
	ScheduleTimezone string     `json:"schedule_timezone"`
	BackupDirectory  string     `json:"backup_directory"`
	AutomaticEnabled bool       `json:"automatic_enabled"`
	BackupDueAt      *time.Time `json:"backup_due_at,omitempty"`
	DrillDueAt       *time.Time `json:"drill_due_at,omitempty"`
}

func (State) TableName() string { return "maintenance_state" }

func Status(ctx context.Context, db *gorm.DB) (State, error) {
	var state State
	err := db.WithContext(ctx).Where("id = true").Take(&state).Error
	state.BackupDirectory = effectiveBackupDirectory(state.BackupDirectory)
	return state, err
}

// Resolve the historical built-in default on the deployment host without
// changing preserved database configuration or explicit user settings.
func effectiveBackupDirectory(directory string) string {
	if directory != "" && directory != "../reliquary-backups" {
		return directory
	}
	if configured := strings.TrimSpace(os.Getenv("BACKUP_DIR")); configured != "" {
		return configured
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Backups", "Reliquary")
	}
	return directory
}

// SnapshotPaused also suspends automatic reconciliation while credentials or
// models are being configured. Explicit configuration remains available.
func SnapshotPaused(ctx context.Context, db *gorm.DB) (bool, error) {
	state, err := Status(ctx, db)
	return state.Paused && state.PauseMode == "snapshot", err
}

// CheckCollectionTx must run in the same transaction as collection and enqueue.
// A previously accepted article task may finish during a drain.
// New intake requests stay paused.
func CheckCollectionTx(ctx context.Context, tx *gorm.DB) error {
	if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(?)", collectionLock).Error; err != nil {
		return err
	}
	state, err := Status(ctx, tx)
	if err != nil {
		return err
	}
	if !state.Paused {
		return nil
	}
	if _, running := taskqueue.GetTaskID(ctx); running {
		return nil
	}
	return apperrors.NewServiceUnavailableError("Collection is paused for maintenance. Retry after maintenance completes.")
}

// Pause waits for collection transactions that crossed the gate, then prevents
// any new collection. The host stops the application before snapshotting files.
func Pause(ctx context.Context, db *gorm.DB, mode, reason string, flush func(context.Context) error) error {
	if mode != "drain" && mode != "snapshot" {
		return fmt.Errorf("pause mode must be drain or snapshot")
	}
	acquired := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", collectionLock).Error; err != nil {
			return err
		}
		state, err := Status(ctx, tx)
		if err != nil {
			return err
		}
		if state.Paused {
			return errors.New("maintenance is already paused; resume the existing operation before starting another")
		}
		if err := tx.Model(&State{}).Where("id = true").Updates(map[string]any{"paused": true, "pause_mode": mode, "pause_reason": reason, "paused_at": time.Now().UTC(), "updated_at": time.Now().UTC()}).Error; err != nil {
			return err
		}
		acquired = true
		return nil
	})
	if err != nil {
		return err
	}
	if mode == "drain" {
		table := "task_pending_ops"
		// Offline maintenance may inspect a backup before its queue migration.
		if !db.Migrator().HasTable(table) {
			table = "task_jobs"
		}
		for {
			var count int64
			err = db.WithContext(ctx).Table(table).Where("task_type IN ? AND state IN ?", []string{"document:process", "manual:process", "knowledge:list_delete", "knowledge:list_reparse"}, []string{"pending", "dispatched", "active", "scheduled", "retry"}).Count(&count).Error
			if err != nil || count == 0 {
				break
			}
			select {
			case <-ctx.Done():
				err = fmt.Errorf("queue drain timed out with %d accepted tasks; backup or pack was skipped: %w", count, ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
			if err != nil {
				break
			}
		}
	}
	if err == nil && flush != nil {
		err = flush(ctx)
	}
	if err != nil && acquired {
		err = errors.Join(err, Resume(context.Background(), db))
	}
	return err
}

func Resume(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Model(&State{}).Where("id = true").Updates(map[string]any{"paused": false, "pause_mode": "", "pause_reason": "", "paused_at": nil, "updated_at": time.Now().UTC()}).Error
}

type Result struct {
	Status     string     `json:"status"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Path       string     `json:"path,omitempty"`
	SizeBytes  int64      `json:"size_bytes,omitempty"`
	Error      string     `json:"error,omitempty"`
}

func Record(ctx context.Context, db *gorm.DB, kind string, result Result) error {
	if kind != "backup" && kind != "drill" {
		return errors.New("result kind must be backup or drill")
	}
	if result.Status != "running" && result.Status != "success" && result.Status != "failed" && result.Status != "skipped" {
		return errors.New("invalid maintenance result status")
	}
	if result.SizeBytes < 0 {
		return errors.New("backup size cannot be negative")
	}
	when := time.Now().UTC()
	if result.FinishedAt != nil {
		when = *result.FinishedAt
	}
	values := map[string]any{kind + "_at": when, kind + "_status": result.Status, kind + "_error": result.Error, "updated_at": time.Now().UTC()}
	if kind == "backup" {
		values["backup_path"], values["backup_size_bytes"] = result.Path, result.SizeBytes
	}
	return db.WithContext(ctx).Model(&State{}).Where("id = true").Updates(values).Error
}

type ScheduleConfig struct {
	BackupSchedule string `json:"backup_schedule"`
	DrillSchedule  string `json:"drill_schedule"`
	Timezone       string `json:"schedule_timezone"`
	Directory      string `json:"backup_directory"`
	Enabled        bool   `json:"automatic_enabled"`
}

func Configure(ctx context.Context, db *gorm.DB, cfg ScheduleConfig) error {
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return fmt.Errorf("invalid schedule timezone: %w", err)
	}
	for _, expression := range []string{cfg.BackupSchedule, cfg.DrillSchedule} {
		if _, err := cron.ParseStandard(expression); err != nil {
			return fmt.Errorf("invalid maintenance schedule: %w", err)
		}
	}
	if cfg.Directory == "" {
		return errors.New("backup directory is required")
	}
	return db.WithContext(ctx).Model(&State{}).Where("id = true").Updates(map[string]any{"backup_schedule": cfg.BackupSchedule, "drill_schedule": cfg.DrillSchedule, "schedule_timezone": cfg.Timezone, "backup_directory": cfg.Directory, "automatic_enabled": cfg.Enabled, "updated_at": time.Now().UTC()}).Error
}

type DueResult struct {
	Jobs      []string `json:"jobs"`
	Directory string   `json:"backup_directory"`
}

// Due atomically claims scheduled jobs, including a tick missed while the host
// was asleep. The first runner makes an initial backup and drill immediately.
// Concurrent runners cannot create duplicate jobs within the same plan period.
func Due(ctx context.Context, db *gorm.DB, now time.Time, force bool) (DueResult, error) {
	result := DueResult{Jobs: []string{}}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state State
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=true").Take(&state).Error; err != nil {
			return err
		}
		result.Directory = effectiveBackupDirectory(state.BackupDirectory)
		if !state.AutomaticEnabled || state.Paused {
			return nil
		}
		location, err := time.LoadLocation(state.ScheduleTimezone)
		if err != nil {
			return err
		}
		minute := now.In(location).Truncate(time.Minute)
		for _, entry := range []struct {
			kind, expression, status string
			last                     *time.Time
		}{{"backup", state.BackupSchedule, state.BackupStatus, state.BackupDueAt}, {"drill", state.DrillSchedule, state.DrillStatus, state.DrillDueAt}} {
			schedule, err := cron.ParseStandard(entry.expression)
			if err != nil {
				return err
			}
			if entry.last != nil && !entry.last.Before(minute) {
				continue
			}
			retry := entry.last != nil && !entry.last.Add(15*time.Minute).After(minute) &&
				(entry.status == "running" || entry.status == "failed" || entry.status == "skipped")
			if !force && !retry && entry.last != nil && schedule.Next(entry.last.In(location)).After(minute) {
				continue
			}
			if err := tx.Model(&State{}).Where("id=true").Updates(map[string]any{
				entry.kind + "_due_at": minute.UTC(), entry.kind + "_status": "running",
				entry.kind + "_at": now.UTC(), entry.kind + "_error": "",
			}).Error; err != nil {
				return err
			}
			result.Jobs = append(result.Jobs, entry.kind)
		}
		return nil
	})
	return result, err
}
