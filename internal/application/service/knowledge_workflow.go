package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

const TypeKnowledgeWorkflowRun = "knowledge_workflow:run"

var (
	ErrWorkflowNotFound         = errors.New("knowledge workflow not found")
	ErrWorkflowForbidden        = errors.New("knowledge workflow access denied")
	ErrWorkflowInvalid          = errors.New("invalid knowledge workflow")
	ErrWorkflowSkillUnavailable = errors.New("workflow skill executor is unavailable")
	ErrWorkflowLeaseLost        = errors.New("knowledge workflow run lease lost")
)

// WorkflowSkillConfig is the bounded input passed to the existing tenant-aware
// agent/sandbox execution path. The workflow service never executes skill code
// in the host process.
type WorkflowSkillConfig struct {
	AgentID    string   `json:"agent_id"`
	SkillName  string   `json:"skill_name"`
	ScriptPath string   `json:"script_path"`
	Args       []string `json:"args,omitempty"`
	Input      string   `json:"input,omitempty"`
	InputStep  string   `json:"input_step,omitempty"`
	RunID      string   `json:"-"`
	StepID     string   `json:"-"`
}

type WorkflowSkillResult struct {
	Output     string                 `json:"output"`
	ExitCode   int                    `json:"exit_code"`
	DurationMS int64                  `json:"duration_ms"`
	SessionID  string                 `json:"session_id,omitempty"`
	MessageID  string                 `json:"message_id,omitempty"`
	Artifacts  types.MessageArtifacts `json:"artifacts,omitempty"`
}

// WorkflowSkillExecutor must resolve the requested agent for ownerID and run
// through that agent's tenant sandbox, allowlist, skill source and env guards.
type WorkflowSkillExecutor func(
	ctx context.Context,
	tenantID uint64,
	ownerID string,
	knowledgeBaseID string,
	cfg WorkflowSkillConfig,
) (*WorkflowSkillResult, error)

type KnowledgeWorkflowService struct {
	db        *gorm.DB
	kbs       interfaces.KnowledgeBaseService
	knowledge interfaces.KnowledgeService
	context   *KnowledgeContextService
	webSearch interfaces.WebSearchService
	wiki      interfaces.WikiPageService
	models    interfaces.ModelService
	members   interfaces.TenantMemberRepository
	tasks     interfaces.TaskEnqueuer
	skillExec WorkflowSkillExecutor
	cron      *cron.Cron
	mu        sync.Mutex
}

