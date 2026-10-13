package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/testutil/redistest"
	"reliquary/internal/types"
)

func awaitOperation(t *testing.T, c *taskqueue.Client, id string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var op taskqueue.Job
		return c.DB().First(&op, "id=?", id).Error == nil && op.State == taskqueue.TaskStateCompleted
	}, 15*time.Second, 20*time.Millisecond)
}
func startEntryOperations(t *testing.T, h *articleProcessingHarness) *taskqueue.Client {
	t.Helper()
	c := redistest.NewClient(t, h.db)
	h.service.task = c
	mux := taskqueue.NewServeMux()
	mux.HandleFunc(types.TypeDocumentProcess, h.service.ProcessDocument)
	mux.HandleFunc(types.TypeManualProcess, h.service.ProcessManualUpdate)
	mux.HandleFunc(types.TypeEntryListDelete, h.service.ProcessEntryListDelete)
	mux.HandleFunc(types.TypeEntryListReparse, h.service.ProcessEntryListReparse)
	require.NoError(t, c.Start(context.Background(), mux, taskqueue.Config{CheckInterval: 20 * time.Millisecond}))
	return c
}
func TestRedisEntryHandlersEndToEnd(t *testing.T) {
	t.Run("document_process", func(t *testing.T) {
		h := newArticleProcessingHarness(t, false)
		c := startEntryOperations(t, h)
		e, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, "redis.md", "# Redis document\n\nOriginal product announcement with durable technical measurements."), map[string]string{"publisher": "Acme", "original_url": "https://example.org/redis-document", "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
		require.NoError(t, err)
		var ops []taskqueue.Job
		require.NoError(t, h.db.Where("task_type=?", types.TypeDocumentProcess).Find(&ops).Error)
		require.Len(t, ops, 1)
		awaitOperation(t, c, ops[0].ID)
		require.Equal(t, types.ParseStatusCompleted, h.article(t, e.ID).ParseStatus)
	})
	t.Run("manual_and_batch_reparse_delete", func(t *testing.T) {
		h := newArticleProcessingHarness(t, false)
		c := startEntryOperations(t, h)
		e, err := h.service.createLegacyManualFixture(h.ctx, h.library.ID, transactionManualPayload(), types.ChannelWeb)
		require.NoError(t, err)
		var op taskqueue.Job
		require.NoError(t, h.db.Where("task_type=?", types.TypeManualProcess).First(&op).Error)
		awaitOperation(t, c, op.ID)
		require.Equal(t, types.ParseStatusCompleted, h.article(t, e.ID).ParseStatus)
		for _, kind := range []string{types.TypeEntryListReparse, types.TypeEntryListDelete} {
			payload, _ := json.Marshal(types.EntryListReparsePayload{SpaceID: 1, EntryIDs: []string{e.ID}})
			info, err := c.Enqueue(taskqueue.NewTask(kind, payload), taskqueue.Queue(types.QueueMaintenance))
			require.NoError(t, err)
			awaitOperation(t, c, info.ID)
			if kind == types.TypeEntryListReparse {
				require.Eventually(t, func() bool {
					var e2 types.Entry
					return h.db.First(&e2, "id=?", e.ID).Error == nil && e2.ParseStatus == types.ParseStatusCompleted
				}, 15*time.Second, 20*time.Millisecond)
			}
		}
		var count int64
		require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", e.ID).Count(&count).Error)
		require.Zero(t, count)
	})
}
func TestRedisEmbeddingHandlersEndToEnd(t *testing.T) {
	t.Run("all_embeddings", func(t *testing.T) {
		h := newAllRebuildFixture(t, false)
		c := redistest.NewClient(t, h.db)
		h.rebuilder.tasks = c
		run, err := h.rebuilder.StartAndEnqueue(h.ctx, 1)
		require.NoError(t, err)
		require.NoError(t, c.Start(context.Background(), taskqueue.HandlerFunc(h.rebuilder.Handle), taskqueue.Config{CheckInterval: 20 * time.Millisecond}))
		var op taskqueue.Job
		require.NoError(t, h.db.Where("task_type=?", types.TypeEmbeddingRebuild).First(&op).Error)
		awaitOperation(t, c, op.ID)
		result, err := h.rebuilder.StatusForRun(h.ctx, 1, run.ID)
		require.NoError(t, err)
		require.Equal(t, "completed", result.LatestRun.Status)
		require.Positive(t, result.LatestRun.CompletedCount)
	})
	t.Run("entity_name", func(t *testing.T) {
		db, ctx, _, rebuilder, models, _ := nameRebuildFixture(t)
		c := redistest.NewClient(t, db)
		rebuilder.tasks = c
		configureNameEmbedding(t, db, "redis-embedding", 2)
		models.models["redis-embedding"] = &nameRebuildEmbedder{dimension: 2, id: "redis-embedding"}
		pendingNameEntity(t, db, "redis-entity", "Redis Entity")
		require.NoError(t, rebuilder.MarkAndEnqueue(ctx, 1, "redis-e2e"))
		require.NoError(t, c.Start(context.Background(), taskqueue.HandlerFunc(rebuilder.Handle), taskqueue.Config{CheckInterval: 20 * time.Millisecond}))
		var op taskqueue.Job
		require.NoError(t, db.Where("task_type=?", types.TypeEntityNameEmbeddingRebuild).First(&op).Error)
		awaitOperation(t, c, op.ID)
		require.False(t, readNameEntity(t, db, "redis-entity").EmbeddingNeedsRebuild)
	})
}
func TestRedisAssetCleanupEndToEnd(t *testing.T) {
	s, ctx, _ := newIngestAssetHarness(t, true)
	c := s.task.(*taskqueue.Client)
	asset, err := s.UploadIngestAsset(ctx, "cleanup.txt", []byte("expired source"))
	require.NoError(t, err)
	require.NoError(t, s.db.Model(&types.IngestAsset{}).Where("id=?", asset.AssetID).Update("created_at", time.Now().Add(-48*time.Hour)).Error)
	var op taskqueue.Job
	require.NoError(t, s.db.Where("task_type=?", types.TypeIngestAssetCleanup).First(&op).Error)
	require.NoError(t, c.RunNow(ctx, op.ID))
	require.NoError(t, c.Start(context.Background(), taskqueue.HandlerFunc(s.HandleIngestAssetCleanup), taskqueue.Config{CheckInterval: 20 * time.Millisecond}))
	awaitOperation(t, c, op.ID)
	var count int64
	require.NoError(t, s.db.Model(&types.IngestAsset{}).Where("id=?", asset.AssetID).Count(&count).Error)
	require.Zero(t, count)
	// Use the normal repository to confirm the task did not create an entry.
	_, err = repository.NewEntryRepository(s.db).GetEntryByID(ctx, 1, asset.AssetID)
	require.Error(t, err)
}

