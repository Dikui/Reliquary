package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func TestBuildKnowledgeIndexContentExcludesCustomMetadata(t *testing.T) {
	t.Parallel()
	knowledge := &types.Knowledge{
		Title:          "  Reliquary Handbook  ",
		CustomMetadata: types.JSON(`{"region":"Shanghai","department":"R&D"}`),
	}

	content := buildKnowledgeIndexContent(knowledge, "Chunk body")

	require.Equal(t, "Reliquary Handbook\nChunk body", content)
	require.NotContains(t, content, "Shanghai")
	require.NotContains(t, content, "department")
}

func TestBuildKnowledgeIndexContentWithoutTitleReturnsContent(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Chunk body", buildKnowledgeIndexContent(&types.Knowledge{
		CustomMetadata: types.JSON(`{"region":"Shanghai"}`),
	}, "Chunk body"))
	require.Equal(t, "Chunk body", buildKnowledgeIndexContent(nil, "Chunk body"))
}

type metadataUpdateKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	knowledge           *types.Knowledge
	summaryStatusUpdate interface{}
}

func (r *metadataUpdateKnowledgeRepo) GetKnowledgeByID(
	_ context.Context, _ uint64, _ string,
) (*types.Knowledge, error) {
	clone := *r.knowledge
	return &clone, nil
}

func (r *metadataUpdateKnowledgeRepo) UpdateKnowledge(_ context.Context, knowledge *types.Knowledge) error {
	clone := *knowledge
	r.knowledge = &clone
	return nil
}

func (r *metadataUpdateKnowledgeRepo) UpdateKnowledgeColumn(
	_ context.Context, _ string, column string, value interface{},
) error {
	if column == "summary_status" {
		r.summaryStatusUpdate = value
	}
	return nil
}

type metadataUpdateChunkRepo struct {
	interfaces.ChunkRepository
	listCalls int
}

type metadataUpdateKBService struct {
	interfaces.KnowledgeBaseService
}

func (metadataUpdateKBService) GetKnowledgeBaseByID(
	_ context.Context, id string,
) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: id, SummaryModelID: "summary-model"}, nil
}

type metadataUpdateTaskEnqueuer struct {
	tasks []*taskqueue.Task
	err   error
}

func (e *metadataUpdateTaskEnqueuer) Enqueue(
	task *taskqueue.Task, _ ...taskqueue.Option,
) (*taskqueue.TaskInfo, error) {
	if e.err != nil {
		return nil, e.err
	}
	e.tasks = append(e.tasks, task)
	return nil, nil
}

func (r *metadataUpdateChunkRepo) ListChunksByKnowledgeID(
	_ context.Context, _ uint64, _ string,
) ([]*types.Chunk, error) {
	r.listCalls++
	return nil, nil
}

func TestUpdateKnowledgeMetadataDoesNotReindexChunks(t *testing.T) {
	t.Parallel()
	repo := &metadataUpdateKnowledgeRepo{knowledge: &types.Knowledge{
		ID:              "knowledge-1",
		TenantID:        7,
		KnowledgeBaseID: "kb-1",
		SummaryStatus:   types.SummaryStatusCompleted,
		CustomMetadata:  types.JSON(`{"region":"Beijing"}`),
	}}
	chunkRepo := &metadataUpdateChunkRepo{}
	taskEnqueuer := &metadataUpdateTaskEnqueuer{}
	service := &knowledgeService{
		repo: repo, chunkRepo: chunkRepo, kbService: metadataUpdateKBService{}, task: taskEnqueuer,
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	err := service.UpdateKnowledge(ctx, &types.Knowledge{
		ID:             "knowledge-1",
		CustomMetadata: types.JSON(`{"region":"Shanghai"}`),
	})

	require.NoError(t, err)
	require.JSONEq(t, `{"region":"Shanghai"}`, string(repo.knowledge.CustomMetadata))
	require.Zero(t, chunkRepo.listCalls)
	require.Nil(t, repo.summaryStatusUpdate)
	require.Empty(t, taskEnqueuer.tasks, "metadata edits do not create a separate summary task")
}

func TestUpdateKnowledgeMetadataDoesNotLeaveTasklessPendingStatus(t *testing.T) {
	t.Parallel()
	repo := &metadataUpdateKnowledgeRepo{knowledge: &types.Knowledge{
		ID: "knowledge-1", TenantID: 7, KnowledgeBaseID: "kb-1",
		SummaryStatus: types.SummaryStatusCompleted,
	}}
	service := &knowledgeService{
		repo: repo, chunkRepo: &metadataUpdateChunkRepo{}, kbService: metadataUpdateKBService{},
		task: &metadataUpdateTaskEnqueuer{err: errors.New("queue unavailable")},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	require.NoError(t, service.UpdateKnowledge(ctx, &types.Knowledge{
		ID: "knowledge-1", CustomMetadata: types.JSON(`{"region":"Shanghai"}`),
	}))
	require.Nil(t, repo.summaryStatusUpdate)
}

func TestUpdateKnowledgeUnchangedMetadataDoesNotRefreshSummary(t *testing.T) {
	t.Parallel()
	repo := &metadataUpdateKnowledgeRepo{knowledge: &types.Knowledge{
		ID: "knowledge-1", TenantID: 7, KnowledgeBaseID: "kb-1",
		SummaryStatus:  types.SummaryStatusCompleted,
		CustomMetadata: types.JSON(`{"department":"R&D","region":"Shanghai"}`),
	}}
	taskEnqueuer := &metadataUpdateTaskEnqueuer{}
	service := &knowledgeService{
		repo: repo, chunkRepo: &metadataUpdateChunkRepo{}, kbService: metadataUpdateKBService{}, task: taskEnqueuer,
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	require.NoError(t, service.UpdateKnowledge(ctx, &types.Knowledge{
		ID:             "knowledge-1",
		CustomMetadata: types.JSON(`{"region":"Shanghai","department":"R&D"}`),
	}))
	require.Empty(t, taskEnqueuer.tasks)
	require.Nil(t, repo.summaryStatusUpdate)
}
