package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"reliquary/internal/testutil/redistest"

	"path/filepath"

	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service/file"
	"reliquary/internal/models/chat"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

func transactionEntryService(s *entryService, tx *gorm.DB) *entryService {
	copy := *s
	copy.db = tx
	copy.repo = repository.NewEntryRepository(tx)
	return &copy
}
func transactionJobs(t *testing.T, db *gorm.DB) []taskqueue.Job {
	t.Helper()
	var jobs []taskqueue.Job
	require.NoError(t, db.Order("created_at,id").Find(&jobs).Error)
	return jobs
}

func assertSamePendingOperations(t *testing.T, before, after []taskqueue.Job) {
	t.Helper()
	require.Len(t, after, len(before))
	after = append([]taskqueue.Job(nil), after...)
	for i := range before {
		if before[i].State == taskqueue.TaskStatePending && after[i].State == taskqueue.TaskStateDispatched {
			require.NotNil(t, after[i].DispatchedAt)
			after[i].State = before[i].State
			after[i].DispatchedAt = before[i].DispatchedAt
			after[i].UpdatedAt = before[i].UpdatedAt
		}
	}
	require.Equal(t, before, after, "duplicate intake preserves the durable operation apart from asynchronous delivery")
}
func transactionManualPayload() *legacyManualFixturePayload {
	return &legacyManualFixturePayload{Title: "Transactional collection", Content: "Original product announcement with durable source details.", Status: types.ManualEntryStatusPublish, SourceInfo: types.SourceInfo{OriginalURL: "https://example.org/transactional-collection", Publisher: "Fixture publisher"}}
}

func TestTaskTransactionCollectionRollbackAndCommit(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	h.service.task = redistest.NewClient(t, h.db)
	rollback := errors.New("abort collection transaction")
	var id string
	require.ErrorIs(t, h.db.WithContext(h.ctx).Transaction(func(tx *gorm.DB) error {
		article, err := transactionEntryService(h.service, tx).createLegacyManualFixture(h.ctx, h.library.ID, transactionManualPayload(), types.ChannelWeb)
		require.NoError(t, err)
		id = article.ID
		jobs := transactionJobs(t, tx)
		require.Len(t, jobs, 1)
		require.Equal(t, types.TypeManualProcess, jobs[0].TaskType)
		require.Empty(t, transactionJobs(t, h.db), "workers cannot see uncommitted collection tasks")
		return rollback
	}), rollback)
	require.Empty(t, transactionJobs(t, h.db))
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", id).Count(&count).Error)
	require.Zero(t, count, "article and task roll back together")
	article, err := h.service.createLegacyManualFixture(h.ctx, h.library.ID, transactionManualPayload(), types.ChannelWeb)
	require.NoError(t, err)
	jobs := transactionJobs(t, h.db)
	require.Len(t, jobs, 1)
	require.Equal(t, types.QueueDefault, jobs[0].Queue)
	require.Equal(t, 3, jobs[0].MaxRetry)
	var payload types.ManualProcessPayload
	require.NoError(t, json.Unmarshal(jobs[0].Payload, &payload))
	require.Equal(t, article.ID, payload.EntryID)
	stored, err := h.service.repo.GetEntryByID(h.ctx, h.library.SpaceID, article.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, stored.ParseStatus)
}