func TestRedisCommittedEntryResultSurvivesLostAcknowledgement(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	c := redistest.NewClient(t, h.db)
	h.service.task = c
	e, err := h.service.createLegacyManualFixture(h.ctx, h.library.ID, transactionManualPayload(), types.ChannelWeb)
	require.NoError(t, err)
	var op taskqueue.Job
	require.NoError(t, h.db.Where("task_type=?", types.TypeManualProcess).First(&op).Error)
	// Simulate a committed business result followed by process loss before the
	// operation outcome and Redis acknowledgement were written.
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, taskqueue.NewTask(op.TaskType, op.Payload)))
	require.Equal(t, types.ParseStatusCompleted, h.article(t, e.ID).ParseStatus)
	var chunks int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", e.ID).Count(&chunks).Error)
	require.NoError(t, h.db.Model(&op).Update("state", taskqueue.TaskStateActive).Error)
	require.NoError(t, c.Start(context.Background(), taskqueue.HandlerFunc(func(context.Context, *taskqueue.Task) error {
		t.Error("committed result must not invoke business handler twice")
		return nil
	}), taskqueue.Config{CheckInterval: 20 * time.Millisecond}))
	awaitOperation(t, c, op.ID)
	var after int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", e.ID).Count(&after).Error)
	require.Equal(t, chunks, after)
}
