package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type completionKBService struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s completionKBService) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type completionChunkService struct {
	interfaces.ChunkService
	chunks []*types.Chunk
}

func (s completionChunkService) ListChunksByKnowledgeID(context.Context, string) ([]*types.Chunk, error) {
	return s.chunks, nil
}

type completionTaskQueue struct {
	interfaces.TaskEnqueuer
	taskTypes []string
	fail      bool
}

func (q *completionTaskQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.taskTypes = append(q.taskTypes, task.Type())
	if q.fail {
		return nil, errors.New("queue unavailable")
	}
	return &asynq.TaskInfo{ID: "queued"}, nil
}

func TestKnowledgePostProcessCompletesWithoutRemovedTasks(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		chunks    []*types.Chunk
		failQueue bool
		wantTasks []string
	}{
		{name: "empty document"},
		{name: "text document formerly awaited page generation", chunks: []*types.Chunk{{ID: "chunk-1", ChunkType: types.ChunkTypeText}}, wantTasks: []string{types.TypeSummaryGeneration}},
		{name: "image OCR formerly awaited page generation", chunks: []*types.Chunk{{ID: "ocr-1", ChunkType: types.ChunkTypeImageOCR}}, wantTasks: []string{types.TypeSummaryGeneration}},
		{name: "failed summary enqueue releases sole slot", chunks: []*types.Chunk{{ID: "chunk-1", ChunkType: types.ChunkTypeText}}, failQueue: true, wantTasks: []string{types.TypeSummaryGeneration}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := setupHousekeepingDB(t)
			require.NoError(t, db.Exec("ALTER TABLE knowledges ADD COLUMN processed_at TIMESTAMPTZ").Error)
			require.NoError(t, db.Exec("ALTER TABLE knowledges ADD COLUMN description TEXT").Error)
			const knowledgeID = "completion-source"
			insertKnowledge(t, db, knowledgeID, types.ParseStatusProcessing, time.Now())
			repo := repository.NewKnowledgeRepository(db)
			// Saved old indexing configurations must remain readable after removing
			// their optional pipeline field, even when it remains in saved JSON.
			var strategy types.IndexingStrategy
			require.NoError(t, strategy.Scan([]byte(`{"vector_enabled":false,"keyword_enabled":false,"wiki_enabled":true}`)))
			queue := &completionTaskQueue{fail: scenario.failQueue}
			service := &KnowledgePostProcessService{
				knowledgeRepo: repo,
				kbService:     completionKBService{kb: &types.KnowledgeBase{ID: "kb", Type: types.KnowledgeBaseTypeDocument, IndexingStrategy: strategy}},
				chunkService:  completionChunkService{chunks: scenario.chunks},
				taskEnqueuer:  queue,
			}
			payload, err := json.Marshal(types.KnowledgePostProcessPayload{TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: knowledgeID})
			require.NoError(t, err)
			require.NoError(t, service.Handle(context.Background(), asynq.NewTask(types.TypeKnowledgePostProcess, payload)))
			require.Equal(t, scenario.wantTasks, queue.taskTypes)
			if len(scenario.wantTasks) > 0 && !scenario.failQueue {
				pending, err := repo.GetKnowledgeByIDOnly(context.Background(), knowledgeID)
				require.NoError(t, err)
				require.Equal(t, types.ParseStatusFinalizing, pending.ParseStatus)
				require.Equal(t, 1, pending.PendingSubtasksCount)
				_, promoted, err := repo.FinalizeSubtask(context.Background(), knowledgeID)
				require.NoError(t, err)
				require.True(t, promoted)
			}
			completed, err := repo.GetKnowledgeByIDOnly(context.Background(), knowledgeID)
			require.NoError(t, err)
			require.Equal(t, types.ParseStatusCompleted, completed.ParseStatus)
			require.Zero(t, completed.PendingSubtasksCount)
			if len(scenario.chunks) == 0 {
				require.Equal(t, types.SummaryStatusSkipped, completed.SummaryStatus)
			}
			// Duplicate post-process deliveries cannot recreate work after completion.
			require.NoError(t, service.Handle(context.Background(), asynq.NewTask(types.TypeKnowledgePostProcess, payload)))
			require.Equal(t, scenario.wantTasks, queue.taskTypes)
		})
	}
}
