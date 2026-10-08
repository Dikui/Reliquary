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
	"gorm.io/gorm"

	chatmodels "reliquary/internal/models/chat"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type workflowTestKBService struct {
	interfaces.KnowledgeBaseService
	kb            *types.KnowledgeBase
	searchCalls   int
	searches      []string
	searchContent string
	afterSearch   func()
	searchErrors  map[string]error
}

func (s *workflowTestKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func (s *workflowTestKBService) HybridSearch(_ context.Context, _ string, params types.SearchParams) ([]*types.SearchResult, error) {
	s.searchCalls++
	s.searches = append(s.searches, params.QueryText)
	if err := s.searchErrors[params.QueryText]; err != nil {
		delete(s.searchErrors, params.QueryText)
		return nil, err
	}
	if s.afterSearch != nil {
		s.afterSearch()
	}
	content := s.searchContent
	if content == "" {
		content = "approved evidence"
	}
	return []*types.SearchResult{{KnowledgeID: "source-1", KnowledgeTitle: "Source", Content: content, Score: 0.9}}, nil
}

type workflowTestKnowledgeService struct{ interfaces.KnowledgeService }

func (workflowTestKnowledgeService) GetKnowledgeBatch(_ context.Context, _ uint64, ids []string) ([]*types.Knowledge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return []*types.Knowledge{{ID: "source-1", TenantID: 1, KnowledgeBaseID: "kb", Title: "Source", ParseStatus: types.ParseStatusCompleted}}, nil
}

type workflowTestIntakeKnowledge struct {
	workflowTestKnowledgeService
	item       *types.Knowledge
	sourceBody string
}

func (s *workflowTestIntakeKnowledge) GetKnowledgeBatch(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return []*types.Knowledge{s.item}, nil
}

func (s *workflowTestIntakeKnowledge) GetKnowledgeBody(context.Context, string) (io.ReadCloser, string, error) {
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
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.KnowledgeDomain{}, &types.KnowledgeBase{}, &types.Knowledge{}, &types.Chunk{}, &types.KnowledgeEvidence{}, &types.KnowledgeWorkflow{}, &types.KnowledgeWorkflowRun{}, &types.KnowledgeWorkflowStepRun{}, &types.KnowledgeWorkflowReport{}))
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "tenant"}).Error)
	require.NoError(t, db.Create(&types.User{ID: "owner", Username: "owner", Email: "owner@example.com", TenantID: 1, IsActive: true}).Error)
	domain := BuiltinAIKnowledgeDomain()
	domain.ID = "ai"
	domain.TenantID = 1
	require.NoError(t, db.Create(domain).Error)
	return db
}

func workflowTestContext(user string, tenantID uint64) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
	return context.WithValue(ctx, types.UserIDContextKey, user)
}

