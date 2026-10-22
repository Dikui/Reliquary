package service

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestSimpleIngestKeepsBodySearchableWhenProcessingModelMissing(t *testing.T) {
	h, _ := newIngestHarness(t)
	h.library.ModelTiers.Generation = ""
	h.library.SummaryModelID = ""
	require.NoError(t, h.db.Model(&types.Library{}).Where("id=?", h.library.ID).Updates(map[string]any{"model_tiers": h.library.ModelTiers, "summary_model_id": ""}).Error)
	request := &types.IngestRequest{Library: h.library.ID, Content: "A short calibration note records a measured pressure of 42 pascals."}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "accepted", first.Status)
	saved := h.article(t, first.EntryID)
	require.Contains(t, saved.VaultBody, "42 pascals")
	require.NotEmpty(t, saved.VaultPath)
	require.Empty(t, saved.Acquisition)
	require.Empty(t, h.processor.requests, "saving must not call any model")
	err = h.service.ProcessManualUpdate(h.ctx, h.queue.tasks[0])
	require.Error(t, err, "missing enrichment model is reported by its background task")
	saved = h.article(t, first.EntryID)
	require.Equal(t, types.ParseStatusCompleted, saved.ParseStatus)
	require.Equal(t, "enabled", saved.EnableStatus)
	require.Equal(t, types.SummaryStatusFailed, saved.SummaryStatus)
	var count int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", saved.ID).Count(&count).Error)
	require.Positive(t, count, "body chunks exist even though enrichment failed")
	hits, err := h.service.retrieveEngine.Retrieve(h.ctx, []types.RetrieveParams{{RetrieverType: types.KeywordsRetrieverType, LibraryIDs: []string{h.library.ID}, Query: "calibration", TopK: 10}})
	require.NoError(t, err)
	require.NotEmpty(t, hits, "body remains searchable without a summary")
	second, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.NotEqual(t, first.EntryID, second.EntryID, "text equality without source identity is not deduplication")
}

func TestSimpleIngestExactRetryReplacementAndBadImageRollback(t *testing.T) {
	h, _ := newIngestHarness(t)
	request := &types.IngestRequest{Library: h.library.ID, SourceRef: "calibration/log", Content: "Calibration result: 42 pascals.", Title: "Calibration"}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	duplicate, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "exists", duplicate.Status)
	require.Equal(t, first.EntryID, duplicate.EntryID)
	require.Len(t, h.queue.tasks, 1)
	request.Content = "Calibration result corrected to 43 pascals."
	replaced, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, first.EntryID, replaced.EntryID)
	require.Contains(t, h.article(t, first.EntryID).VaultBody, "43 pascals")
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, h.queue.tasks[0]), "old queued body must be ignored")
	require.Empty(t, h.processor.requests)
	request.Content = "Bad update must not replace the saved calibration."
	_, encoded, _ := strings.Cut(articleImageDataURI(t, 18), ",")
	broken, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	broken = broken[:len(broken)-12] // Valid dimensions and format, but a truncated image stream.
	request.Images = []types.IngestImage{{SourceURL: "https://example.org/bad.png", Data: base64.StdEncoding.EncodeToString(broken)}}
	_, err = h.service.Ingest(h.ctx, request)
	require.Error(t, err)
	require.Contains(t, h.article(t, first.EntryID).VaultBody, "43 pascals")
	require.Len(t, h.queue.tasks, 2, "failed replacement must not enqueue work")
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("library_id=?", h.library.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
