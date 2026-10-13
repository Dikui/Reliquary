package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

func TestLegacyArticleReprocessingMarksMissingBodyAndCompletesThroughSameLink(t *testing.T) {
	for _, channel := range []string{types.ChannelRSS, types.ChannelWeb} {
		for _, kind := range []string{"url", "file_url", types.EntryTypeManual, "metadata_with_intake"} {
			t.Run(channel+"_"+kind, func(t *testing.T) {
				h, _ := newIngestHarness(t)
				transport := forbidIngestNetwork(t, h)
				originalURL := "https://example.org/legacy-" + channel + "-" + kind
				entry := &types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: kind, SourceKind: "article", Channel: channel,
					Source: originalURL, Title: "Legacy publication", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", ContentType: "tutorial",
					ProcessingMode: types.ProcessingModeSummaryOnly, ExternalCompletionAttempted: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
					Metadata: types.JSON(`{"publisher":"Legacy Journal","datasource_id":"removed-feed","original_url":"` + originalURL + `"}`)}
				if kind == "metadata_with_intake" {
					entry.Type, entry.Source = types.EntryTypeManual, types.EntryTypeManual
					require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{Source: types.EntryTypeManual, Decision: "approved", SourceInfo: types.SourceInfo{Publisher: "Legacy Journal", Author: "Legacy Author"}}))
				}
				require.NoError(t, entry.SetIngestMissing([]types.IngestMissing{{Type: "image", SourceURL: "https://example.org/legacy.png", Reason: "既有缺图原因"}}))
				require.NoError(t, h.db.Create(entry).Error)
				beforeTasks := len(h.queue.tasks)
				result, err := h.service.ReparseEntry(h.ctx, entry.ID, nil)
				require.NoError(t, err)
				require.True(t, result.Incomplete)
				require.False(t, result.ExternalCompletionAttempted)
				require.Equal(t, beforeTasks, len(h.queue.tasks))
				stored := h.article(t, entry.ID)
				require.Equal(t, channel, stored.Channel)
				require.Equal(t, types.ParseStatusCompleted, stored.ParseStatus)
				require.Equal(t, "removed-feed", stored.GetMetadata()["datasource_id"])
				require.Equal(t, normalizeMessageURL(originalURL), stored.NormalizedURL)
				intake, err := stored.IntakeMetadata()
				require.NoError(t, err)
				require.Equal(t, originalURL, intake.SourceInfo.OriginalURL)
				if kind == "metadata_with_intake" {
					require.Equal(t, "Legacy Author", intake.SourceInfo.Author)
				}
				require.Len(t, stored.IngestMissing(), 2)
				require.Equal(t, "既有缺图原因", stored.IngestMissing()[0].Reason)
				require.Equal(t, types.IngestMissing{Type: "content", SourceURL: originalURL, Reason: "重新处理需要正文", SubmittedAt: stored.IngestMissing()[1].SubmittedAt}, stored.IngestMissing()[1])
				checks, err := h.service.CheckURLs(h.ctx, []string{originalURL})
				require.NoError(t, err)
				require.Equal(t, entry.ID, checks[0].EntryID)
				supplement, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: originalURL, Publisher: "Legacy Journal", Acquisition: "verbatim", Content: "An original experiment reports instrument settings and complete measured research observations."})
				require.NoError(t, err)
				require.Equal(t, entry.ID, supplement.EntryID, "same-link completion must retain the historical article")
				h.drain(t)
				stored = h.article(t, entry.ID)
				require.Equal(t, channel, stored.Channel)
				require.False(t, hasIngestMissing(stored.IngestMissing(), "content", ""))
				require.True(t, hasIngestMissing(stored.IngestMissing(), "image", "https://example.org/legacy.png"))
				intake, err = stored.IntakeMetadata()
				require.NoError(t, err)
				require.Equal(t, originalURL, intake.SourceInfo.OriginalURL)
				if kind == "metadata_with_intake" {
					require.Equal(t, "Legacy Author", intake.SourceInfo.Author)
				}
				var count int64
				require.NoError(t, h.db.Model(&types.Entry{}).Count(&count).Error)
				require.EqualValues(t, 1, count)
				require.Zero(t, transport.calls.Load())
			})
		}
	}
}

func TestLegacyQueuedURLTasksNeverRetrieveContent(t *testing.T) {
	for _, field := range []string{"url", "file_url"} {
		t.Run(field, func(t *testing.T) {
			h, _ := newIngestHarness(t)
			transport := forbidIngestNetwork(t, h)
			entry := &types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: "url", SourceKind: "article", Channel: types.ChannelRSS,
				Source: "https://example.org/queued-legacy", Title: "Queued legacy article", ParseStatus: types.ParseStatusPending, EnableStatus: "disabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
			require.NoError(t, h.db.Create(entry).Error)
			raw, err := json.Marshal(map[string]any{"tenant_id": h.library.SpaceID, "knowledge_id": entry.ID, "library_id": h.library.ID, field: entry.Source})
			require.NoError(t, err)
			require.NoError(t, h.service.ProcessDocument(h.ctx, taskqueue.NewTask(types.TypeDocumentProcess, raw)))
			stored := h.article(t, entry.ID)
			require.True(t, stored.Incomplete)
			require.Equal(t, "重新处理需要正文", stored.IngestMissing()[0].Reason)
			require.Empty(t, h.queue.tasks)
			require.Zero(t, transport.calls.Load())
		})
	}
}

