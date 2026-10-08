package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/agent/tools"
	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	chatmodel "reliquary/internal/models/chat"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

const (
	fusionTenantID = uint64(1)
	fusionKBID     = "kb-fusion"
	fusionOwnerID  = "owner-fusion"
	fusionSourceID = "source-fusion"
)

type fusionModelService struct {
	interfaces.ModelService
	model chatmodel.Chat
}

func (s fusionModelService) GetChatModel(context.Context, string) (chatmodel.Chat, error) {
	return s.model, nil
}

type fusionChatInterface interface{ chatmodel.Chat }

type fusionChat struct{ fusionChatInterface }

const fusionExtraction = `{"entities":[{"mention":"Acme Holdings","quote":"Acme Holdings acquired WidgetCo","canonical_name":"Acme Holdings","entity_type":"organization","description":"Acme Holdings","aliases":[]},{"mention":"WidgetCo","quote":"Acme Holdings acquired WidgetCo","canonical_name":"WidgetCo","entity_type":"organization","description":"WidgetCo","aliases":[]}],"facts":[{"kind":"relation","subject":"Acme Holdings","predicate":"acquired","value":null,"value_type":"entity","unit":"","object":"WidgetCo","effective_from":"2026-09-25","effective_to":"","quote":"Acme Holdings acquired WidgetCo on 2026-09-25."},{"kind":"attribute","subject":"Acme Holdings","predicate":"transaction_status","value":"complete","value_type":"string","unit":"","object":"","effective_from":"2026-09-25","effective_to":"","quote":"Acme Holdings acquired WidgetCo on 2026-09-25."}],"events":[{"title":"Acme Holdings acquired WidgetCo","event_type":"acquisition","description":"Acme Holdings completed its acquisition of WidgetCo.","occurred_at":"2026-09-25","entity_mentions":["Acme Holdings","WidgetCo"],"quote":"Acme Holdings acquired WidgetCo on 2026-09-25."}],"attention":{"novelty":80,"impact":85,"credibility":90,"relevance":90,"actionability":55,"score":82,"reason":"A confirmed acquisition changes company ownership."}}`

func (fusionChat) Chat(_ context.Context, messages []chatmodel.Message, _ *chatmodel.ChatOptions) (*types.ChatResponse, error) {
	if len(messages) > 0 && strings.Contains(messages[0].Content, "知识库研究报告撰写者") {
		return &types.ChatResponse{Content: "## 交易进展\nAcme Holdings 已完成对 WidgetCo 的收购。[C1]"}, nil
	}
	return &types.ChatResponse{Content: fusionExtraction}, nil
}

func (fusionChat) GetModelName() string { return "fusion-deterministic" }
func (fusionChat) GetModelID() string   { return "fusion-deterministic" }

type fusionTaskQueue struct{ tasks []*asynq.Task }

func (q *fusionTaskQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{ID: task.Type()}, nil
}

type fusionEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   any             `json:"error"`
}

func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m))
}

