package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	chatmodels "reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type workflowTestKBService struct {
	interfaces.KnowledgeBaseService
	kb          *types.KnowledgeBase
	searchCalls int
	searches    []string
}

func (s *workflowTestKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func (s *workflowTestKBService) HybridSearch(_ context.Context, _ string, params types.SearchParams) ([]*types.SearchResult, error) {
	s.searchCalls++
	s.searches = append(s.searches, params.QueryText)
	return []*types.SearchResult{{KnowledgeID: "source-1", KnowledgeTitle: "Source", Content: "approved evidence", Score: 0.9}}, nil
}

type workflowTestKnowledgeService struct{ interfaces.KnowledgeService }

func (workflowTestKnowledgeService) GetKnowledgeBatch(_ context.Context, _ uint64, ids []string) ([]*types.Knowledge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []*types.Knowledge{{ID: "source-1", TenantID: 1, KnowledgeBaseID: "kb", Title: "Source", ParseStatus: types.ParseStatusCompleted}}, nil
}

type workflowTestWebSearch struct {
	interfaces.WebSearchService
	results []*types.WebSearchResult
	calls   int
}

func (s *workflowTestWebSearch) Search(context.Context, string, *types.WebSearchConfig, string) ([]*types.WebSearchResult, error) {
	s.calls++
	return s.results, nil
}

type workflowTestIntakeKnowledge struct {
	workflowTestKnowledgeService
	item        *types.Knowledge
	sourceBody  string
	createCalls int
}

func (s *workflowTestIntakeKnowledge) CreateKnowledgeFromURL(context.Context, string, string, string, string, *bool, string, []string, string, *types.KnowledgeProcessOverrides, ...types.SourceInfo) (*types.Knowledge, error) {
	s.createCalls++
	return s.item, nil
}

func (s *workflowTestIntakeKnowledge) GetKnowledgeBatch(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return []*types.Knowledge{s.item}, nil
}

func (s *workflowTestIntakeKnowledge) GetKnowledgeFile(context.Context, string) (io.ReadCloser, string, error) {
	return io.NopCloser(strings.NewReader(s.sourceBody)), s.item.FileName, nil
}

type workflowTestReportModelService struct {
	interfaces.ModelService
	model chatmodels.Chat
}

func (s workflowTestReportModelService) GetChatModel(context.Context, string) (chatmodels.Chat, error) {
	return s.model, nil
}

type workflowTestReportChat struct {
	messages [][]chatmodels.Message
}

func (s *workflowTestReportChat) Chat(_ context.Context, messages []chatmodels.Message, _ *chatmodels.ChatOptions) (*types.ChatResponse, error) {
	s.messages = append(s.messages, append([]chatmodels.Message(nil), messages...))
	return &types.ChatResponse{Content: "# Evidence report\nThe source supports this finding [C1]."}, nil
}

func (s *workflowTestReportChat) ChatStream(context.Context, []chatmodels.Message, *chatmodels.ChatOptions) (<-chan types.StreamResponse, error) {
	stream := make(chan types.StreamResponse)
	close(stream)
	return stream, nil
}

func (*workflowTestReportChat) GetModelName() string { return "workflow-test" }
func (*workflowTestReportChat) GetModelID() string   { return "workflow-test" }

type workflowTestWikiService struct {
	interfaces.WikiPageService
	page        *types.WikiPage
	createCalls int
	updateCalls int
}

func (s *workflowTestWikiService) GetPageBySlug(context.Context, string, string) (*types.WikiPage, error) {
	return nil, repository.ErrWikiPageNotFound
}

func (s *workflowTestWikiService) CreatePage(_ context.Context, page *types.WikiPage) (*types.WikiPage, error) {
	created := *page
	created.ID, created.Version = "workflow-wiki-page", 1
	s.page = &created
	s.createCalls++
	return &created, nil
}

func (s *workflowTestWikiService) UpdatePage(_ context.Context, page *types.WikiPage) (*types.WikiPage, error) {
	updated := *page
	s.page = &updated
	s.updateCalls++
	return &updated, nil
}

type workflowTestMembers struct {
	interfaces.TenantMemberRepository
	member *types.TenantMember
}

func (s workflowTestMembers) Get(_ context.Context, userID string, tenantID uint64) (*types.TenantMember, error) {
	if s.member == nil || s.member.UserID != userID || s.member.TenantID != tenantID {
		return nil, nil
	}
	return s.member, nil
}

type workflowRecordedTask struct {
	task    *asynq.Task
	payload types.KnowledgeWorkflowTaskPayload
}

type workflowTestTaskQueue struct {
	interfaces.TaskEnqueuer
	tasks []workflowRecordedTask
}

func (s *workflowTestTaskQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	var payload types.KnowledgeWorkflowTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return nil, err
	}
	s.tasks = append(s.tasks, workflowRecordedTask{task: task, payload: payload})
	return &asynq.TaskInfo{ID: task.Type()}, nil
}

func workflowTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.KnowledgeBase{}, &types.Knowledge{}, &types.Chunk{}, &types.KnowledgeEvidence{}, &types.KnowledgeWorkflow{}, &types.KnowledgeWorkflowRun{}, &types.KnowledgeWorkflowStepRun{}, &types.KnowledgeWorkflowReport{}))
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "tenant"}).Error)
	return db
}

func workflowTestContext(user string, tenantID uint64) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
	return context.WithValue(ctx, types.UserIDContextKey, user)
}

func TestKnowledgeWorkflowPausesForReviewAndResumesWithoutRepeatingSuccess(t *testing.T) {
	db := workflowTestDB(t)
	kb := &workflowTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, CreatorID: "owner"}}
	require.NoError(t, db.Create(kb.kb).Error)
	require.NoError(t, db.Create(&types.Knowledge{ID: "source-1", TenantID: 1, KnowledgeBaseID: "kb", Title: "Source", ParseStatus: types.ParseStatusCompleted, Metadata: types.JSON(`{"intake":{"decision":"approved","source_info":{"original_url":"https://example.com/source"}}}`)}).Error)
	require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: "source-evidence", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "source-1", Version: 1, BodyHash: strings.Repeat("a", 64), Title: "Source", Body: "An approved evidence excerpt from the source.", OriginalURL: "https://example.com/source", Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: types.KnowledgeExtractionPending, CreatedAt: time.Now()}).Error)
	queue := &workflowTestTaskQueue{}
	svc := NewKnowledgeWorkflowService(db, kb, workflowTestKnowledgeService{}, NewKnowledgeContextService(db, nil), nil, nil, nil,
		workflowTestMembers{member: &types.TenantMember{UserID: "owner", TenantID: 1, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive}}, queue, nil)
	ctx := workflowTestContext("owner", 1)
	wf, err := svc.Create(ctx, "kb", types.KnowledgeWorkflowInput{
		Name: "Weekly report", Enabled: true,
		Steps: []types.KnowledgeWorkflowStep{
			{ID: "search", Type: types.WorkflowStepSearch, Config: map[string]any{"query": "source evidence"}},
			{ID: "review", Type: types.WorkflowStepHumanReview, DependsOn: []string{"search"}},
		},
	})
	require.NoError(t, err)
	run, err := svc.Run(ctx, "kb", wf.ID, "manual")
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusQueued, run.Status)
	require.Len(t, queue.tasks, 1)
	_, err = svc.Update(ctx, "kb", wf.ID, types.KnowledgeWorkflowInput{
		Name: "Weekly report v2", Enabled: true,
		Steps: []types.KnowledgeWorkflowStep{
			{ID: "search", Type: types.WorkflowStepSearch, Config: map[string]any{"query": "changed definition"}},
			{ID: "review", Type: types.WorkflowStepHumanReview, DependsOn: []string{"search"}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, svc.Handle(ctx, queue.tasks[0].task))
	require.Equal(t, []string{"source evidence"}, kb.searches)
	run, err = svc.GetRun(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusPaused, run.Status)
	require.Len(t, run.Steps, 2)
	require.Equal(t, types.WorkflowStepSucceeded, run.Steps[0].Status)
	require.Equal(t, types.WorkflowStepWaitingReview, run.Steps[1].Status)
	var searchOutput workflowStepOutput
	require.NoError(t, json.Unmarshal(run.Steps[0].Output, &searchOutput))
	require.Len(t, searchOutput.Sources, 1)
	require.Equal(t, "source-evidence", searchOutput.Sources[0].EvidenceID)
	require.Equal(t, "approved evidence", searchOutput.Sources[0].Quote)
	// A task retry while the review is pending must not repeat the completed search.
	require.NoError(t, svc.Handle(ctx, queue.tasks[0].task))
	require.Equal(t, 1, kb.searchCalls)
	run, err = svc.Approve(ctx, "kb", run.ID, "approve")
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusQueued, run.Status)
	require.Len(t, queue.tasks, 2)
	require.NoError(t, svc.Handle(ctx, queue.tasks[1].task))
	run, err = svc.GetRun(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusCompleted, run.Status)
	require.Equal(t, 1, kb.searchCalls)
}

func TestKnowledgeWorkflowWaitsForWebIntakeDecisionWithoutDuplicateIngest(t *testing.T) {
	db := workflowTestDB(t)
	kb := &workflowTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, CreatorID: "owner", SummaryModelID: "summary-model"}}
	require.NoError(t, db.Create(kb.kb).Error)
	queue := &workflowTestTaskQueue{}
	item := &types.Knowledge{
		ID: "web-source", TenantID: 1, KnowledgeBaseID: "kb", Title: "Web source", ParseStatus: types.ParseStatusPending,
		Metadata: types.JSON(`{"intake":{"decision":"pending"}}`),
	}
	require.NoError(t, db.Create(item).Error)
	const rawSource = "The 2025 report records revenue of $10 million. This text comes from the approved original document."
	const searchSnippet = "SEARCH SUMMARY ONLY; not present in the original source"
	knowledge := &workflowTestIntakeKnowledge{item: item, sourceBody: rawSource}
	webSearch := &workflowTestWebSearch{results: []*types.WebSearchResult{{
		Title: "Undated source", URL: "https://example.com/report", Snippet: searchSnippet,
	}}}
	reportModel := &workflowTestReportChat{}
	wiki := &workflowTestWikiService{}
	member := &types.TenantMember{UserID: "owner", TenantID: 1, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive}
	var run *types.KnowledgeWorkflowRun
	skillCalls := 0
	skill := func(_ context.Context, tenantID uint64, ownerID, kbID string, cfg WorkflowSkillConfig) (*WorkflowSkillResult, error) {
		skillCalls++
		require.Equal(t, uint64(1), tenantID)
		require.Equal(t, "owner", ownerID)
		require.Equal(t, "kb", kbID)
		require.Equal(t, run.ID, cfg.RunID)
		require.Equal(t, "skill", cfg.StepID)
		require.Equal(t, "report", cfg.InputStep)
		require.Contains(t, cfg.Input, "# Evidence report")
		if skillCalls == 1 {
			return nil, errors.New("temporary sandbox failure")
		}
		return &WorkflowSkillResult{Output: "skill complete", ExitCode: 0}, nil
	}
	svc := NewKnowledgeWorkflowService(db, kb, knowledge, NewKnowledgeContextService(db, nil), webSearch, wiki,
		workflowTestReportModelService{model: reportModel}, workflowTestMembers{member: member}, queue, skill)
	ctx := workflowTestContext("owner", 1)
	wf, err := svc.Create(ctx, "kb", types.KnowledgeWorkflowInput{
		Name: "Research", Enabled: true,
		Steps: []types.KnowledgeWorkflowStep{
			{ID: "research", Type: types.WorkflowStepWebResearch, Config: map[string]any{"query": "market report"}},
			{ID: "report", Type: types.WorkflowStepReport, DependsOn: []string{"research"}, Config: map[string]any{"title": "Market report", "prompt": "Summarize the source."}},
			{ID: "review", Type: types.WorkflowStepHumanReview, DependsOn: []string{"report"}},
			{ID: "wiki", Type: types.WorkflowStepWikiUpdate, DependsOn: []string{"review"}, Config: map[string]any{"slug": "market-report", "title": "Market report", "source_step": "report"}},
			{ID: "skill", Type: types.WorkflowStepSkill, DependsOn: []string{"wiki"}, Config: map[string]any{"agent_id": "agent-1", "skill_name": "source-check", "script_path": "check.py", "input_step": "report"}},
		},
	})
	require.NoError(t, err)
	run, err = svc.Run(ctx, "kb", wf.ID, "manual")
	require.NoError(t, err)
	require.NoError(t, svc.Handle(ctx, queue.tasks[0].task))
	run, err = svc.GetRun(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusWaitingIngestion, run.Status)
	require.Equal(t, types.WorkflowStepWaitingIngestion, run.Steps[0].Status)
	require.Equal(t, 1, webSearch.calls)
	require.Equal(t, 1, knowledge.createCalls)

	item.ParseStatus = types.ParseStatusCompleted
	item.Metadata = types.JSON(`{"intake":{"decision":"approved","source_info":{"original_url":"https://example.com/report"}}}`)
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", item.ID).Updates(map[string]any{"parse_status": item.ParseStatus, "metadata": item.Metadata}).Error)
	require.NoError(t, db.Model(&types.KnowledgeWorkflowRun{}).Where("id = ?", run.ID).Updates(map[string]any{"next_run_at": time.Now().Add(-time.Second)}).Error)
	require.NoError(t, svc.Handle(ctx, queue.tasks[1].task))
	run, err = svc.GetRun(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusPaused, run.Status)
	require.Equal(t, 1, webSearch.calls)
	require.Equal(t, 1, knowledge.createCalls)
	require.Equal(t, types.WorkflowStepWaitingReview, run.Steps[2].Status)
	var sourceSnapshot types.KnowledgeEvidence
	require.NoError(t, db.Where("knowledge_id = ? AND status = ?", item.ID, types.KnowledgeEvidenceCurrent).First(&sourceSnapshot).Error)
	require.Equal(t, types.KnowledgeExtractionPending, sourceSnapshot.ExtractionStatus)

	var report types.KnowledgeWorkflowReport
	require.NoError(t, db.Where("run_id = ? AND step_id = ?", run.ID, "report").First(&report).Error)
	require.Len(t, report.Citations, 1)
	require.Equal(t, "evidence", report.Citations[0].Kind)
	require.NotEmpty(t, report.Citations[0].EvidenceID)
	require.Equal(t, rawSource, report.Citations[0].Quote)
	require.NotContains(t, report.Citations[0].Quote, searchSnippet)
	require.NotEmpty(t, reportModel.messages)
	require.Contains(t, reportModel.messages[0][1].Content, rawSource)
	require.NotContains(t, reportModel.messages[0][1].Content, searchSnippet)
	require.Equal(t, 0, wiki.createCalls)

	_, err = svc.Approve(ctx, "kb", run.ID, "approve")
	require.NoError(t, err)
	require.Len(t, queue.tasks, 3)
	err = svc.Handle(ctx, queue.tasks[2].task)
	require.ErrorContains(t, err, "temporary sandbox failure")
	run, err = svc.GetRun(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusFailed, run.Status)
	require.Equal(t, 1, skillCalls)
	require.Equal(t, 1, wiki.createCalls)
	require.Contains(t, wiki.page.Content, "[C1]")

	queuedBeforeRetry := len(queue.tasks)
	member.Status = types.TenantMemberStatusSuspended
	_, err = svc.Retry(ctx, "kb", run.ID)
	require.ErrorIs(t, err, ErrWorkflowForbidden)
	require.Len(t, queue.tasks, queuedBeforeRetry)
	member.Status = types.TenantMemberStatusActive
	_, err = svc.Retry(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Len(t, queue.tasks, queuedBeforeRetry+1)
	require.NoError(t, svc.Handle(ctx, queue.tasks[3].task))
	run, err = svc.GetRun(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusCompleted, run.Status)
	require.Equal(t, 2, skillCalls)
	require.Equal(t, 1, wiki.createCalls)
	var reportCount int64
	require.NoError(t, db.Model(&types.KnowledgeWorkflowReport{}).Where("run_id = ?", run.ID).Count(&reportCount).Error)
	require.EqualValues(t, 1, reportCount)

	admin := &types.User{ID: "admin", Username: "workflow-admin", Email: "workflow-admin@example.com", PasswordHash: "test", TenantID: 1, IsActive: true, IsSystemAdmin: true}
	require.NoError(t, db.Create(admin).Error)
	adminCtx := context.WithValue(workflowTestContext("admin", 1), types.SystemAdminContextKey, true)
	role, err := svc.currentRole(adminCtx, 1, "admin")
	require.NoError(t, err)
	require.Equal(t, types.TenantRoleOwner, role)
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", admin.ID).Update("is_system_admin", false).Error)
	_, err = svc.currentRole(adminCtx, 1, "admin")
	require.ErrorIs(t, err, ErrWorkflowForbidden)
}