func TestTaskTransactionApproveRollbackAndCommit(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	h.service.task = redistest.NewClient(t, h.db)
	article, err := h.service.createLegacyManualFixture(h.ctx, h.library.ID, transactionManualPayload(), types.ChannelWeb)
	require.NoError(t, err)
	intake, err := article.IntakeMetadata()
	require.NoError(t, err)
	intake.Decision, intake.Reason = "rejected", "User review required"
	article.ParseStatus = types.ParseStatusRejected
	require.NoError(t, article.SetIntakeMetadata(intake))
	require.NoError(t, h.db.Save(article).Error)
	require.NoError(t, h.db.Where("1=1").Delete(&taskqueue.Job{}).Error)
	rollback := errors.New("abort approval transaction")
	require.ErrorIs(t, h.db.WithContext(h.ctx).Transaction(func(tx *gorm.DB) error {
		reviewed, err := transactionEntryService(h.service, tx).ReviewEntryIntake(h.ctx, article.ID, "approve", types.SourceInfo{})
		require.NoError(t, err)
		require.Equal(t, types.ParseStatusPending, reviewed.ParseStatus)
		require.Len(t, transactionJobs(t, tx), 1)
		require.Empty(t, transactionJobs(t, h.db), "workers cannot see uncommitted approval tasks")
		return rollback
	}), rollback)
	require.Empty(t, transactionJobs(t, h.db))
	unchanged, err := h.service.repo.GetEntryByID(h.ctx, h.library.SpaceID, article.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusRejected, unchanged.ParseStatus)
	unchangedIntake, err := unchanged.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "rejected", unchangedIntake.Decision)
	require.Equal(t, intake.OriginalPath, unchangedIntake.OriginalPath)
	var logs int64
	require.NoError(t, h.db.Model(&types.OperationLog{}).Where("operation_type='review_approve'").Count(&logs).Error)
	require.Zero(t, logs, "the user decision rolls back with its task")
	reviewed, err := h.service.ReviewEntryIntake(h.ctx, article.ID, "approve", types.SourceInfo{})
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, reviewed.ParseStatus)
	require.Len(t, transactionJobs(t, h.db), 1)
	saved, err := h.service.repo.GetEntryByID(h.ctx, h.library.SpaceID, article.ID)
	require.NoError(t, err)
	savedIntake, err := saved.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", savedIntake.Decision)
	require.NoError(t, h.db.Model(&types.OperationLog{}).Where("operation_type='review_approve'").Count(&logs).Error)
	require.EqualValues(t, 1, logs)
}

type taskTransactionFailure struct {
	*taskqueue.Client
	onEnqueue func(*gorm.DB)
}

func (q taskTransactionFailure) EnqueueTx(ctx context.Context, tx *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	info, err := q.Client.EnqueueTx(ctx, tx, task, opts...)
	if err != nil {
		return info, err
	}
	if q.onEnqueue != nil {
		q.onEnqueue(tx)
	}
	return info, errors.New("simulated failure after PostgreSQL enqueue")
}

func transactionStoredFiles(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	}))
	return paths
}

