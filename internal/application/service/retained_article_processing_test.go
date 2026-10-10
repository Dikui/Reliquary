package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/models/chat"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type retainedProcessingKB struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s retainedProcessingKB) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}
func (s retainedProcessingKB) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type retainedProcessingQueue struct{ tasks []*asynq.Task }

func (q *retainedProcessingQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{ID: fmt.Sprint(len(q.tasks))}, nil
}

type retainedProcessingChatInterface interface{ chat.Chat }

type retainedProcessingChat struct {
	retainedProcessingChatInterface
	stages []string
}

func (m *retainedProcessingChat) Chat(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	m.stages = append(m.stages, stage)
	result := "Acme documents its new widget product and supported deployment options."
	if stage == "intake_review_and_tag" {
		result = `{"approved":true,"reason":"Original source describes the product","natures":["描述"],"referents":[{"kind":"topic","name":"Products"}]}`
	}
	if stage == "attention" {
		result = `{"attention":{"novelty":30,"impact":40,"credibility":80,"relevance":60,"actionability":40,"score":50,"reason":"Original product announcement"}}`
	}
	return &types.ChatResponse{Content: result}, nil
}
func (*retainedProcessingChat) GetModelID() string   { return "local-fixture" }
func (*retainedProcessingChat) GetModelName() string { return "local-fixture" }

func TestMarkdownArticleCompletesWithoutRemovedBackgroundTasks(t *testing.T) {
	db := pgtest.Open(t)
	tenant := &types.Tenant{ID: 1, Name: "Space"}
	require.NoError(t, db.Create(tenant).Error)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, Name: "Articles", Type: types.KnowledgeBaseTypeDocument,
		SummaryModelID: "local-fixture", ChunkingConfig: types.ChunkingConfig{ChunkSize: 512}}
	require.NoError(t, db.Create(kb).Error)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	ctx = context.WithValue(ctx, types.UserIDContextKey, "personal-user")
	repo := repository.NewKnowledgeRepository(db)
	chunkRepo := repository.NewChunkRepository(db)
	chunks := &chunkService{chunkRepository: chunkRepo}
	model := &retainedProcessingChat{}
	models := &summaryStateModelService{model: model}
	queue := &retainedProcessingQueue{}
	bases := retainedProcessingKB{kb: kb}
	attention := NewKnowledgeAttentionService(db, models)
	svc := &knowledgeService{db: db, repo: repo, chunkRepo: chunkRepo, chunkService: chunks,
		kbService: bases, modelService: models, task: queue, tenantRepo: repository.NewTenantRepository(db),
		fileSvc: &intakeFileService{}, config: &config.Config{Conversation: &config.ConversationConfig{GenerateSummaryPrompt: "Summarize the document."}}}
	article, err := svc.CreateKnowledgeFromManual(ctx, kb.ID, &types.ManualKnowledgePayload{
		Title: "Widget announcement", Content: "# Widget\n\nAcme released its new widget product. The original announcement describes deployment choices, supported platforms and the complete product capabilities.", Status: "publish",
		SourceInfo: types.SourceInfo{Publisher: "Acme", OriginalURL: "https://example.com/widget", PublishedAt: "2026-10-01"},
	}, types.ChannelWeb)
	require.NoError(t, err)
	post := &KnowledgePostProcessService{knowledgeRepo: repo, knowledgeService: svc, kbService: bases, chunkService: chunks, taskEnqueuer: queue, attentionService: attention}
	tags := NewKnowledgeAutoTagService(db, repo, repository.NewKnowledgeTagRepository(db), bases, chunks, models, nil)
	for i := 0; i < len(queue.tasks); i++ {
		require.Less(t, i, 10, "processing should produce a bounded set of tasks")
		task := queue.tasks[i]
		switch task.Type() {
		case types.TypeManualProcess:
			require.NoError(t, svc.ProcessManualUpdate(ctx, task))
		case types.TypeKnowledgePostProcess:
			require.NoError(t, post.Handle(ctx, task))
		case types.TypeSummaryGeneration:
			require.NoError(t, svc.ProcessSummaryGeneration(ctx, task))
		case types.TypeKnowledgeAutoTag:
			require.NoError(t, tags.Handle(ctx, task))
		default:
			t.Fatalf("unexpected background task %q", task.Type())
		}
	}
	completed, err := repo.GetKnowledgeByID(ctx, 1, article.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusCompleted, completed.ParseStatus)
	require.Equal(t, types.SummaryStatusCompleted, completed.SummaryStatus)
	require.Contains(t, completed.Description, "Acme")
	require.Contains(t, model.stages, "attention", "attention scoring remains active")
	var score types.KnowledgeAttention
	require.NoError(t, db.Where("tenant_id = ? AND evidence_id = ?", 1, article.ID).First(&score).Error)
	require.Empty(t, score.EventID)
	require.Equal(t, float64(50), score.Score)
	require.NoError(t, attention.ScoreArticle(ctx, kb, completed, "The same approved article."))
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeAttention{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	encoded, err := json.Marshal(types.KnowledgePostProcessPayload{TenantID: 1, KnowledgeBaseID: kb.ID, KnowledgeID: completed.ID})
	require.NoError(t, err)
	queued := len(queue.tasks)
	require.NoError(t, post.Handle(ctx, asynq.NewTask(types.TypeKnowledgePostProcess, encoded)))
	require.Len(t, queue.tasks, queued, "duplicate post-processing must not spawn more tasks")
}

