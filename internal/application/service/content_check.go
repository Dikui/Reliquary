package service

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

//go:embed reliquary-content-check/SKILL.md
var contentCheckSkill string

type contentCheckStrictKey struct{}

func strictContentCheck(ctx context.Context) bool {
	v, _ := ctx.Value(contentCheckStrictKey{}).(bool)
	return v
}

type ContentCheckService struct {
	db       *gorm.DB
	tasks    interfaces.TaskEnqueuer
	models   interfaces.ModelService
	entries  interfaces.EntryService
	search   interfaces.SearchService
	entities *EntityService
	graph    *GraphSearchService
}

func NewContentCheckService(db *gorm.DB, tasks interfaces.TaskEnqueuer, models interfaces.ModelService, entries interfaces.EntryService, search interfaces.SearchService, entities *EntityService, graph *GraphSearchService) *ContentCheckService {
	return &ContentCheckService{db: db, tasks: tasks, models: models, entries: entries, search: search, entities: entities, graph: graph}
}
func nextContentCheck(now time.Time, weekday int, clock string) time.Time {
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	now = now.In(loc)
	parts := strings.Split(clock, ":")
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	next := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc).AddDate(0, 0, (weekday-int(now.Weekday())+7)%7)
	if !next.After(now) {
		next = next.AddDate(0, 0, 7)
	}
	return next.UTC()
}
func (s *ContentCheckService) Settings(ctx context.Context) (*types.ContentCheckSettings, error) {
	out := &types.ContentCheckSettings{SpaceID: types.MustSpaceIDFromContext(ctx), Weekday: 1}
	err := s.db.WithContext(ctx).First(out, "space_id=?", out.SpaceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, nil
	}
	return out, err
}
func (s *ContentCheckService) Configure(ctx context.Context, in types.ContentCheckSettings) error {
	in.SpaceID = types.MustSpaceIDFromContext(ctx)
	if in.Weekday < 0 || in.Weekday > 6 {
		return apperrors.NewBadRequestError("weekday must be between 0 and 6")
	}
	if in.Enabled {
		if _, err := time.Parse("15:04", in.Time); err != nil || len(in.Time) != 5 {
			return apperrors.NewBadRequestError("time must be HH:MM (Asia/Shanghai)")
		}
		var model types.Model
		if err := s.db.WithContext(ctx).Where("id=? AND (space_id=? OR is_builtin=true) AND type=? AND status=?", in.ModelID, in.SpaceID, types.ModelTypeGeneration, types.ModelStatusActive).First(&model).Error; err != nil {
			return apperrors.NewBadRequestError("select an active generation model")
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		initial := types.ContentCheckSettings{SpaceID: in.SpaceID, Weekday: 1, ScanAfter: time.Now().UTC()}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		var next *time.Time
		if in.Enabled {
			n := nextContentCheck(time.Now(), in.Weekday, in.Time)
			next = &n
		}
		return tx.Model(&types.ContentCheckSettings{}).Where("space_id=?", in.SpaceID).Updates(map[string]any{"enabled": in.Enabled, "model_id": in.ModelID, "weekday": in.Weekday, "time": in.Time, "next_run_at": next}).Error
	})
}
func (s *ContentCheckService) Get(ctx context.Context, id string) (*types.ContentCheckRun, error) {
	var out types.ContentCheckRun
	err := s.db.WithContext(ctx).Where("id=? AND space_id=?", id, types.MustSpaceIDFromContext(ctx)).First(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NewNotFoundError("check run not found")
	}
	out.Total = len(out.Targets)
	return &out, err
}
func (s *ContentCheckService) List(ctx context.Context, page int) (*types.ContentCheckPage, error) {
	if page < 1 || page > 1000000 {
		return nil, apperrors.NewBadRequestError("invalid page")
	}
	out := &types.ContentCheckPage{Items: []types.ContentCheckRun{}}
	q := s.db.WithContext(ctx).Model(&types.ContentCheckRun{}).Where("space_id=?", types.MustSpaceIDFromContext(ctx))
	if err := q.Count(&out.Total).Error; err != nil {
		return nil, err
	}
	// Reports are fetched on demand, keeping the list bounded even for long runs.
	err := q.Omit("results").Order("created_at DESC,id DESC").Offset((page - 1) * 20).Limit(20).Find(&out.Items).Error
	for i := range out.Items {
		out.Items[i].Total = len(out.Items[i].Targets)
		out.Items[i].Targets = nil
	}
	return out, err
}
func (s *ContentCheckService) Start(ctx context.Context, previous string) (*types.ContentCheckRun, error) {
	var run *types.ContentCheckRun
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { var err error; run, err = s.startTx(ctx, tx, previous, false); return err })
	return run, err
}
func (s *ContentCheckService) startTx(ctx context.Context, tx *gorm.DB, previous string, scheduled bool) (*types.ContentCheckRun, error) {
	space := types.MustSpaceIDFromContext(ctx)
	var settings types.ContentCheckSettings
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "space_id=?", space).Error; err != nil {
		return nil, apperrors.NewBadRequestError("configure the check model and schedule first")
	}
	now := time.Now().UTC()
	if scheduled && (!settings.Enabled || settings.NextRunAt == nil || settings.NextRunAt.After(now)) {
		return nil, nil
	}
	paused, err := maintenanceops.SnapshotPaused(ctx, tx)
	if err != nil {
		return nil, err
	}
	if paused {
		return nil, apperrors.NewConflictError("maintenance is paused")
	}
	var active int64
	if err := tx.Model(&types.ContentCheckRun{}).Where("space_id=? AND status IN ?", space, []string{"pending", "running"}).Count(&active).Error; err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, apperrors.NewConflictError("a check is already running")
	}
	if strings.TrimSpace(settings.ModelID) == "" {
		return nil, apperrors.NewBadRequestError("select a check model")
	}
	run := &types.ContentCheckRun{ID: uuid.NewString(), SpaceID: space, ModelID: settings.ModelID, PreviousID: previous, Status: "pending", WindowFrom: settings.ScanAfter, WindowTo: now, Targets: []string{}, Results: []types.ContentCheckResult{}, CreatedAt: now}
	if previous != "" {
		var old types.ContentCheckRun
		if err := tx.Where("id=? AND space_id=? AND status='failed'", previous, space).First(&old).Error; err != nil {
			return nil, apperrors.NewBadRequestError("only a failed check can be rechecked")
		}
		var retried int64
		if err := tx.Model(&types.ContentCheckRun{}).Where("previous_id=? AND status IN ?", previous, []string{"pending", "running", "completed"}).Count(&retried).Error; err != nil {
			return nil, err
		}
		if retried > 0 {
			return nil, apperrors.NewConflictError("this check already has a recheck")
		}
		run.WindowFrom, run.WindowTo = old.WindowFrom, old.WindowTo
		run.Targets = append(run.Targets, old.Targets[old.Completed:]...)
	} else {
		var last types.ContentCheckRun
		err := tx.Where("space_id=?", space).Order("created_at DESC").First(&last).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if last.Status == "failed" {
			return nil, apperrors.NewConflictError("recheck the failed run first")
		}
		if err := tx.Model(&types.Entry{}).Where("space_id=? AND updated_at>? AND updated_at<=? AND parse_status NOT IN ?", space, settings.ScanAfter, now, []string{types.ParseStatusRejected, types.ParseStatusDeleting}).Order("updated_at,id").Pluck("id", &run.Targets).Error; err != nil {
			return nil, err
		}
	}
	if len(run.Targets) == 0 {
		run.Status = "completed"
		run.FinishedAt = &now
	}
	if err := tx.Create(run).Error; err != nil {
		return nil, err
	}
	if run.Status == "pending" {
		enqueuer, ok := s.tasks.(interfaces.TransactionalTaskEnqueuer)
		if !ok {
			return nil, errors.New("transactional task queue unavailable")
		}
		payload, _ := json.Marshal(map[string]any{"space_id": space, "run_id": run.ID})
		// A whole check can span many model calls; the model's own call timeout still applies.
		_, err := enqueuer.EnqueueTx(ctx, tx, queue.NewTask(types.TypeContentCheck, payload), queue.TaskID(run.ID), queue.Queue(types.QueueMaintenance), queue.MaxRetry(0), queue.Timeout(7*24*time.Hour))
		if err != nil {
			return nil, err
		}
	} else {
		if err := tx.Model(&settings).Update("scan_after", run.WindowTo).Error; err != nil {
			return nil, err
		}
	}
	if settings.Enabled {
		next := nextContentCheck(now, settings.Weekday, settings.Time)
		if err := tx.Model(&settings).Update("next_run_at", next).Error; err != nil {
			return nil, err
		}
	}
	return run, nil
}
func (s *ContentCheckService) Tick(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := s.startTx(ctx, tx, "", true)
		var app *apperrors.AppError
		if errors.As(err, &app) {
			return nil
		}
		return err
	})
}
func (s *ContentCheckService) fail(ctx context.Context, id string, cause error) error {
	now := time.Now().UTC()
	return s.db.WithContext(context.WithoutCancel(ctx)).Model(&types.ContentCheckRun{}).Where("id=? AND status IN ?", id, []string{"pending", "running"}).Updates(map[string]any{"status": "failed", "error": cause.Error(), "finished_at": now}).Error
}