func TestTaskTransactionCollectionResourcesRollBackAndNewFilesAreRemoved(t *testing.T) {
	for _, kind := range []string{"upload", "manual_attachment"} {
		t.Run(kind, func(t *testing.T) {
			h := newRetainedIntakeHarness(t)
			root := t.TempDir()
			catalog := NewResourceCatalog(repository.NewResourceRepository(h.db))
			h.service.resourceCatalog = catalog
			h.service.fileSvc = file.NewResourceCatalogFileService(file.NewLocalFileService(root, ""), catalog)
			payload := transactionManualPayload()
			var retainedRef string
			if kind == "manual_attachment" {
				var err error
				retainedRef, err = h.service.fileSvc.SaveBytes(h.ctx, []byte("existing shared attachment"), types.SingleSpaceID, "attachment.txt", false)
				require.NoError(t, err)
				payload.Content += "\n[Shared attachment](" + retainedRef + ")"
			}
			baselineFiles := transactionStoredFiles(t, root)
			var baselineResources int64
			require.NoError(t, h.db.Model(&types.StoredResource{}).Count(&baselineResources).Error)
			client := redistest.NewClient(t, h.db)
			observed := false
			h.service.task = taskTransactionFailure{Client: client, onEnqueue: func(tx *gorm.DB) {
				var resources, bindings int64
				require.NoError(t, tx.Model(&types.StoredResource{}).Count(&resources).Error)
				require.NoError(t, tx.Model(&types.ResourceBinding{}).Count(&bindings).Error)
				require.Equal(t, baselineResources+1, resources)
				require.EqualValues(t, 1, bindings)
				require.Len(t, transactionJobs(t, tx), 1)
				observed = true
			}}
			collect := func() (*types.Entry, error) {
				if kind == "manual_attachment" {
					return h.service.createLegacyManualFixture(h.ctx, h.library.ID, payload, types.ChannelWeb)
				}
				return h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, "durable.md", payload.Content),
					map[string]string{"original_url": payload.SourceInfo.OriginalURL}, nil, "", types.ChannelWeb, nil)
			}
			article, err := collect()
			require.ErrorContains(t, err, "simulated failure")
			require.Nil(t, article)
			require.True(t, observed)
			var resources, bindings, articles int64
			require.NoError(t, h.db.Model(&types.StoredResource{}).Count(&resources).Error)
			require.NoError(t, h.db.Model(&types.ResourceBinding{}).Count(&bindings).Error)
			require.NoError(t, h.db.Model(&types.Entry{}).Count(&articles).Error)
			require.Equal(t, baselineResources, resources, "new resource identities roll back with the article and its task")
			require.Zero(t, bindings, "resource ownership binds use the same transaction")
			require.Zero(t, articles)
			require.Empty(t, transactionJobs(t, h.db))
			require.Equal(t, baselineFiles, transactionStoredFiles(t, root), "failed collection removes only the newly created bytes")
			if retainedRef != "" {
				resource, err := catalog.Resolve(h.ctx, retainedRef)
				require.NoError(t, err)
				require.Equal(t, types.ResourceStateActive, resource.State)
			}
			h.service.task = client
			article, err = collect()
			require.NoError(t, err)
			require.NotNil(t, article)
			require.NoError(t, h.db.Model(&types.StoredResource{}).Count(&resources).Error)
			require.NoError(t, h.db.Model(&types.ResourceBinding{}).Count(&bindings).Error)
			require.Equal(t, baselineResources+1, resources)
			require.EqualValues(t, 1, bindings)
			require.Len(t, transactionStoredFiles(t, root), len(baselineFiles)+1)
			require.Len(t, transactionJobs(t, h.db), 1)
		})
	}
}
func TestTaskTransactionEnqueueFailureRollsBackApprovalAndRetainsOriginal(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	article, err := h.service.createLegacyManualFixture(h.ctx, h.library.ID, transactionManualPayload(), types.ChannelWeb)
	require.NoError(t, err)
	intake, err := article.IntakeMetadata()
	require.NoError(t, err)
	intake.Decision = "rejected"
	article.ParseStatus = types.ParseStatusRejected
	require.NoError(t, article.SetIntakeMetadata(intake))
	require.NoError(t, h.db.Save(article).Error)
	h.service.task = taskTransactionFailure{Client: redistest.NewClient(t, h.db)}
	_, err = h.service.ReviewEntryIntake(h.ctx, article.ID, "approve", types.SourceInfo{})
	require.ErrorContains(t, err, "simulated failure")
	require.Empty(t, transactionJobs(t, h.db))
	saved, err := h.service.repo.GetEntryByID(h.ctx, h.library.SpaceID, article.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusRejected, saved.ParseStatus)
	savedIntake, err := saved.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, intake.OriginalPath, savedIntake.OriginalPath)
	source, err := h.service.fileSvc.GetFile(h.ctx, intake.OriginalPath)
	require.NoError(t, err, "queue failure must retain the previously rejected source")
	require.NoError(t, source.Close())
}

type taskTransactionInterruptedChat struct {
	retainedIntakeModel
	entered     chan struct{}
	interrupted atomic.Bool
}

