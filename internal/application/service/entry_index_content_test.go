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

func TestBuildEntryIndexContentExcludesCustomMetadata(t *testing.T) {
	t.Parallel()
	entry := &types.Entry{
		Title:          "  Reliquary Handbook  ",
		CustomMetadata: types.JSON(`{"region":"Shanghai","department":"R&D"}`),
	}

	content := buildEntryIndexContent(entry, "Chunk body")

	require.Equal(t, "Reliquary Handbook\nChunk body", content)
	require.NotContains(t, content, "Shanghai")
	require.NotContains(t, content, "department")
}

func TestBuildEntryIndexContentWithoutTitleReturnsContent(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Chunk body", buildEntryIndexContent(&types.Entry{
		CustomMetadata: types.JSON(`{"region":"Shanghai"}`),
	}, "Chunk body"))
	require.Equal(t, "Chunk body", buildEntryIndexContent(nil, "Chunk body"))
}

type metadataUpdateEntryRepo struct {
	interfaces.EntryRepository
	entry               *types.Entry
	summaryStatusUpdate interface{}
}

func (r *metadataUpdateEntryRepo) GetEntryByID(
	_ context.Context, _ uint64, _ string,
) (*types.Entry, error) {
	clone := *r.entry
	return &clone, nil
}

func (r *metadataUpdateEntryRepo) UpdateEntry(_ context.Context, entry *types.Entry) error {
	clone := *entry
	r.entry = &clone
	return nil
}

func (r *metadataUpdateEntryRepo) UpdateEntryColumn(
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

type metadataUpdateLibraryService struct {
	interfaces.LibraryService
}

func (metadataUpdateLibraryService) GetLibraryByID(
	_ context.Context, id string,
) (*types.Library, error) {
	return &types.Library{ID: id, SummaryModelID: "summary-model"}, nil
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

func (r *metadataUpdateChunkRepo) ListChunksByEntryID(
	_ context.Context, _ uint64, _ string,
) ([]*types.Chunk, error) {
	r.listCalls++
	return nil, nil
}

func TestUpdateEntryMetadataDoesNotReindexChunks(t *testing.T) {
	t.Parallel()
	repo := &metadataUpdateEntryRepo{entry: &types.Entry{
		ID:             "entry-1",
		SpaceID:        7,
		LibraryID:      "library-1",
		SummaryStatus:  types.SummaryStatusCompleted,
		CustomMetadata: types.JSON(`{"region":"Beijing"}`),
	}}
	chunkRepo := &metadataUpdateChunkRepo{}
	taskEnqueuer := &metadataUpdateTaskEnqueuer{}
	service := &entryService{
		repo: repo, chunkRepo: chunkRepo, libraryService: metadataUpdateLibraryService{}, task: taskEnqueuer,
	}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(7))

	err := service.UpdateEntry(ctx, &types.Entry{
		ID:             "entry-1",
		CustomMetadata: types.JSON(`{"region":"Shanghai"}`),
	})

	require.NoError(t, err)
	require.JSONEq(t, `{"region":"Shanghai"}`, string(repo.entry.CustomMetadata))
	require.Zero(t, chunkRepo.listCalls)
	require.Nil(t, repo.summaryStatusUpdate)
	require.Empty(t, taskEnqueuer.tasks, "metadata edits do not create a separate summary task")
}

func TestUpdateEntryMetadataDoesNotLeaveTasklessPendingStatus(t *testing.T) {
	t.Parallel()
	repo := &metadataUpdateEntryRepo{entry: &types.Entry{
		ID: "entry-1", SpaceID: 7, LibraryID: "library-1",
		SummaryStatus: types.SummaryStatusCompleted,
	}}
	service := &entryService{
		repo: repo, chunkRepo: &metadataUpdateChunkRepo{}, libraryService: metadataUpdateLibraryService{},
		task: &metadataUpdateTaskEnqueuer{err: errors.New("queue unavailable")},
	}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(7))

	require.NoError(t, service.UpdateEntry(ctx, &types.Entry{
		ID: "entry-1", CustomMetadata: types.JSON(`{"region":"Shanghai"}`),
	}))
	require.Nil(t, repo.summaryStatusUpdate)
}

func TestUpdateEntryUnchangedMetadataDoesNotRefreshSummary(t *testing.T) {
	t.Parallel()
	repo := &metadataUpdateEntryRepo{entry: &types.Entry{
		ID: "entry-1", SpaceID: 7, LibraryID: "library-1",
		SummaryStatus:  types.SummaryStatusCompleted,
		CustomMetadata: types.JSON(`{"department":"R&D","region":"Shanghai"}`),
	}}
	taskEnqueuer := &metadataUpdateTaskEnqueuer{}
	service := &entryService{
		repo: repo, chunkRepo: &metadataUpdateChunkRepo{}, libraryService: metadataUpdateLibraryService{}, task: taskEnqueuer,
	}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(7))

	require.NoError(t, service.UpdateEntry(ctx, &types.Entry{
		ID:             "entry-1",
		CustomMetadata: types.JSON(`{"region":"Shanghai","department":"R&D"}`),
	}))
	require.Empty(t, taskEnqueuer.tasks)
	require.Nil(t, repo.summaryStatusUpdate)
}