func TestKnowledgeWorkflowLeaseFencesDuplicateAndStaleWorker(t *testing.T) {
	db := workflowTestDB(t)
	svc := NewKnowledgeWorkflowService(db, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	run := &types.KnowledgeWorkflowRun{
		ID: "run-lease", TenantID: 1, KnowledgeBaseID: "kb", WorkflowID: "wf", OwnerID: "owner",
		Trigger: "manual", IdempotencyKey: "lease:test", Status: types.WorkflowStatusQueued,
		WorkflowSnapshot: []types.KnowledgeWorkflowStep{{ID: "search", Type: types.WorkflowStepSearch}},
	}
	require.NoError(t, db.Create(run).Error)
	claimed, err := svc.claimRun(context.Background(), run)
	require.NoError(t, err)
	require.True(t, claimed)
	staleToken := run.LeaseToken

	duplicate := &types.KnowledgeWorkflowRun{ID: run.ID}
	claimed, err = svc.claimRun(context.Background(), duplicate)
	require.NoError(t, err)
	require.False(t, claimed)

	require.NoError(t, db.Model(&types.KnowledgeWorkflowRun{}).Where("id = ?", run.ID).Update("updated_at", time.Now().Add(-3*time.Minute)).Error)
	recovered := &types.KnowledgeWorkflowRun{ID: run.ID}
	claimed, err = svc.claimRun(context.Background(), recovered)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEqual(t, staleToken, recovered.LeaseToken)
	step := &types.KnowledgeWorkflowStepRun{ID: "step-lease", TenantID: 1, KnowledgeBaseID: "kb", RunID: run.ID, StepID: "search", StepType: types.WorkflowStepSearch, Status: types.WorkflowStepRunning}
	require.NoError(t, db.Create(step).Error)
	run.LeaseToken = staleToken
	err = svc.completeStep(context.Background(), run, step, types.WorkflowStepSucceeded, workflowStepOutput{Text: "stale"}, nil)
	require.ErrorIs(t, err, ErrWorkflowLeaseLost)
}

func TestWorkflowReportRangeUsesTimezoneAndCalendarBoundaries(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, loc)
	from, to, gotLocation, err := workflowReportRange(workflowReportConfig{Window: "previous_day", Timezone: "Asia/Tokyo"}, now)
	require.NoError(t, err)
	require.Equal(t, loc, gotLocation)
	require.Equal(t, time.Date(2026, 9, 30, 0, 0, 0, 0, loc), *from)
	require.Equal(t, time.Date(2026, 9, 30, 23, 59, 59, 999999999, loc), *to)
}

func TestKnowledgeWorkflowRejectsOtherTenantAndUnsupportedStep(t *testing.T) {
	db := workflowTestDB(t)
	kb := &workflowTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, CreatorID: "owner"}}
	svc := NewKnowledgeWorkflowService(db, kb, workflowTestKnowledgeService{}, nil, nil, nil, nil,
		workflowTestMembers{member: &types.TenantMember{UserID: "owner", TenantID: 1, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive}}, &workflowTestTaskQueue{}, nil)
	_, err := svc.Create(workflowTestContext("owner", 2), "kb", types.KnowledgeWorkflowInput{
		Name: "Cross tenant", Steps: []types.KnowledgeWorkflowStep{{ID: "step", Type: types.WorkflowStepSearch, Config: map[string]any{"query": "x"}}},
	})
	require.ErrorIs(t, err, ErrWorkflowForbidden)
	err = validateWorkflowInput(types.KnowledgeWorkflowInput{
		Name: "Unsupported", Steps: []types.KnowledgeWorkflowStep{{ID: "step", Type: "external_publish", Config: map[string]any{}}},
	})
	require.ErrorIs(t, err, ErrWorkflowInvalid)
}