func TestKnowledgeWorkflowDisabledRunResumesSnapshotWithoutRepeatingSuccess(t *testing.T) {
	db := workflowTestDB(t)
	kb := &workflowTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, DomainID: "ai", CreatorID: "owner", SummaryModelID: "summary-model"}}
	require.NoError(t, db.Create(kb.kb).Error)
	require.NoError(t, db.Create(&types.Knowledge{ID: "source-1", TenantID: 1, KnowledgeBaseID: "kb", Title: "Source", ParseStatus: types.ParseStatusCompleted, Metadata: types.JSON(`{"intake":{"decision":"approved","source_info":{"original_url":"https://example.com/source"}}}`)}).Error)
	require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: "source-evidence", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "source-1", Version: 1, BodyHash: strings.Repeat("a", 64), Title: "Source", Body: "An approved evidence excerpt from the source.", OriginalURL: "https://example.com/source", Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: types.KnowledgeExtractionPending, CreatedAt: time.Now()}).Error)
	queue := &workflowTestTaskQueue{}
	svc := NewKnowledgeWorkflowService(db, kb, workflowTestKnowledgeService{}, NewKnowledgeContextService(db, nil), workflowTestReportModelService{model: &workflowTestReportChat{}}, queue)
	ctx := workflowTestContext("owner", 1)
	wf, err := svc.Create(ctx, "kb", types.KnowledgeWorkflowInput{
		Name: "Weekly report", Enabled: true,
		Steps: []types.KnowledgeWorkflowStep{
			{ID: "search", Type: types.WorkflowStepSearch, Config: map[string]any{"query": "source evidence"}},
			{ID: "report", Type: types.WorkflowStepReport, DependsOn: []string{"search"}, Config: map[string]any{"title": "Report"}},
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
			{ID: "report", Type: types.WorkflowStepReport, DependsOn: []string{"search"}, Config: map[string]any{"title": "Report"}},
		},
	})
	require.NoError(t, err)
	kb.afterSearch = func() {
		require.NoError(t, db.Model(&types.KnowledgeWorkflow{}).Where("id = ?", wf.ID).Update("enabled", false).Error)
	}
	require.NoError(t, svc.Handle(ctx, queue.tasks[0].task))
	require.Equal(t, []string{"source evidence"}, kb.searches)
	run, err = svc.GetRun(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusPaused, run.Status)
	require.Len(t, run.Steps, 2)
	require.Equal(t, types.WorkflowStepSucceeded, run.Steps[0].Status)
	require.Equal(t, types.WorkflowStepPending, run.Steps[1].Status)
	var searchOutput workflowStepOutput
	require.NoError(t, json.Unmarshal(run.Steps[0].Output, &searchOutput))
	require.Len(t, searchOutput.Sources, 1)
	require.Equal(t, "source-evidence", searchOutput.Sources[0].EvidenceID)
	require.Equal(t, "approved evidence", searchOutput.Sources[0].Quote)
	// A duplicate task while disabled must not repeat the completed search.
	require.NoError(t, svc.Handle(ctx, queue.tasks[0].task))
	require.Equal(t, 1, kb.searchCalls)
	_, err = svc.Resume(ctx, "kb", run.ID)
	require.ErrorContains(t, err, "enable the workflow")
	require.NoError(t, db.Model(&types.KnowledgeWorkflow{}).Where("id = ?", wf.ID).Update("enabled", true).Error)
	kb.afterSearch = nil
	run, err = svc.Resume(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusQueued, run.Status)
	require.Len(t, queue.tasks, 2)
	require.NoError(t, svc.Handle(ctx, queue.tasks[1].task))
	run, err = svc.GetRun(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusCompleted, run.Status)
	require.Equal(t, 1, kb.searchCalls)
}

func TestKnowledgeWorkflowReportRetriesPreserveSourceEvidence(t *testing.T) {
	db := workflowTestDB(t)
	kb := &workflowTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, DomainID: "ai", CreatorID: "owner", SummaryModelID: "summary-model"}}
	require.NoError(t, db.Create(kb.kb).Error)
	queue := &workflowTestTaskQueue{}
	item := &types.Knowledge{
		ID: "source-1", TenantID: 1, KnowledgeBaseID: "kb", Title: "Source", ParseStatus: types.ParseStatusCompleted,
		Metadata: types.JSON(`{"intake":{"decision":"approved","source_info":{"original_url":"https://example.com/report"}}}`),
	}
	require.NoError(t, db.Create(item).Error)
	const rawSource = "The 2025 report records revenue of $10 million. This text comes from the approved original document."
	const searchSnippet = "SEARCH SUMMARY ONLY; not present in the original source"
	kb.searchContent = searchSnippet
	knowledge := &workflowTestIntakeKnowledge{item: item, sourceBody: rawSource}
	reportModel := &workflowTestReportChat{}
	kb.searchErrors = map[string]error{"verify source": errors.New("temporary search failure")}
	svc := NewKnowledgeWorkflowService(db, kb, knowledge, NewKnowledgeContextService(db, nil),
		workflowTestReportModelService{model: reportModel}, queue)
	ctx := workflowTestContext("owner", 1)
	wf, err := svc.Create(ctx, "kb", types.KnowledgeWorkflowInput{
		Name: "Evidence report", Enabled: true,
		Steps: []types.KnowledgeWorkflowStep{
			{ID: "search", Type: types.WorkflowStepSearch, Config: map[string]any{"query": "market report"}},
			{ID: "report", Type: types.WorkflowStepReport, DependsOn: []string{"search"}, Config: map[string]any{"title": "Market report", "prompt": "Summarize the source."}},
			{ID: "verify", Type: types.WorkflowStepSearch, DependsOn: []string{"report"}, Config: map[string]any{"query": "verify source"}},
		},
	})
	require.NoError(t, err)
	run, err := svc.Run(ctx, "kb", wf.ID, "manual")
	require.NoError(t, err)
	require.ErrorContains(t, svc.Handle(ctx, queue.tasks[0].task), "temporary search failure")
	run, err = svc.GetRun(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusFailed, run.Status)
	require.Equal(t, 2, kb.searchCalls)
	require.Equal(t, types.WorkflowStepSucceeded, run.Steps[1].Status)
	require.Equal(t, types.WorkflowStepFailed, run.Steps[2].Status)
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
	queuedBeforeRetry := len(queue.tasks)
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "owner").Update("is_active", false).Error)
	_, err = svc.Retry(ctx, "kb", run.ID)
	require.ErrorIs(t, err, ErrWorkflowForbidden)
	require.Len(t, queue.tasks, queuedBeforeRetry)
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "owner").Update("is_active", true).Error)
	_, err = svc.Retry(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Len(t, queue.tasks, queuedBeforeRetry+1)
	require.NoError(t, svc.Handle(ctx, queue.tasks[1].task))
	run, err = svc.GetRun(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusCompleted, run.Status)
	require.Equal(t, 3, kb.searchCalls)
	require.Equal(t, []string{"market report", "verify source", "verify source"}, kb.searches)
	require.Len(t, reportModel.messages, 1)
	var reportCount int64
	require.NoError(t, db.Model(&types.KnowledgeWorkflowReport{}).Where("run_id = ?", run.ID).Count(&reportCount).Error)
	require.EqualValues(t, 1, reportCount)

	admin := &types.User{ID: "admin", Username: "workflow-admin", Email: "workflow-admin@example.com", PasswordHash: "test", TenantID: 1, IsActive: true}
	require.NoError(t, db.Create(admin).Error)
	adminCtx := workflowTestContext("admin", 1)
	err = svc.validateActor(adminCtx, 1, "admin")
	require.NoError(t, err)

	require.NoError(t, db.Model(&types.User{}).Where("id = ?", admin.ID).Update("is_active", false).Error)
	err = svc.validateActor(adminCtx, 1, "admin")
	require.ErrorIs(t, err, ErrWorkflowForbidden)
}