func TestArticleAttentionRejectsCrossSpaceAndHonorsDisabledSetting(t *testing.T) {
	db := pgtest.Open(t)
	model := &retainedProcessingChat{}
	svc := NewKnowledgeAttentionService(db, &summaryStateModelService{model: model})
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1}
	article := &types.Knowledge{ID: "article", KnowledgeBaseID: "kb", TenantID: 2}
	require.ErrorContains(t, svc.ScoreArticle(ctx, kb, article, "source"), "scope mismatch")
	require.Empty(t, model.stages)
	t.Setenv("RELIQUARY_CONTEXT_ATTENTION_ENABLED", "false")
	article.TenantID = 1
	require.NoError(t, svc.ScoreArticle(ctx, kb, article, "source"))
	require.Empty(t, model.stages)
}

func TestApprovedArticleReplacementPreservesAttentionScoring(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Space"}).Error)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, Name: "Articles", SummaryModelID: "local-fixture"}
	require.NoError(t, db.Create(kb).Error)
	old := &types.Knowledge{ID: "old-article", TenantID: 1, KnowledgeBaseID: kb.ID, Type: types.KnowledgeTypeManual, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, old.SetManualMetadata(types.NewManualKnowledgeMetadata("Acme published the original article.", types.ManualKnowledgeStatusPublish, 1)))
	require.NoError(t, old.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved"}))
	candidate := &types.Knowledge{ID: "updated-article", TenantID: 1, KnowledgeBaseID: kb.ID, Type: types.KnowledgeTypeManual, ParseStatus: types.ParseStatusCompleted, EnableStatus: "disabled", Metadata: types.JSON(`{"source_update_candidate":"true","source_update_replaces":"old-article"}`)}
	require.NoError(t, candidate.SetManualMetadata(types.NewManualKnowledgeMetadata("Acme published a new product update.", types.ManualKnowledgeStatusPublish, 1)))
	require.NoError(t, candidate.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved"}))
	require.NoError(t, db.Create(old).Error)
	require.NoError(t, db.Create(candidate).Error)
	model := &retainedProcessingChat{}
	svc := &knowledgeService{db: db, repo: repository.NewKnowledgeRepository(db), kbService: retainedProcessingKB{kb: kb}, modelService: &summaryStateModelService{model: model}}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	require.NoError(t, svc.promoteSourceUpdateCandidate(ctx, candidate.ID))
	require.Equal(t, []string{"attention"}, model.stages)
	var score types.KnowledgeAttention
	require.NoError(t, db.Where("evidence_id = ?", candidate.ID).First(&score).Error)
	require.Equal(t, float64(50), score.Score)
	require.NoError(t, svc.promoteSourceUpdateCandidate(ctx, candidate.ID))
	require.Len(t, model.stages, 1)
}