func TestLegacySummaryContentTypeCorrectionRequestsExternalBody(t *testing.T) {
	h, v := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	article := uploadedCorrectionSummary(t, h, "https://example.org/legacy-summary-correction")
	_, _, body := readVaultArticle(t, v, article)
	require.Empty(t, body)
	requests := len(h.processor.requests)
	chunks, vectors, articleVectors := correctionIndexCounts(t, h, article.ID)
	corrected, err := h.service.CorrectContentType(h.ctx, article.ID, "research", false)
	require.NoError(t, err)
	require.True(t, corrected.Incomplete)
	require.Equal(t, "修正为全文保存，需要正文", corrected.IngestMissing()[0].Reason)
	require.Equal(t, types.ProcessingModeFull, corrected.ProcessingMode)
	require.Len(t, h.processor.requests, requests)
	_, _, afterBody := readVaultArticle(t, v, corrected)
	require.Empty(t, afterBody)
	newChunks, newVectors, newArticleVectors := correctionIndexCounts(t, h, article.ID)
	require.Equal(t, chunks, newChunks)
	require.Equal(t, vectors, newVectors)
	require.Equal(t, articleVectors, newArticleVectors)
	require.Zero(t, transport.calls.Load())
}

func TestLegacyQueuedURLTasksRespectCompletedCancelledAndDeletingArticles(t *testing.T) {
	for _, status := range []string{types.ParseStatusCompleted, types.ParseStatusCancelled, types.ParseStatusDeleting} {
		t.Run(status, func(t *testing.T) {
			h, _ := newIngestHarness(t)
			transport := forbidIngestNetwork(t, h)
			entry := &types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: "url", SourceKind: "article", Channel: types.ChannelRSS, Source: "https://example.org/retained-state", Title: "Retained state", ParseStatus: status, ProcessingMode: types.ProcessingModeFull, VaultBody: "Stored original body", CreatedAt: time.Now(), UpdatedAt: time.Now()}
			require.NoError(t, h.db.Create(entry).Error)
			before := h.article(t, entry.ID)
			raw, err := json.Marshal(types.DocumentProcessPayload{SpaceID: h.library.SpaceID, EntryID: entry.ID, LibraryID: h.library.ID, URL: entry.Source})
			require.NoError(t, err)
			require.NoError(t, h.service.ProcessDocument(h.ctx, taskqueue.NewTask(types.TypeDocumentProcess, raw)))
			after := h.article(t, entry.ID)
			require.Equal(t, before, after, "obsolete acquisition tasks cannot alter terminal or deleting article states")
			require.Empty(t, h.queue.tasks)
			require.Zero(t, transport.calls.Load())
		})
	}
}

func TestLegacyQueuedURLTaskUsesStoredBodyWithoutFetching(t *testing.T) {
	h, _ := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	body := "An original experiment reports its instrument settings and complete measured research observations."
	entry := &types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: "url", SourceKind: "article", Channel: types.ChannelRSS, Source: "https://example.org/stored-legacy", Title: "Stored legacy", ParseStatus: types.ParseStatusPending, ProcessingMode: types.ProcessingModeFull, VaultBody: body, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, h.db.Create(entry).Error)
	raw, err := json.Marshal(types.DocumentProcessPayload{SpaceID: h.library.SpaceID, EntryID: entry.ID, LibraryID: h.library.ID, FileURL: entry.Source})
	require.NoError(t, err)
	require.NoError(t, h.service.ProcessDocument(h.ctx, taskqueue.NewTask(types.TypeDocumentProcess, raw)))
	h.drain(t)
	stored := h.article(t, entry.ID)
	require.Equal(t, types.ParseStatusCompleted, stored.ParseStatus)
	require.Equal(t, body, stored.VaultBody)
	require.False(t, stored.Incomplete)
	require.Zero(t, transport.calls.Load())
}

func TestLegacyReprocessingUsesDatabaseBodyWithoutVaultPath(t *testing.T) {
	h, _ := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	body := "An original paper preserves all instrument measurements and research observations."
	entry := &types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: "url", SourceKind: "article", Channel: types.ChannelWeb, Source: "https://example.org/database-body", Title: "Database body", ParseStatus: types.ParseStatusCompleted, ProcessingMode: types.ProcessingModeFull, VaultBody: body, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, h.db.Create(entry).Error)
	_, err := h.service.ReparseEntry(h.ctx, entry.ID, nil)
	require.NoError(t, err)
	require.Len(t, h.queue.tasks, 1)
	var payload types.ManualProcessPayload
	require.NoError(t, json.Unmarshal(h.queue.tasks[0].Payload(), &payload))
	require.Equal(t, body, payload.Content)
	h.drain(t)
	stored := h.article(t, entry.ID)
	require.Equal(t, types.ParseStatusCompleted, stored.ParseStatus)
	require.Equal(t, body, stored.VaultBody)
	require.False(t, stored.Incomplete)
	require.Zero(t, transport.calls.Load())
}