func TestKnowledgeWorkflowLeaseFencesDuplicateAndStaleWorker(t *testing.T) {
	db := workflowTestDB(t)
	svc := NewKnowledgeWorkflowService(db, nil, nil, nil, nil, nil)
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
	kb := &workflowTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, DomainID: "ai", CreatorID: "owner"}}
	svc := NewKnowledgeWorkflowService(db, kb, workflowTestKnowledgeService{}, nil, nil, &workflowTestTaskQueue{})
	_, err := svc.Create(workflowTestContext("owner", 2), "kb", types.KnowledgeWorkflowInput{
		Name: "Cross tenant", Steps: []types.KnowledgeWorkflowStep{{ID: "step", Type: types.WorkflowStepSearch, Config: map[string]any{"query": "x"}}},
	})
	require.ErrorIs(t, err, ErrWorkflowForbidden)
	err = validateWorkflowInput(types.KnowledgeWorkflowInput{
		Name: "Unsupported", Steps: []types.KnowledgeWorkflowStep{{ID: "step", Type: "external_publish", Config: map[string]any{}}},
	})
	require.ErrorIs(t, err, ErrWorkflowInvalid)

	input := types.KnowledgeWorkflowInput{
		Name: "Scheduled search", Schedule: types.KnowledgeWorkflowSchedule{Cron: "0 9 * * *"},
		Steps: []types.KnowledgeWorkflowStep{{ID: "search", Type: types.WorkflowStepSearch, Config: map[string]any{"query": "source"}}},
	}
	require.NoError(t, validateWorkflowInput(input))
	input.Schedule.Cron = "@daily"
	require.ErrorIs(t, validateWorkflowInput(input), ErrWorkflowInvalid, "the schedule must use the same five fields as the scheduler")

}

