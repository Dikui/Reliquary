package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/agent/tools"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func TestKnowledgeContextAllScopeAcceptsLegacyTeamAlias(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "acme", TenantID: 9, CanonicalName: "Acme", NormalizedName: "acme"}).Error)
	for i, kbID := range []string{"kb", "energy-one", "energy-two"} {
		id := fmt.Sprintf("alias-source-%d", i)
		p2ContextMessage(t, db, id, kbID, "Acme has ten workers.")
		require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: id, TenantID: 9, KnowledgeBaseID: kbID, KnowledgeID: id, Version: 1, BodyHash: strings.Repeat(fmt.Sprint(i), 64), Body: "Acme has ten workers.", Status: types.KnowledgeEvidenceCurrent}).Error)
		require.NoError(t, db.Create(&types.KnowledgeFact{ID: id, TenantID: 9, KnowledgeBaseID: kbID, SubjectEntityID: "acme", Kind: types.KnowledgeFactAttribute, Predicate: "workers", Value: types.JSON(`10`), ValueType: "number", EvidenceID: id, Quote: "Acme has ten workers.", Status: types.KnowledgeFactCurrent}).Error)
	}
	all, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{Scope: "all", EntityID: "acme"})
	require.NoError(t, err)
	require.Len(t, all.Facts, 3)
	legacy, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{Scope: "team", EntityID: "acme"})
	require.NoError(t, err)
	require.Equal(t, all, legacy)
}

func TestKnowledgeWorkflowWritesAllAndRunsLegacyTeamSnapshot(t *testing.T) {
	db := workflowTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeEntity{}, &types.KnowledgeEntityAlias{}, &types.KnowledgeEvidenceEntity{}, &types.KnowledgeFact{}, &types.KnowledgeEvent{}, &types.KnowledgeEventEvidence{}, &types.KnowledgeEventEntity{}, &types.KnowledgeEventEvidenceEntity{}, &types.KnowledgeAttention{}))
	kb := &workflowTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, DomainID: "ai"}}
	kb.kb.EnsureDefaults()
	require.NoError(t, db.Create(kb.kb).Error)
	queue := &workflowTestTaskQueue{}
	svc := NewKnowledgeWorkflowService(db, kb, nil, NewKnowledgeContextService(db, nil), nil, queue)
	ctx := workflowTestContext("owner", 1)
	input := types.KnowledgeWorkflowInput{Name: "Scope compatibility", Enabled: true, Steps: []types.KnowledgeWorkflowStep{{ID: "facts", Type: types.WorkflowStepFactQuery, Config: map[string]any{"scope": "team", "predicate": "workers"}}}}
	wf, err := svc.Create(ctx, "kb", input)
	require.NoError(t, err)
	require.Equal(t, "team", input.Steps[0].Config["scope"], "normalization must not mutate caller-owned maps")
	var saved types.KnowledgeWorkflow
	require.NoError(t, db.First(&saved, "id = ?", wf.ID).Error)
	require.Equal(t, "all", saved.Steps[0].Config["scope"])
	wf, err = svc.Update(ctx, "kb", wf.ID, input)
	require.NoError(t, err)
	require.NoError(t, db.First(&saved, "id = ?", wf.ID).Error)
	require.Equal(t, "all", saved.Steps[0].Config["scope"])

	// Emulate both an old saved definition and an already queued old run.
	saved.Steps[0].Config["scope"] = "team"
	require.NoError(t, db.Save(&saved).Error)
	run, err := svc.Run(ctx, "kb", wf.ID, "manual")
	require.NoError(t, err)
	require.Equal(t, "all", run.WorkflowSnapshot[0].Config["scope"])
	run.WorkflowSnapshot[0].Config["scope"] = "team"
	require.NoError(t, db.Save(run).Error)
	require.NoError(t, svc.Handle(ctx, queue.tasks[0].task))
	run, err = svc.GetRun(ctx, "kb", run.ID)
	require.NoError(t, err)
	require.Equal(t, types.WorkflowStatusCompleted, run.Status)
	require.Len(t, run.Steps, 1)
	require.Equal(t, types.WorkflowStepSucceeded, run.Steps[0].Status)
}

type contextQueueRecorder struct {
	interfaces.TaskEnqueuer
	task  *asynq.Task
	queue string
}

func (q *contextQueueRecorder) Enqueue(task *asynq.Task, options ...asynq.Option) (*asynq.TaskInfo, error) {
	q.task = task
	for _, option := range options {
		if option.Type() == asynq.QueueOpt {
			q.queue, _ = option.Value().(string)
		}
	}
	return &asynq.TaskInfo{}, nil
}

func TestKnowledgeContextEnqueuesContextQueue(t *testing.T) {
	queue := &contextQueueRecorder{}
	require.NoError(t, enqueueKnowledgeContext(context.Background(), queue, types.KnowledgeAutoTagPayload{TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "source"}))
	require.Equal(t, types.TypeKnowledgeContext, queue.task.Type())
	require.Equal(t, "context", queue.queue)
	declared, ok := types.QueueForTaskType(queue.task.Type())
	require.True(t, ok)
	require.Equal(t, queue.queue, declared)
}

type legacyContextAgentKBService struct {
	fakeAgentKnowledgeBaseService
}

func (s *legacyContextAgentKBService) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func TestAgentRegistersSavedKnowledgeContextToolAlias(t *testing.T) {
	var saved types.CustomAgent
	require.NoError(t, json.Unmarshal([]byte(`{"config":{"allowed_tools":["query_knowledge_graph"],"kb_selection_mode":"all"}}`), &saved))
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1}
	kb.EnsureDefaults()
	svc := &agentService{knowledgeBaseService: &legacyContextAgentKBService{fakeAgentKnowledgeBaseService{kb: kb}}}
	registry := tools.NewToolRegistry()
	config := &types.AgentConfig{AllowedTools: saved.Config.AllowedTools, SearchTargets: types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb"}}}
	require.NoError(t, svc.registerTools(context.Background(), registry, config, nil, nil, "saved-session"))
	require.Equal(t, []string{tools.ToolQueryKnowledgeContext}, registry.ListTools())
	current, err := registry.GetTool(tools.ToolQueryKnowledgeContext)
	require.NoError(t, err)
	legacy, err := registry.GetTool("query_knowledge_graph")
	require.NoError(t, err)
	require.Same(t, current, legacy)
}
