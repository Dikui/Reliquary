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
	if stage == "intake_review" {
		result = `{"approved":true,"reason":"Original source describes the product"}`
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
	svc := &knowledgeService{db: db, repo: repo, chunkRepo: chunkRepo, chunkService: chunks,
		kbService: bases, modelService: models, task: queue, tenantRepo: repository.NewTenantRepository(db),
		fileSvc: &intakeFileService{}, config: &config.Config{ArticleProcessing: &config.ArticleProcessingConfig{GenerateSummaryPrompt: "Summarize the document."}}}
	article, err := svc.CreateKnowledgeFromManual(ctx, kb.ID, &types.ManualKnowledgePayload{
		Title: "Widget announcement", Content: "# Widget\n\nAcme released its new widget product. The original announcement describes deployment choices, supported platforms and the complete product capabilities.", Status: "publish",
		SourceInfo: types.SourceInfo{Publisher: "Acme", OriginalURL: "https://example.com/widget", PublishedAt: "2026-10-01"},
	}, types.ChannelWeb)
	require.NoError(t, err)
	post := &KnowledgePostProcessService{knowledgeRepo: repo, kbService: bases, chunkService: chunks, taskEnqueuer: queue}
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
		default:
			t.Fatalf("unexpected background task %q", task.Type())
		}
	}
	completed, err := repo.GetKnowledgeByID(ctx, 1, article.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusCompleted, completed.ParseStatus)
	require.Equal(t, types.SummaryStatusCompleted, completed.SummaryStatus)
	require.Contains(t, completed.Description, "Acme")
	encoded, err := json.Marshal(types.KnowledgePostProcessPayload{TenantID: 1, KnowledgeBaseID: kb.ID, KnowledgeID: completed.ID})
	require.NoError(t, err)
	queued := len(queue.tasks)
	require.NoError(t, post.Handle(ctx, asynq.NewTask(types.TypeKnowledgePostProcess, encoded)))
	require.Len(t, queue.tasks, queued, "duplicate post-processing must not spawn more tasks")
}