// FailTask mirrors terminal queue failures without retrying the check or losing checkpoints.
func (s *ContentCheckService) FailTask(ctx context.Context, task *queue.Task, cause error) error {
	var payload struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.RunID == "" {
		return errors.New("invalid check task")
	}
	return s.fail(ctx, payload.RunID, cause)
}
func (s *ContentCheckService) Handle(ctx context.Context, task *queue.Task) error {
	var payload struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("invalid check: %w", queue.SkipRetry)
	}
	run, err := s.Get(ctx, payload.RunID)
	if err != nil {
		return err
	}
	if run.Status == "failed" || run.Status == "completed" {
		return nil
	}
	if run.Status == "running" {
		return s.fail(ctx, run.ID, errors.New("check interrupted; choose recheck to continue"))
	}
	if err := s.db.WithContext(ctx).Model(run).Update("status", "running").Error; err != nil {
		return err
	}
	ctx = context.WithValue(ctx, contentCheckStrictKey{}, true)
	var metadata types.Model
	if err := s.db.WithContext(ctx).Where("id=? AND (space_id=? OR is_builtin=true) AND type=? AND status=?", run.ModelID, run.SpaceID, types.ModelTypeGeneration, types.ModelStatusActive).First(&metadata).Error; err != nil {
		return s.fail(ctx, run.ID, err)
	}
	model, err := s.models.GetChatModel(ctx, run.ModelID)
	if err != nil {
		return s.fail(ctx, run.ID, err)
	}
	var libraries []string
	if err := s.db.WithContext(ctx).Model(&types.Library{}).Where("space_id=?", run.SpaceID).Pluck("id", &libraries).Error; err != nil {
		return s.fail(ctx, run.ID, err)
	}
	for run.Completed < len(run.Targets) {
		id := run.Targets[run.Completed]
		findings, err := s.checkEntry(ctx, model, &metadata, id, libraries)
		if err != nil {
			return s.fail(ctx, run.ID, err)
		}
		seen := map[string]bool{}
		for _, result := range run.Results {
			for _, f := range result.Findings {
				seen[contentCheckFindingKey(f)] = true
			}
		}
		unique := []types.ContentCheckFinding{}
		for _, f := range findings {
			key := contentCheckFindingKey(f)
			if !seen[key] {
				unique = append(unique, f)
				seen[key] = true
			}
		}
		run.Results = append(run.Results, types.ContentCheckResult{EntryID: id, Findings: unique})
		run.Completed++
		if err := s.db.WithContext(ctx).Model(run).Select("results", "completed").Updates(run).Error; err != nil {
			return s.fail(ctx, run.ID, err)
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Model(run).Updates(map[string]any{"status": "completed", "finished_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&types.ContentCheckSettings{}).Where("space_id=? AND scan_after<?", run.SpaceID, run.WindowTo).Update("scan_after", run.WindowTo).Error
	})
}

func contentCheckFindingKey(f types.ContentCheckFinding) string {
	parts := []string{f.Kind}
	for _, e := range f.Evidence {
		parts = append(parts, e.EntryID+"\n"+e.Quote)
	}
	sort.Strings(parts[1:])
	return strings.Join(parts, "\x00")
}
