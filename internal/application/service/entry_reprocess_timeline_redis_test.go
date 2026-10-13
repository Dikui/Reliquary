package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/queue"
	"reliquary/internal/testutil/redistest"
	"reliquary/internal/types"
)

func TestMarkdownReprocessingRecordsOneFinishedAttemptPerExecution(t *testing.T) {
	h, _ := newIngestHarness(t)
	client := redistest.NewClient(t, h.db)
	h.service.task = client
	h.service.spanTracker = NewSpanTracker(repository.NewEntrySpanRepository(h.db), h.db)
	mux := queue.NewServeMux()
	mux.HandleFunc(types.TypeManualProcess, h.service.ProcessManualUpdate)
	mux.HandleFunc(types.TypeDocumentProcess, h.service.ProcessDocument)
	require.NoError(t, client.Start(context.Background(), mux, queue.Config{Pools: types.WorkerPoolConcurrency{Core: 1, Enrichment: 1, Maintenance: 1, Shared: 1}, CheckInterval: 20 * time.Millisecond}))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/timeline-redis", Acquisition: "verbatim", Publisher: "Fixture Journal", Title: "Markdown timeline", Content: "# Calibration record\n\nThe original research records independent pressure measurements, instrument calibration and the complete observation procedure."})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	wait := func(attempts int) {
		require.Eventually(t, func() bool {
			var pending int64
			if h.db.Model(&queue.Job{}).Where("state NOT IN ?", []string{"completed", "cancelled"}).Count(&pending).Error != nil || pending != 0 {
				return false
			}
			var entry types.Entry
			return h.db.First(&entry, "id=?", result.EntryID).Error == nil && entry.ParseStatus == types.ParseStatusCompleted
		}, 20*time.Second, 20*time.Millisecond)
		var roots []types.EntryProcessingSpan
		require.NoError(t, h.db.Where("entry_id=? AND kind=?", result.EntryID, types.SpanKindRoot).Order("attempt").Find(&roots).Error)
		require.Len(t, roots, attempts)
		for i, root := range roots {
			require.Equal(t, i+1, root.Attempt)
			require.Equal(t, types.SpanStatusDone, root.Status)
			require.NotNil(t, root.FinishedAt)
		}
		var open int64
		require.NoError(t, h.db.Model(&types.EntryProcessingSpan{}).Where("entry_id=? AND status IN ?", result.EntryID, []string{types.SpanStatusRunning, types.SpanStatusPending}).Count(&open).Error)
		require.Zero(t, open)
	}
	wait(1)
	_, err = h.service.ReparseEntry(h.ctx, result.EntryID, nil)
	require.NoError(t, err)
	wait(2)
}
