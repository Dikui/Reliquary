package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/robfig/cron/v3"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

const TypeKnowledgeWorkflowRun = "knowledge_workflow:run"

var (
	ErrWorkflowNotFound  = errors.New("knowledge workflow not found")
	ErrWorkflowForbidden = errors.New("knowledge workflow access denied")
	ErrWorkflowInvalid   = errors.New("invalid knowledge workflow")
	ErrWorkflowLeaseLost = errors.New("knowledge workflow run lease lost")
)

type KnowledgeWorkflowService struct {
	db        *gorm.DB
	kbs       interfaces.KnowledgeBaseService
	knowledge interfaces.KnowledgeService
	context   *KnowledgeContextService
	models    interfaces.ModelService
	tasks     interfaces.TaskEnqueuer
	cron      *cron.Cron
	mu        sync.Mutex
}

func NewKnowledgeWorkflowService(
	db *gorm.DB,
	kbs interfaces.KnowledgeBaseService,
	knowledge interfaces.KnowledgeService,
	contextService *KnowledgeContextService,
	models interfaces.ModelService,
	tasks interfaces.TaskEnqueuer,
) *KnowledgeWorkflowService {
	return &KnowledgeWorkflowService{
		db: db, kbs: kbs, knowledge: knowledge, context: contextService,
		models: models, tasks: tasks,
	}
}

var workflowStepIDPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,127}$`)

func validateWorkflowInput(in types.KnowledgeWorkflowInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 255 {
		return fmt.Errorf("%w: name is required and must be at most 255 characters", ErrWorkflowInvalid)
	}
	if len(in.Steps) == 0 || len(in.Steps) > 100 {
		return fmt.Errorf("%w: workflow must contain 1 to 100 steps", ErrWorkflowInvalid)
	}
	if strings.TrimSpace(in.Schedule.Cron) != "" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(in.Schedule.Cron); err != nil {
			return fmt.Errorf("%w: invalid schedule cron: %v", ErrWorkflowInvalid, err)
		}
		if strings.TrimSpace(in.Schedule.Timezone) != "" {
			if _, err := time.LoadLocation(in.Schedule.Timezone); err != nil {
				return fmt.Errorf("%w: invalid schedule timezone: %v", ErrWorkflowInvalid, err)
			}
		}
	}
	seen := make(map[string]types.KnowledgeWorkflowStep, len(in.Steps))
	for i, step := range in.Steps {
		if !workflowStepIDPattern.MatchString(step.ID) {
			return fmt.Errorf("%w: step %d has invalid id", ErrWorkflowInvalid, i)
		}
		if _, ok := seen[step.ID]; ok {
			return fmt.Errorf("%w: duplicate step id %q", ErrWorkflowInvalid, step.ID)
		}
		if !validWorkflowStepType(step.Type) {
			return fmt.Errorf("%w: unsupported step type %q", ErrWorkflowInvalid, step.Type)
		}
		if step.Config == nil {
			step.Config = map[string]any{}
		}
		for _, dependency := range step.DependsOn {
			if _, ok := seen[dependency]; !ok {
				return fmt.Errorf("%w: step %q depends on missing or later step %q", ErrWorkflowInvalid, step.ID, dependency)
			}
		}
		if err := validateWorkflowStepConfig(step); err != nil {
			return fmt.Errorf("%w: step %q: %v", ErrWorkflowInvalid, step.ID, err)
		}
		seen[step.ID] = step
	}
	return nil
}

func validWorkflowStepType(stepType string) bool {
	switch stepType {
	case types.WorkflowStepSearch, types.WorkflowStepFactQuery,
		types.WorkflowStepReport:
		return true
	default:
		return false
	}
}

func validateWorkflowStepConfig(step types.KnowledgeWorkflowStep) error {
	str := func(key string) string { value, _ := step.Config[key].(string); return strings.TrimSpace(value) }
	switch step.Type {
	case types.WorkflowStepSearch:
		if str("query") == "" {
			return errors.New("query is required")
		}
	case types.WorkflowStepFactQuery:
		if str("entity") == "" && str("entity_id") == "" && str("predicate") == "" && str("relation") == "" {
			return errors.New("entity, attribute predicate or relation is required")
		}
		cfg, err := decodeWorkflowConfig[workflowFactConfig](step.Config)
		if err != nil {
			return err
		}
		switch cfg.Scope {
		case "", "knowledge_base", "all", "team":
		case "domain":
			if strings.TrimSpace(cfg.DomainID) == "" {
				return errors.New("domain_id is required for domain scope")
			}
		default:
			return errors.New("invalid fact query scope")
		}
		if (cfg.MinValue != nil || cfg.MaxValue != nil) && str("predicate") == "" {
			return errors.New("numeric comparison requires an attribute predicate")
		}
		if cfg.MinValue != nil && cfg.MaxValue != nil && *cfg.MinValue > *cfg.MaxValue {
			return errors.New("invalid numeric range")
		}
	case types.WorkflowStepReport:
		if str("title") == "" {
			return errors.New("title is required")
		}
		window := str("window")
		if window != "" && window != "previous_day" && window != "previous_week" && window != "rolling_days" {
			return errors.New("unsupported report window")
		}
		if window != "" && (str("time_from") != "" || str("time_to") != "") {
			return errors.New("report window cannot be combined with explicit time bounds")
		}
		if window == "rolling_days" && (workflowInt(step.Config["days"]) < 1 || workflowInt(step.Config["days"]) > 365) {
			return errors.New("rolling_days requires days between 1 and 365")
		}
		if tz := str("timezone"); tz != "" {
			if _, err := time.LoadLocation(tz); err != nil {
				return errors.New("timezone must be a valid IANA timezone")
			}
		}

	}
	return nil
}

func workflowInt(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

func (s *KnowledgeWorkflowService) Create(ctx context.Context, kbID string, in types.KnowledgeWorkflowInput) (*types.KnowledgeWorkflow, error) {
	in.Steps = normalizeWorkflowScopes(in.Steps)
	kb, tenantID, ownerID, err := s.authorizeKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err := validateWorkflowInput(in); err != nil {
		return nil, err
	}
	if in.Schedule.Timezone == "" {
		in.Schedule.Timezone = "UTC"
	}
	wf := &types.KnowledgeWorkflow{
		ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kb.ID,
		OwnerID: ownerID, Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description),
		Enabled: in.Enabled, Schedule: in.Schedule, Steps: in.Steps,
	}
	if err := s.db.WithContext(ctx).Create(wf).Error; err != nil {
		return nil, err
	}
	return wf, nil
}

func (s *KnowledgeWorkflowService) Update(ctx context.Context, kbID, id string, in types.KnowledgeWorkflowInput) (*types.KnowledgeWorkflow, error) {
	in.Steps = normalizeWorkflowScopes(in.Steps)
	wf, err := s.getWorkflow(ctx, kbID, id)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeWorkflow(ctx, wf); err != nil {
		return nil, err
	}
	if err := validateWorkflowInput(in); err != nil {
		return nil, err
	}
	if in.Schedule.Timezone == "" {
		in.Schedule.Timezone = "UTC"
	}
	wf.Name, wf.Description, wf.Enabled = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), in.Enabled
	wf.Schedule, wf.Steps = in.Schedule, in.Steps
	if err := s.db.WithContext(ctx).Save(wf).Error; err != nil {
		return nil, err
	}
	return wf, nil
}

// Delete removes only the editable workflow definition. Runs, reports and
// their citations remain available for audit and download.
func (s *KnowledgeWorkflowService) Delete(ctx context.Context, kbID, id string) error {
	wf, err := s.getWorkflow(ctx, kbID, id)
	if err != nil {
		return err
	}
	if err := s.authorizeWorkflow(ctx, wf); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(wf).Error
}

func (s *KnowledgeWorkflowService) List(ctx context.Context, kbID string) ([]types.KnowledgeWorkflow, error) {
	_, tenantID, _, err := s.authorizeKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)

	var workflows []types.KnowledgeWorkflow
	err = query.Order("updated_at DESC").Find(&workflows).Error
	return workflows, err
}

func (s *KnowledgeWorkflowService) Get(ctx context.Context, kbID, id string) (*types.KnowledgeWorkflow, error) {
	wf, err := s.getWorkflow(ctx, kbID, id)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeWorkflow(ctx, wf); err != nil {
		return nil, err
	}
	return wf, nil
}

func (s *KnowledgeWorkflowService) ListRuns(ctx context.Context, kbID, workflowID string) ([]types.KnowledgeWorkflowRun, error) {
	_, tenantID, _, err := s.authorizeKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)
	if workflowID != "" {
		wf, err := s.getWorkflow(ctx, kbID, workflowID)
		if err != nil {
			return nil, err
		}
		if err := s.authorizeWorkflow(ctx, wf); err != nil {
			return nil, err
		}
		query = query.Where("workflow_id = ?", workflowID)
	}
	var runs []types.KnowledgeWorkflowRun
	err = query.Order("created_at DESC").Limit(200).Find(&runs).Error
	return runs, err
}

func (s *KnowledgeWorkflowService) GetRun(ctx context.Context, kbID, runID string) (*types.KnowledgeWorkflowRun, error) {
	run, err := s.getRun(ctx, kbID, runID)
	if err != nil {
		return nil, err
	}
	wf, err := s.getWorkflow(ctx, kbID, run.WorkflowID)
	if err != nil && !errors.Is(err, ErrWorkflowNotFound) {
		return nil, err
	}
	if wf != nil {
		if err := s.authorizeWorkflow(ctx, wf); err != nil {
			return nil, err
		}
	} else if err := s.authorizeActor(ctx, run.TenantID, run.OwnerID); err != nil {
		return nil, err
	}
	return s.loadRunSteps(ctx, run)
}

func (s *KnowledgeWorkflowService) ListReports(ctx context.Context, kbID string) ([]types.KnowledgeWorkflowReport, error) {
	_, tenantID, _, err := s.authorizeKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	var reports []types.KnowledgeWorkflowReport
	query := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)

	err = query.Order("created_at DESC").Limit(200).Find(&reports).Error
	return reports, err
}

func (s *KnowledgeWorkflowService) GetReport(ctx context.Context, kbID, reportID string) (*types.KnowledgeWorkflowReport, error) {
	_, tenantID, _, err := s.authorizeKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	var report types.KnowledgeWorkflowReport
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", reportID, tenantID, kbID).First(&report).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWorkflowNotFound
		}
		return nil, err
	}
	wf, workflowErr := s.getWorkflow(ctx, kbID, report.WorkflowID)
	if workflowErr == nil {
		if err := s.authorizeWorkflow(ctx, wf); err != nil {
			return nil, err
		}
	} else if !errors.Is(workflowErr, ErrWorkflowNotFound) {
		return nil, workflowErr
	} else {
		run, err := s.getRun(ctx, kbID, report.RunID)
		if err != nil {
			return nil, err
		}
		if err := s.authorizeActor(ctx, tenantID, run.OwnerID); err != nil {
			return nil, err
		}
	}
	return &report, nil
}

func (s *KnowledgeWorkflowService) getWorkflow(ctx context.Context, kbID, id string) (*types.KnowledgeWorkflow, error) {
	_, tenantID, _, err := s.authorizeKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	var wf types.KnowledgeWorkflow
	err = s.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", id, tenantID, kbID).First(&wf).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWorkflowNotFound
	}
	return &wf, err
}

func (s *KnowledgeWorkflowService) getRun(ctx context.Context, kbID, runID string) (*types.KnowledgeWorkflowRun, error) {
	_, tenantID, _, err := s.authorizeKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	var run types.KnowledgeWorkflowRun
	err = s.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", runID, tenantID, kbID).First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWorkflowNotFound
	}
	return &run, err
}

func (s *KnowledgeWorkflowService) loadRunSteps(ctx context.Context, run *types.KnowledgeWorkflowRun) (*types.KnowledgeWorkflowRun, error) {
	if err := s.db.WithContext(ctx).Where("run_id = ? AND tenant_id = ? AND knowledge_base_id = ?", run.ID, run.TenantID, run.KnowledgeBaseID).Order("created_at ASC, id ASC").Find(&run.Steps).Error; err != nil {
		return nil, err
	}
	return run, nil
}

func (s *KnowledgeWorkflowService) authorizeKB(ctx context.Context, kbID string) (*types.KnowledgeBase, uint64, string, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, 0, "", ErrWorkflowForbidden
	}
	actor, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, 0, "", ErrWorkflowForbidden
	}
	if err := s.validateActor(ctx, tenantID, actor); err != nil {
		return nil, 0, "", err
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, 0, "", err
	}
	// Workflows are stored in and execute only against the active tenant's KB.
	if kb == nil || kb.TenantID != tenantID {
		return nil, 0, "", ErrWorkflowForbidden
	}

	return kb, tenantID, actor, nil
}

func (s *KnowledgeWorkflowService) validateActor(ctx context.Context, tenantID uint64, actor string) error {
	var user types.User
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND is_active = TRUE", actor, tenantID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrWorkflowForbidden
		}
		return err
	}
	return nil
}

func (s *KnowledgeWorkflowService) authorizeWorkflow(ctx context.Context, wf *types.KnowledgeWorkflow) error {
	_, tenantID, _, err := s.authorizeKB(ctx, wf.KnowledgeBaseID)
	if err != nil {
		return err
	}
	if tenantID != wf.TenantID {
		return ErrWorkflowForbidden
	}
	return nil
}

func (s *KnowledgeWorkflowService) refreshWorkflowAuthorization(ctx context.Context, wf *types.KnowledgeWorkflow) (context.Context, error) {
	return ctx, s.authorizeWorkflow(ctx, wf)
}

func (s *KnowledgeWorkflowService) authorizeActor(ctx context.Context, tenantID uint64, ownerID string) error {
	actor, ok := types.UserIDFromContext(ctx)
	if !ok {
		return ErrWorkflowForbidden
	}
	return s.validateActor(ctx, tenantID, actor)
}

// normalizeWorkflowScopes writes the current scope without mutating caller-owned configs.
func normalizeWorkflowScopes(steps []types.KnowledgeWorkflowStep) []types.KnowledgeWorkflowStep {
	result := append([]types.KnowledgeWorkflowStep(nil), steps...)
	for i, step := range result {
		if step.Type != types.WorkflowStepFactQuery {
			continue
		}
		scope, ok := step.Config["scope"].(string)
		if !ok {
			continue
		}
		config := make(map[string]any, len(step.Config))
		for key, value := range step.Config {
			config[key] = value
		}
		config["scope"] = types.NormalizeKnowledgeContextScope(scope)
		result[i].Config = config
	}
	return result
}