func TestKnowledgeFusionPersistsGraphWorkflowReportAndEvidence(t *testing.T) {
	db := fusionFixtureDB(t)

	ctx := fusionContext(fusionTenantID, fusionOwnerID, types.TenantRoleContributor)
	now := time.Now().UTC()
	require.NoError(t, db.Create(&types.Tenant{ID: fusionTenantID, Name: "Fusion tenant", Business: "test"}).Error)
	require.NoError(t, db.Create(&types.User{ID: fusionOwnerID, Username: fusionOwnerID, Email: "fusion@example.test", PasswordHash: "test-only", TenantID: fusionTenantID, IsActive: true}).Error)
	memberRepo := apprepo.NewTenantMemberRepository(db)
	require.NoError(t, memberRepo.Create(ctx, &types.TenantMember{
		UserID: fusionOwnerID, TenantID: fusionTenantID, Role: types.TenantRoleContributor,
		Status: types.TenantMemberStatusActive, JoinedAt: now,
	}))
	kbRepo := apprepo.NewKnowledgeBaseRepository(db)
	knowledgeRepo := apprepo.NewKnowledgeRepository(db)
	kbService := service.NewKnowledgeBaseService(db, kbRepo, knowledgeRepo, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil)
	kb := &types.KnowledgeBase{
		DomainID: service.BuiltinAIKnowledgeDomainID,
		ID:       fusionKBID, Name: "Fusion KB", TenantID: fusionTenantID, CreatorID: fusionOwnerID,
		Type: "document", EmbeddingModelID: "unused", SummaryModelID: "fusion-deterministic",
	}
	kb.EnsureDefaults()
	require.NoError(t, kbRepo.CreateKnowledgeBase(ctx, kb))

	knowledge, err := service.NewKnowledgeService(db, nil, knowledgeRepo, nil, kbService, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, fusionModelService{model: fusionChat{}}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	source := &types.Knowledge{
		ID: fusionSourceID, TenantID: fusionTenantID, KnowledgeBaseID: fusionKBID,
		Type: types.KnowledgeTypeManual, Title: "Acquisition announcement", Source: "manual",
		ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: now, UpdatedAt: now,
		CustomMetadata: types.JSON(`{}`),
	}
	const sourceBody = "Acme Holdings acquired WidgetCo on 2026-09-25. The closing is complete."
	require.NoError(t, source.SetManualMetadata(&types.ManualKnowledgeMetadata{
		Content: sourceBody, Format: types.ManualKnowledgeFormatMarkdown, Status: types.ManualKnowledgeStatusPublish,
	}))
	require.NoError(t, source.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{
		Decision:   "approved",
		SourceInfo: types.SourceInfo{Publisher: "Daily News", OriginalURL: "https://news.example/acme-widgetco", PublishedAt: "2026-09-25T10:00:00Z"},
	}))
	require.NoError(t, knowledgeRepo.CreateKnowledge(ctx, source))
	// The integration's open facts and event are requested by an enabled
	// object-tracking task before intake, using the existing workflow condition.
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "fusion-tracked-acme", TenantID: fusionTenantID,
		CanonicalName: "Acme Holdings", NormalizedName: "acme holdings", EntityType: "organization"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeWorkflow{ID: "fusion-intake-tracking", TenantID: fusionTenantID,
		KnowledgeBaseID: fusionKBID, Name: "Acme intake focus", Enabled: true,
		Steps: []types.KnowledgeWorkflowStep{{ID: "facts", Type: "fact_query", Config: map[string]any{"entity_id": "fusion-tracked-acme"}}}}).Error)

	modelService := fusionModelService{model: fusionChat{}}
	contextService := service.NewKnowledgeContextService(db, modelService)
	contextWorker := service.NewKnowledgeContextWorker(db, contextService, knowledge, nil, nil)
	contextPayload, err := json.Marshal(types.KnowledgeAutoTagPayload{
		TenantID: fusionTenantID, KnowledgeBaseID: fusionKBID, KnowledgeID: fusionSourceID,
	})
	require.NoError(t, err)
	require.NoError(t, contextWorker.Handle(ctx, asynq.NewTask(types.TypeKnowledgeAutoTag, contextPayload)))

	graphTool := tools.NewQueryKnowledgeGraphTool(kbService).WithContextQuery(func(ctx context.Context, id string, query types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
		return contextService.Query(ctx, id, query)
	})
	graphArgs, err := json.Marshal(tools.QueryKnowledgeGraphInput{KnowledgeBaseIDs: []string{fusionKBID}, Query: "Acme Holdings"})
	require.NoError(t, err)
	graphResult, err := graphTool.Execute(ctx, graphArgs)
	require.NoError(t, err)
	require.True(t, graphResult.Success, graphResult.Error)
	graphData := graphResult.Data["graph_data"].(map[string]any)
	require.Equal(t, 1, graphData["total_edges"])
	graphContext := graphResult.Data["contexts"].(map[string]*types.KnowledgeContextResult)[fusionKBID]
	require.Len(t, graphContext.Facts, 2)
	require.Empty(t, graphContext.Evidence[0].Body, "graph queries include evidence metadata, not the full source snapshot")

	taskQueue := &fusionTaskQueue{}
	workflowService := service.NewKnowledgeWorkflowService(db, kbService, knowledge, contextService, nil, nil,
		modelService, memberRepo, taskQueue, nil)
	kbHandler := handler.NewKnowledgeBaseHandler(kbService, knowledge, nil, nil, nil, nil, nil)
	contextHandler := handler.NewKnowledgeContextHandler(contextService, contextWorker)
	workflowHandler := handler.NewKnowledgeWorkflowHandler(workflowService)
	enabled := true
	guards := newRBACGuards(&config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}, kbHandler, nil, nil, nil, nil, kbService, knowledge, nil)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), fusionIdentityMiddleware())
	v1 := engine.Group("/api/v1")
	RegisterKnowledgeContextRoutes(v1, contextHandler, guards)
	RegisterKnowledgeWorkflowRoutes(v1, workflowHandler, guards)

	queryRec := fusionRequest(t, engine, http.MethodPost, "/api/v1/knowledge-bases/"+fusionKBID+"/context/query",
		`{"entity":"Acme Holdings","include_history":true,"limit":20}`, fusionTenantID, fusionOwnerID, types.TenantRoleContributor)
	require.Equal(t, http.StatusOK, queryRec.Code, queryRec.Body.String())
	var queryResponse fusionEnvelope
	require.NoError(t, json.Unmarshal(queryRec.Body.Bytes(), &queryResponse))
	require.True(t, queryResponse.Success)
	var contextResult types.KnowledgeContextResult
	require.NoError(t, json.Unmarshal(queryResponse.Data, &contextResult))
	require.Len(t, contextResult.Facts, 2)
	require.Len(t, contextResult.Evidence, 1)
	evidenceID := contextResult.Evidence[0].ID
	require.Equal(t, sourceBody, contextResult.Evidence[0].Body)

	snapshotRec := fusionRequest(t, engine, http.MethodGet,
		"/api/v1/knowledge-bases/"+fusionKBID+"/context/evidence/"+evidenceID, "",
		fusionTenantID, fusionOwnerID, types.TenantRoleContributor)
	require.Equal(t, http.StatusOK, snapshotRec.Code, snapshotRec.Body.String())
	var snapshotResponse fusionEnvelope
	require.NoError(t, json.Unmarshal(snapshotRec.Body.Bytes(), &snapshotResponse))
	var evidence types.KnowledgeEvidence
	require.NoError(t, json.Unmarshal(snapshotResponse.Data, &evidence))
	require.Equal(t, sourceBody, evidence.Body)
	require.Equal(t, "https://news.example/acme-widgetco", evidence.OriginalURL)

	workflowBody := `{"name":"Acquisition report","enabled":true,"schedule":{"timezone":"UTC"},"steps":[{"id":"facts","type":"fact_query","config":{"entity":"Acme Holdings","limit":20}},{"id":"report","type":"report","depends_on":["facts"],"config":{"title":"Acquisition report","prompt":"Summarize the sourced acquisition fact."}},{"id":"review","type":"human_review","depends_on":["report"],"config":{"message":"Review the cited report."}}]}`
	createRec := fusionRequest(t, engine, http.MethodPost,
		"/api/v1/knowledge-bases/"+fusionKBID+"/workflows", workflowBody,
		fusionTenantID, fusionOwnerID, types.TenantRoleContributor)
	require.Equal(t, http.StatusOK, createRec.Code, createRec.Body.String())
	var workflowResponse fusionEnvelope
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &workflowResponse))
	var workflow types.KnowledgeWorkflow
	require.NoError(t, json.Unmarshal(workflowResponse.Data, &workflow))
	require.True(t, workflow.Enabled)

	runRec := fusionRequest(t, engine, http.MethodPost,
		fmt.Sprintf("/api/v1/knowledge-bases/%s/workflows/%s/run", fusionKBID, workflow.ID), "{}",
		fusionTenantID, fusionOwnerID, types.TenantRoleContributor)
	require.Equal(t, http.StatusOK, runRec.Code, runRec.Body.String())
	var runResponse fusionEnvelope
	require.NoError(t, json.Unmarshal(runRec.Body.Bytes(), &runResponse))
	var run types.KnowledgeWorkflowRun
	require.NoError(t, json.Unmarshal(runResponse.Data, &run))
	require.Equal(t, types.WorkflowStatusQueued, run.Status)
	require.Len(t, taskQueue.tasks, 1)
	require.NoError(t, workflowService.Handle(ctx, taskQueue.tasks[0]))
	run = fusionGetRun(t, engine, run.ID, fusionTenantID, fusionOwnerID)
	require.Equal(t, types.WorkflowStatusPaused, run.Status)
	require.Len(t, run.Steps, 3)
	require.Equal(t, types.WorkflowStepSucceeded, run.Steps[0].Status)
	require.Equal(t, types.WorkflowStepSucceeded, run.Steps[1].Status)
	require.Equal(t, types.WorkflowStepWaitingReview, run.Steps[2].Status)

	var storedReport types.KnowledgeWorkflowReport
	require.NoError(t, db.Where("run_id = ? AND step_id = ?", run.ID, "report").First(&storedReport).Error)
	require.Contains(t, storedReport.Markdown, "[C1]")
	require.Len(t, storedReport.Citations, 1)
	require.Equal(t, evidenceID, storedReport.Citations[0].EvidenceID)

	decisionRec := fusionRequest(t, engine, http.MethodPost,
		fmt.Sprintf("/api/v1/knowledge-bases/%s/workflows/runs/%s/decision", fusionKBID, run.ID),
		`{"decision":"approve"}`, fusionTenantID, fusionOwnerID, types.TenantRoleContributor)
	require.Equal(t, http.StatusOK, decisionRec.Code, decisionRec.Body.String())
	require.Len(t, taskQueue.tasks, 2)
	require.NoError(t, workflowService.Handle(ctx, taskQueue.tasks[1]))
	run = fusionGetRun(t, engine, run.ID, fusionTenantID, fusionOwnerID)
	require.Equal(t, types.WorkflowStatusCompleted, run.Status)
	require.Len(t, run.Steps, 3)
	for _, step := range run.Steps {
		require.Equal(t, types.WorkflowStepSucceeded, step.Status)
	}

	markdownRec := fusionRequest(t, engine, http.MethodGet,
		fmt.Sprintf("/api/v1/knowledge-bases/%s/reports/%s/download", fusionKBID, storedReport.ID), "",
		fusionTenantID, fusionOwnerID, types.TenantRoleContributor)
	require.Equal(t, http.StatusOK, markdownRec.Code, markdownRec.Body.String())
	require.Contains(t, markdownRec.Header().Get("Content-Type"), "text/markdown")
	require.Contains(t, markdownRec.Body.String(), "Acme Holdings 已完成对 WidgetCo 的收购。[C1]")

	jsonRec := fusionRequest(t, engine, http.MethodGet,
		fmt.Sprintf("/api/v1/knowledge-bases/%s/reports/%s/download?format=json", fusionKBID, storedReport.ID), "",
		fusionTenantID, fusionOwnerID, types.TenantRoleContributor)
	require.Equal(t, http.StatusOK, jsonRec.Code, jsonRec.Body.String())
	require.Contains(t, jsonRec.Header().Get("Content-Type"), "application/json")
	var downloaded types.KnowledgeWorkflowReport
	require.NoError(t, json.Unmarshal(jsonRec.Body.Bytes(), &downloaded))
	require.Equal(t, storedReport.ID, downloaded.ID)
	require.Equal(t, evidenceID, downloaded.Citations[0].EvidenceID)

	crossTenantRec := fusionRequest(t, engine, http.MethodGet,
		"/api/v1/knowledge-bases/"+fusionKBID+"/context/evidence/"+evidenceID, "",
		2, "outsider", types.TenantRoleViewer)
	require.Equal(t, http.StatusForbidden, crossTenantRec.Code, "another tenant must not read the evidence snapshot")
}

func fusionFixtureDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.Open(t)
	require.NoError(t, db.Create(service.BuiltinAIKnowledgeDomain()).Error)
	return db
}

func fusionContext(tenantID uint64, userID string, role types.TenantRole) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	return context.WithValue(ctx, types.TenantRoleContextKey, role)
}

func fusionIdentityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := uint64(1)
		if raw := c.GetHeader("X-Test-Tenant"); raw != "" {
			_, _ = fmt.Sscanf(raw, "%d", &tenantID)
		}
		userID := c.GetHeader("X-Test-User")
		if userID == "" {
			userID = fusionOwnerID
		}
		role := types.TenantRole(c.GetHeader("X-Test-Role"))
		if role == "" {
			role = types.TenantRoleContributor
		}
		ctx := fusionContext(tenantID, userID, role)
		c.Set(types.TenantIDContextKey.String(), tenantID)
		c.Set(types.TenantRoleContextKey.String(), role)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func fusionRequest(t *testing.T, engine *gin.Engine, method, target, body string, tenantID uint64, userID string, role types.TenantRole) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Tenant", fmt.Sprint(tenantID))
	req.Header.Set("X-Test-User", userID)
	req.Header.Set("X-Test-Role", string(role))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func fusionGetRun(t *testing.T, engine *gin.Engine, runID string, tenantID uint64, userID string) types.KnowledgeWorkflowRun {
	t.Helper()
	rec := fusionRequest(t, engine, http.MethodGet,
		fmt.Sprintf("/api/v1/knowledge-bases/%s/workflows/runs/%s", fusionKBID, runID), "",
		tenantID, userID, types.TenantRoleContributor)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var envelope fusionEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	var run types.KnowledgeWorkflowRun
	require.NoError(t, json.Unmarshal(envelope.Data, &run))
	return run
}
