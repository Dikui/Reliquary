package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/models/chat"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type summaryStateKBService struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s summaryStateKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type summaryStateModelService struct {
	interfaces.ModelService
	model chat.Chat
	calls int
	err   error
}

func (s *summaryStateModelService) GetChatModel(context.Context, string) (chat.Chat, error) {
	s.calls++
	return s.model, s.err
}

type summaryStateChatInterface interface{ chat.Chat }

type summaryStateChat struct {
	summaryStateChatInterface
	calls    int
	response string
	err      error
}

func (m *summaryStateChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	m.calls++
	return &types.ChatResponse{Content: m.response}, m.err
}

type summaryStateRetriever struct {
	interfaces.RetrievalService
	deleted []string
}

func (r *summaryStateRetriever) DeleteByChunkIDList(_ context.Context, ids []string, _ int, _ string) error {
	r.deleted = append(r.deleted, ids...)
	return nil
}

func summaryStateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.OpenFixture(t)
	for _, table := range []string{"knowledges", "chunks", "knowledge_processing_spans"} {
		require.NoError(t, db.Exec("CREATE TABLE "+table+" (LIKE public."+table+" INCLUDING DEFAULTS INCLUDING INDEXES)").Error)
	}
	return db
}

func TestKnowledgeSummaryTerminalStates(t *testing.T) {
	for _, mode := range []string{"generation", "refresh"} {
		for _, scenario := range []struct {
			name          string
			content       string
			withoutChunks bool
			disabledText  bool
			withoutModel  bool
			modelErr      error
			resolutionErr error
			emptyResponse bool
			wantStatus    string
		}{
			{name: "image without extracted text", content: "![image](images/no-text.png)", wantStatus: types.SummaryStatusSkipped},
			{name: "image without any configured model", content: "![image](images/no-text.png)", withoutModel: true, wantStatus: types.SummaryStatusSkipped},
			{name: "no text chunks", withoutChunks: true, withoutModel: true, wantStatus: types.SummaryStatusSkipped},
			{name: "disabled text is excluded", content: "This disabled text must not be summarized.", disabledText: true, wantStatus: types.SummaryStatusSkipped},
			{name: "text document", content: "Meeting at 3pm tomorrow to review the quarterly report.", wantStatus: types.SummaryStatusCompleted},
			{name: "model request failed", content: "Meeting at 3pm tomorrow to review the quarterly report.", modelErr: errors.New("model unavailable"), wantStatus: types.SummaryStatusFailed},
			{name: "model resolution failed", content: "Meeting at 3pm tomorrow to review the quarterly report.", resolutionErr: errors.New("model configuration unavailable"), wantStatus: types.SummaryStatusFailed},
			{name: "empty model response", content: "Meeting at 3pm tomorrow to review the quarterly report.", emptyResponse: true, wantStatus: types.SummaryStatusFailed},
		} {
			t.Run(mode+"/"+scenario.name, func(t *testing.T) {
				db := summaryStateDB(t)
				ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
				ctx = types.WithTaskRetryMetadata(ctx, 3, 3)
				kb := &types.KnowledgeBase{ID: "summary-kb", Type: types.KnowledgeBaseTypeDocument,
					IndexingStrategy: types.IndexingStrategy{VectorEnabled: false, KeywordEnabled: false}}
				if !scenario.withoutModel {
					kb.SummaryModelID = "summary-model"
				}
				knowledge := &types.Knowledge{ID: "summary-source", TenantID: 1, KnowledgeBaseID: kb.ID,
					FileType: "png", SummaryStatus: types.SummaryStatusPending, ParseStatus: types.ParseStatusFinalizing,
					PendingSubtasksCount: 1, Description: "Previous summary"}
				if mode == "refresh" {
					knowledge.ParseStatus = types.ParseStatusCompleted
					knowledge.PendingSubtasksCount = 0
				}
				repo := repository.NewKnowledgeRepository(db)
				require.NoError(t, repo.CreateKnowledge(ctx, knowledge))
				chunkRepo := repository.NewChunkRepository(db)
				if !scenario.withoutChunks {
					chunk := &types.Chunk{ID: "summary-text", TenantID: 1, KnowledgeBaseID: kb.ID, KnowledgeID: knowledge.ID,
						ChunkType: types.ChunkTypeText, Content: scenario.content, IsEnabled: !scenario.disabledText,
						EndAt: len([]rune(scenario.content))}
					require.NoError(t, chunkRepo.CreateChunks(ctx, []*types.Chunk{chunk}))
				}
				model := &summaryStateChat{response: "The meeting is at 3pm tomorrow.", err: scenario.modelErr}
				if scenario.emptyResponse {
					model.response = " \n "
				}
				models := &summaryStateModelService{model: model, err: scenario.resolutionErr}
				tracker := NewSpanTracker(repository.NewKnowledgeSpanRepository(db), db)
				_, attempt, err := tracker.OpenAttempt(ctx, knowledge.ID)
				require.NoError(t, err)
				tracker.BeginStage(ctx, knowledge.ID, attempt, types.StagePostProcess, nil)
				service := &knowledgeService{repo: repo, chunkRepo: chunkRepo,
					chunkService: &chunkService{chunkRepository: chunkRepo}, kbService: summaryStateKBService{kb: kb},
					modelService: models, spanTracker: tracker,
					config: &config.Config{Conversation: &config.ConversationConfig{GenerateSummaryPrompt: "Summarize the document."}}}
				if mode == "generation" {
					payload, marshalErr := json.Marshal(types.SummaryGenerationPayload{TenantID: 1, KnowledgeID: knowledge.ID,
						KnowledgeBaseID: kb.ID, Attempt: attempt})
					require.NoError(t, marshalErr)
					err = service.ProcessSummaryGeneration(ctx, asynq.NewTask(types.TypeSummaryGeneration, payload))
				} else {
					_, err = service.RegenerateKnowledgeSummary(ctx, knowledge.ID)
				}
				if scenario.wantStatus == types.SummaryStatusFailed {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				got, err := repo.GetKnowledgeByID(ctx, 1, knowledge.ID)
				require.NoError(t, err)
				require.Equal(t, scenario.wantStatus, got.SummaryStatus)
				require.Equal(t, types.ParseStatusCompleted, got.ParseStatus)
				require.Zero(t, got.PendingSubtasksCount)
				if scenario.wantStatus == types.SummaryStatusSkipped {
					require.Empty(t, got.Description)
					require.Zero(t, models.calls, "empty text must be checked before resolving the model")
					require.Zero(t, model.calls, "empty text must not be sent to the model")
					if mode == "generation" {
						var span types.KnowledgeProcessingSpan
						require.NoError(t, db.Where("knowledge_id = ? AND name = ?", knowledge.ID, "postprocess.summary").First(&span).Error)
						require.Equal(t, types.SpanStatusSkipped, span.Status)
					}
				} else if scenario.wantStatus == types.SummaryStatusCompleted {
					require.Equal(t, model.response, got.Description)
					require.Equal(t, 1, model.calls)
				}
				var summaryChunks int64
				require.NoError(t, db.Model(&types.Chunk{}).Where("knowledge_id = ? AND chunk_type = ?", knowledge.ID, types.ChunkTypeSummary).Count(&summaryChunks).Error)
				require.Zero(t, summaryChunks, "disabled indexing and skipped summaries must not create summary chunks")
			})
		}
	}
}

func TestSummaryRefreshSkippedCanCompleteAfterTextIsAdded(t *testing.T) {
	db := summaryStateDB(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	repo := repository.NewKnowledgeRepository(db)
	chunkRepo := repository.NewChunkRepository(db)
	knowledge := &types.Knowledge{ID: "refresh-source", TenantID: 1, KnowledgeBaseID: "summary-kb",
		ParseStatus: types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusCompleted, Description: "Previous summary"}
	require.NoError(t, repo.CreateKnowledge(ctx, knowledge))
	chunk := &types.Chunk{ID: "refresh-text", TenantID: 1, KnowledgeBaseID: knowledge.KnowledgeBaseID, KnowledgeID: knowledge.ID,
		Content: "![image](no-text.png)", ChunkType: types.ChunkTypeText, IsEnabled: true}
	oldSummary := &types.Chunk{ID: "old-summary", TenantID: 1, KnowledgeBaseID: knowledge.KnowledgeBaseID, KnowledgeID: knowledge.ID,
		Content: "# Summary\nPrevious summary", ChunkType: types.ChunkTypeSummary, IsEnabled: true}
	require.NoError(t, chunkRepo.CreateChunks(ctx, []*types.Chunk{chunk, oldSummary}))
	kb := &types.KnowledgeBase{ID: knowledge.KnowledgeBaseID, Type: types.KnowledgeBaseTypeDocument,
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: false, KeywordEnabled: false}}
	model := &summaryStateChat{response: "Summary of the added text."}
	models := &summaryStateModelService{model: model}
	retriever := &summaryStateRetriever{}
	queue := &metadataUpdateTaskEnqueuer{}
	service := &knowledgeService{repo: repo, chunkRepo: chunkRepo, kbService: summaryStateKBService{kb: kb},
		modelService: models, retrieveEngine: retriever, task: queue,
		config: &config.Config{Conversation: &config.ConversationConfig{GenerateSummaryPrompt: "Summarize the document."}}}
	got, err := service.RegenerateKnowledgeSummary(ctx, knowledge.ID)
	require.NoError(t, err)
	require.Equal(t, types.SummaryStatusSkipped, got.SummaryStatus)
	require.Zero(t, models.calls)
	require.Equal(t, []string{oldSummary.ID}, retriever.deleted)
	disabledSummary, err := chunkRepo.GetChunkByID(ctx, 1, oldSummary.ID)
	require.NoError(t, err)
	require.False(t, disabledSummary.IsEnabled)

	// A skipped summary still accepts a refresh, including when no model is
	// configured. The worker decides whether effective text needs a model.
	require.NoError(t, service.RequestKnowledgeSummaryRefresh(ctx, knowledge.ID))
	queued, err := repo.GetKnowledgeByID(ctx, 1, knowledge.ID)
	require.NoError(t, err)
	require.Equal(t, types.SummaryStatusPending, queued.SummaryStatus)
	require.Len(t, queue.tasks, 1)
	_, err = service.RegenerateKnowledgeSummary(ctx, knowledge.ID)
	require.NoError(t, err)

	chunk.Content = "The added text contains enough details to produce a document summary."
	chunk.ContentRevision++
	chunk.UpdatedAt = time.Now()
	require.NoError(t, chunkRepo.UpdateChunk(ctx, chunk))
	kb.SummaryModelID = "summary-model"
	got, err = service.RegenerateKnowledgeSummary(ctx, knowledge.ID)
	require.NoError(t, err)
	require.Equal(t, types.SummaryStatusCompleted, got.SummaryStatus)
	require.Equal(t, model.response, got.Description)
}

func TestSkippedSummaryRemainsCompletedInKnowledgeListsAndCounts(t *testing.T) {
	db := summaryStateDB(t)
	ctx := context.Background()
	repo := repository.NewKnowledgeRepository(db)
	require.NoError(t, repo.CreateKnowledge(ctx, &types.Knowledge{ID: "skipped-image", TenantID: 1,
		KnowledgeBaseID: "summary-kb", ParseStatus: types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusSkipped}))
	rows, total, err := repo.ListPagedKnowledgeByKnowledgeBaseID(ctx, 1, "summary-kb",
		&types.Pagination{Page: 1, PageSize: 20}, types.KnowledgeListFilter{ParseStatus: types.ParseStatusCompleted})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	require.Equal(t, types.SummaryStatusSkipped, rows[0].SummaryStatus)
	completed, err := repo.CountKnowledgeByStatus(ctx, 1, "summary-kb", []string{types.ParseStatusCompleted})
	require.NoError(t, err)
	require.EqualValues(t, 1, completed)
	failed, err := repo.CountKnowledgeByStatus(ctx, 1, "summary-kb", []string{types.ParseStatusFailed})
	require.NoError(t, err)
	require.Zero(t, failed)
}