func (m *taskTransactionInterruptedChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if stage == "entry_understanding" && m.interrupted.CompareAndSwap(false, true) {
		close(m.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return m.retainedIntakeModel.Chat(ctx, messages, opts)
}
func TestTaskTransactionInterruptedArticleWorkerRestartsAndCompletes(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	model := &taskTransactionInterruptedChat{entered: make(chan struct{})}
	h.service.modelService = &summaryStateModelService{model: model}
	first := redistest.NewClient(t, h.db)
	h.service.task = first
	article, err := h.service.createLegacyManualFixture(h.ctx, h.library.ID, transactionManualPayload(), types.ChannelWeb)
	require.NoError(t, err)
	config := taskqueue.Config{Pools: types.WorkerPoolConcurrency{Core: 1}, CheckInterval: 10 * time.Millisecond}
	require.NoError(t, first.Start(context.Background(), taskqueue.HandlerFunc(h.service.ProcessManualUpdate), config))
	t.Cleanup(func() { require.NoError(t, first.Close()) })
	select {
	case <-model.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("article worker did not begin background processing")
	}
	require.NoError(t, first.Close(), "closing the worker simulates an interrupted process")
	jobs := transactionJobs(t, h.db)
	require.Len(t, jobs, 1)
	require.Equal(t, taskqueue.TaskStatePending, jobs[0].State)
	require.Zero(t, jobs[0].Retried, "process interruption does not consume ordinary retries")
	stored, err := h.service.repo.GetEntryByID(h.ctx, h.library.SpaceID, article.ID)
	require.NoError(t, err)
	preserved, err := stored.IntakeMetadata()
	require.NoError(t, err)
	require.NotEqual(t, "error", preserved.Decision)
	require.NotEmpty(t, preserved.OriginalPath)
	source, err := h.service.fileSvc.GetFile(h.ctx, preserved.OriginalPath)
	require.NoError(t, err)
	body, err := io.ReadAll(source)
	require.NoError(t, err)
	require.Equal(t, transactionManualPayload().Content, string(body), "interrupted intake must retain the original article body")
	require.NoError(t, source.Close())
	restarted := redistest.NewClient(t, h.db)
	h.service.task = restarted
	require.NoError(t, restarted.Start(context.Background(), taskqueue.HandlerFunc(h.service.ProcessManualUpdate), config))
	t.Cleanup(func() { require.NoError(t, restarted.Close()) })
	require.Eventually(t, func() bool {
		completed, err := h.service.repo.GetEntryByID(h.ctx, h.library.SpaceID, article.ID)
		return err == nil && completed.ParseStatus == types.ParseStatusCompleted && completed.SummaryStatus == types.SummaryStatusCompleted && len(transactionJobs(t, h.db)) == 1 && transactionJobs(t, h.db)[0].State == taskqueue.TaskStateCompleted
	}, 5*time.Second, 20*time.Millisecond)
}

func TestTaskTransactionEmbeddingStartAndResumeAreAtomic(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	client := redistest.NewClient(t, h.db)
	h.rebuilder.tasks = taskTransactionFailure{Client: client}
	_, err := h.rebuilder.StartAndEnqueue(h.ctx, types.SingleSpaceID)
	require.ErrorContains(t, err, "simulated failure")
	require.Empty(t, transactionJobs(t, h.db))
	var runs, items int64
	require.NoError(t, h.db.Model(&types.EmbeddingRebuildRun{}).Count(&runs).Error)
	require.NoError(t, h.db.Model(&types.EmbeddingRebuildItem{}).Count(&items).Error)
	require.Zero(t, runs)
	require.Zero(t, items, "snapshot checkpoints roll back with their task")
	h.rebuilder.tasks = client
	run, err := h.rebuilder.StartAndEnqueue(h.ctx, types.SingleSpaceID)
	require.NoError(t, err)
	require.Len(t, transactionJobs(t, h.db), 1)
	require.Equal(t, types.TypeEmbeddingRebuild, transactionJobs(t, h.db)[0].TaskType)
	require.NoError(t, h.db.Where("1=1").Delete(&taskqueue.Job{}).Error)
	require.NoError(t, h.rebuilder.Pause(h.ctx, types.SingleSpaceID, run.ID))
	h.rebuilder.tasks = taskTransactionFailure{Client: client}
	require.ErrorContains(t, h.rebuilder.ContinueAndEnqueue(h.ctx, types.SingleSpaceID, run.ID, false), "simulated failure")
	require.Equal(t, "paused", h.state(t).Status)
	require.Empty(t, transactionJobs(t, h.db))
	h.rebuilder.tasks = client
	require.NoError(t, h.rebuilder.ContinueAndEnqueue(h.ctx, types.SingleSpaceID, run.ID, false))
	require.Equal(t, "pending", h.state(t).Status)
	require.Len(t, transactionJobs(t, h.db), 1)
}