func NewKnowledgeWorkflowService(
	db *gorm.DB,
	kbs interfaces.KnowledgeBaseService,
	knowledge interfaces.KnowledgeService,
	contextService *KnowledgeContextService,
	webSearch interfaces.WebSearchService,
	wiki interfaces.WikiPageService,
	models interfaces.ModelService,
	members interfaces.TenantMemberRepository,
	tasks interfaces.TaskEnqueuer,
	skillExecutor WorkflowSkillExecutor,
) *KnowledgeWorkflowService {
	return &KnowledgeWorkflowService{
		db: db, kbs: kbs, knowledge: knowledge, context: contextService,
		webSearch: webSearch, wiki: wiki, models: models, members: members,
		tasks: tasks, skillExec: skillExecutor,
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
		if _, err := cron.ParseStandard(in.Schedule.Cron); err != nil {
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
		if step.When != nil {
			prior, ok := seen[step.When.StepID]
			if !ok || prior.Type != types.WorkflowStepCondition {
				return fmt.Errorf("%w: step %q must branch on an earlier condition step", ErrWorkflowInvalid, step.ID)
			}
		}
		if step.Type == types.WorkflowStepSkill {
			if source, _ := step.Config["input_step"].(string); strings.TrimSpace(source) != "" {
				if _, ok := seen[source]; !ok {
					return fmt.Errorf("%w: skill step %q references missing or later input step %q", ErrWorkflowInvalid, step.ID, source)
				}
			}
		}
		if step.Type == types.WorkflowStepWikiUpdate {
			if source, _ := step.Config["source_step"].(string); source != "" {
				if _, ok := seen[source]; !ok {
					return fmt.Errorf("%w: wiki step %q references missing or later source step %q", ErrWorkflowInvalid, step.ID, source)
				}
			}
		}
		if step.Type == types.WorkflowStepCondition {
			if source, _ := step.Config["step_id"].(string); source != "" {
				if _, ok := seen[source]; !ok {
					return fmt.Errorf("%w: condition step %q references missing or later source step %q", ErrWorkflowInvalid, step.ID, source)
				}
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
	case types.WorkflowStepSearch, types.WorkflowStepFactQuery, types.WorkflowStepWebResearch,
		types.WorkflowStepReport, types.WorkflowStepWikiUpdate, types.WorkflowStepSkill,
		types.WorkflowStepCondition, types.WorkflowStepHumanReview:
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
		if (cfg.MinValue != nil || cfg.MaxValue != nil) && str("predicate") == "" {
			return errors.New("numeric comparison requires an attribute predicate")
		}
		if cfg.MinValue != nil && cfg.MaxValue != nil && *cfg.MinValue > *cfg.MaxValue {
			return errors.New("invalid numeric range")
		}
	case types.WorkflowStepWebResearch:
		if str("query") == "" {
			return errors.New("query is required")
		}
		if max := workflowInt(step.Config["max_results"]); max > 30 {
			return errors.New("max_results cannot exceed 30")
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
	case types.WorkflowStepWikiUpdate:
		if str("slug") == "" || str("title") == "" {
			return errors.New("slug and title are required")
		}
		if str("source_step") == "" {
			return errors.New("source_step is required")
		}
		pageType := str("page_type")
		if pageType == "" {
			pageType = types.WikiPageTypeSynthesis
		}
		if !types.IsValidWikiPageType(pageType) {
			return errors.New("unsupported wiki page_type")
		}
	case types.WorkflowStepSkill:
		if str("agent_id") == "" || str("skill_name") == "" || str("script_path") == "" {
			return errors.New("agent_id, skill_name and script_path are required")
		}
	case types.WorkflowStepCondition:
		if str("step_id") == "" || str("path") == "" {
			return errors.New("step_id and path are required")
		}
		operator := str("operator")
		if operator != "exists" && operator != "equals" && operator != "not_equals" && operator != "contains" && operator != "gt" && operator != "gte" {
			return errors.New("unsupported condition operator")
		}
	case types.WorkflowStepHumanReview:
		// A review step needs no additional fields; the persisted step itself is
		// the actionable approval request shown to the user.
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
	_, tenantID, actor, err := s.authorizeKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	role, err := s.currentRole(ctx, tenantID, actor)
	if err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)
	if !role.HasPermission(types.TenantRoleAdmin) && !types.IsSystemAdminFromContext(ctx) {
		query = query.Where("owner_id = ?", actor)
	}
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
	_, tenantID, actor, err := s.authorizeKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)
	role, err := s.currentRole(ctx, tenantID, actor)
	if err != nil {
		return nil, err
	}
	if workflowID != "" {
		wf, err := s.getWorkflow(ctx, kbID, workflowID)
		if err != nil {
			return nil, err
		}
		if err := s.authorizeWorkflow(ctx, wf); err != nil {
			return nil, err
		}
		query = query.Where("workflow_id = ?", workflowID)
	} else if !role.HasPermission(types.TenantRoleAdmin) && !types.IsSystemAdminFromContext(ctx) {
		query = query.Where("owner_id = ?", actor)
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
	_, tenantID, actor, err := s.authorizeKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	role, err := s.currentRole(ctx, tenantID, actor)
	if err != nil {
		return nil, err
	}
	var reports []types.KnowledgeWorkflowReport
	query := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)
	if !role.HasPermission(types.TenantRoleAdmin) && !types.IsSystemAdminFromContext(ctx) {
		query = query.Where("run_id IN (?)", s.db.Model(&types.KnowledgeWorkflowRun{}).
			Select("id").Where("tenant_id = ? AND knowledge_base_id = ? AND owner_id = ?", tenantID, kbID, actor))
	}
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
	role, err := s.currentRole(ctx, tenantID, actor)
	if err != nil {
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
	if !types.IsSystemAdminFromContext(ctx) && !role.HasPermission(types.TenantRoleAdmin) && kb.CreatorID != actor {
		return nil, 0, "", ErrWorkflowForbidden
	}
	return kb, tenantID, actor, nil
}

func (s *KnowledgeWorkflowService) currentRole(ctx context.Context, tenantID uint64, actor string) (types.TenantRole, error) {
	if types.IsSystemAdminFromContext(ctx) {
		// Queued jobs may outlive an administrator grant. Do not trust the
		// original task payload after the account is disabled or demoted.
		var user types.User
		if err := s.db.WithContext(ctx).Select("id", "is_active", "is_system_admin").Where("id = ?", actor).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", ErrWorkflowForbidden
			}
			return "", err
		}
		if !user.IsActive || !user.IsSystemAdmin {
			return "", ErrWorkflowForbidden
		}
		return types.TenantRoleOwner, nil
	}
	if types.IsSyntheticUserID(actor) {
		if actor == fmt.Sprintf("system-%d", tenantID) {
			return types.TenantRoleOwner, nil
		}
		return "", ErrWorkflowForbidden
	}
	if s.members == nil {
		return "", fmt.Errorf("workflow tenant member repository is unavailable")
	}
	member, err := s.members.Get(ctx, actor, tenantID)
	if err != nil {
		return "", err
	}
	if member == nil || member.Status != types.TenantMemberStatusActive {
		return "", ErrWorkflowForbidden
	}
	return member.Role, nil
}

func (s *KnowledgeWorkflowService) authorizeWorkflow(ctx context.Context, wf *types.KnowledgeWorkflow) error {
	_, tenantID, actor, err := s.authorizeKB(ctx, wf.KnowledgeBaseID)
	if err != nil {
		return err
	}
	if tenantID != wf.TenantID {
		return ErrWorkflowForbidden
	}
	role, err := s.currentRole(ctx, tenantID, actor)
	if err != nil {
		return err
	}
	if actor == wf.OwnerID || role.HasPermission(types.TenantRoleAdmin) || types.IsSystemAdminFromContext(ctx) {
		return nil
	}
	return ErrWorkflowForbidden
}

func (s *KnowledgeWorkflowService) refreshWorkflowAuthorization(ctx context.Context, wf *types.KnowledgeWorkflow) (context.Context, error) {
	_, tenantID, actor, err := s.authorizeKB(ctx, wf.KnowledgeBaseID)
	if err != nil {
		return ctx, err
	}
	if tenantID != wf.TenantID {
		return ctx, ErrWorkflowForbidden
	}
	role, err := s.currentRole(ctx, tenantID, actor)
	if err != nil {
		return ctx, err
	}
	ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
	if err := s.authorizeWorkflow(ctx, wf); err != nil {
		return ctx, err
	}
	return ctx, nil
}

func (s *KnowledgeWorkflowService) authorizeActor(ctx context.Context, tenantID uint64, ownerID string) error {
	actor, ok := types.UserIDFromContext(ctx)
	if !ok {
		return ErrWorkflowForbidden
	}
	role, err := s.currentRole(ctx, tenantID, actor)
	if err != nil {
		return err
	}
	if actor == ownerID || role.HasPermission(types.TenantRoleAdmin) || types.IsSystemAdminFromContext(ctx) {
		return nil
	}
	return ErrWorkflowForbidden
}