func TestKnowledgeWorkflowCreateRejectsRemovedSteps(t *testing.T) {
	db := workflowTestDB(t)
	kb := &workflowTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, DomainID: "ai", CreatorID: "owner"}}
	svc := NewKnowledgeWorkflowService(db, kb, workflowTestKnowledgeService{}, nil, nil, &workflowTestTaskQueue{})
	for _, stepType := range []string{"web_research", "condition", "skill", "wiki_update", "human_review"} {
		t.Run(stepType, func(t *testing.T) {
			_, err := svc.Create(workflowTestContext("owner", 1), "kb", types.KnowledgeWorkflowInput{
				Name: "Removed step", Steps: []types.KnowledgeWorkflowStep{{ID: "removed", Type: stepType, Config: map[string]any{}}},
			})
			require.ErrorIs(t, err, ErrWorkflowInvalid)
			require.ErrorContains(t, err, `unsupported step type "`+stepType+`"`)
		})
	}
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeWorkflow{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestKnowledgeWorkflowDailyAndWeeklyTemplatesComplete(t *testing.T) {
	for _, template := range []struct{ name, sourceType, cron, window string }{
		{"daily", types.WorkflowStepSearch, "0 8 * * *", "previous_day"},
		{"weekly", types.WorkflowStepFactQuery, "0 9 * * 1", "previous_week"},
	} {
		t.Run(template.name, func(t *testing.T) {
			db := pgtest.Open(t)
			require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "tenant"}).Error)
			require.NoError(t, db.Create(&types.User{ID: "owner", Username: "owner", Email: "owner@example.com", TenantID: 1, IsActive: true}).Error)
			domain := BuiltinAIKnowledgeDomain()
			domain.ID = "ai"
			domain.TenantID = 1
			require.NoError(t, db.Create(domain).Error)
			kb := &workflowTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, DomainID: "ai", CreatorID: "owner", SummaryModelID: "summary-model"}}
			require.NoError(t, db.Create(kb.kb).Error)
			from, _, _, err := workflowReportRange(workflowReportConfig{Window: template.window, Timezone: "Asia/Shanghai"}, time.Now())
			require.NoError(t, err)
			publishedAt := from.Add(12 * time.Hour).Format(time.RFC3339)
			require.NoError(t, db.Create(&types.Knowledge{ID: "source-1", TenantID: 1, KnowledgeBaseID: "kb", Title: "Source", ParseStatus: types.ParseStatusCompleted}).Error)
			require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: "source-evidence", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "source-1", Version: 1, BodyHash: strings.Repeat("a", 64), Title: "Source", Body: "approved evidence", OriginalURL: "https://example.com/source", PublishedAt: publishedAt, Status: types.KnowledgeEvidenceCurrent}).Error)
			require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "tracked", TenantID: 1, CanonicalName: "Tracked object", NormalizedName: "tracked object", EntityType: "company"}).Error)
			require.NoError(t, db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: "source-evidence", EntityID: "tracked"}).Error)
			require.NoError(t, db.Create(&types.KnowledgeFact{ID: "fact", TenantID: 1, KnowledgeBaseID: "kb", SubjectEntityID: "tracked", Predicate: "finding", EvidenceID: "source-evidence", Quote: "approved evidence", Status: types.KnowledgeFactCurrent}).Error)
			queue := &workflowTestTaskQueue{}
			svc := NewKnowledgeWorkflowService(db, kb, workflowTestKnowledgeService{}, NewKnowledgeContextService(db, nil), workflowTestReportModelService{model: &workflowTestReportChat{}}, queue)
			ctx := workflowTestContext("owner", 1)
			wf, err := svc.Create(ctx, "kb", types.KnowledgeWorkflowInput{
				Name: template.name, Enabled: true, Schedule: types.KnowledgeWorkflowSchedule{Cron: template.cron, Timezone: "Asia/Shanghai"},
				Steps: []types.KnowledgeWorkflowStep{
					{ID: "source", Type: template.sourceType, Config: map[string]any{"query": "daily messages", "entity": "Tracked object", "include_history": true}},
					{ID: "report", Type: types.WorkflowStepReport, DependsOn: []string{"source"}, Config: map[string]any{"title": template.name, "window": template.window, "timezone": "Asia/Shanghai"}},
				},
			})
			require.NoError(t, err)
			run, err := svc.Run(ctx, "kb", wf.ID, "manual")
			require.NoError(t, err)
			require.NoError(t, svc.Handle(ctx, queue.tasks[0].task))
			run, err = svc.GetRun(ctx, "kb", run.ID)
			require.NoError(t, err)
			require.Equal(t, types.WorkflowStatusCompleted, run.Status)
			require.Len(t, run.Steps, 2)
			for _, step := range run.Steps {
				require.Equal(t, types.WorkflowStepSucceeded, step.Status)
			}
			var report types.KnowledgeWorkflowReport
			require.NoError(t, db.Where("run_id = ?", run.ID).First(&report).Error)
			require.NotEmpty(t, report.Markdown)
			require.NotNil(t, report.TimeFrom)
			require.NotNil(t, report.TimeTo)
		})
	}
}
