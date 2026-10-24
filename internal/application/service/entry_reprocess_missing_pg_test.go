package service

import (
	"encoding/json"
	"reliquary/internal/types"
	"testing"
	"time"

	taskqueue "reliquary/internal/queue"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLegacyQueuedURLTasksNeverRetrieveContent(t *testing.T) {
	for _, field := range []string{"url", "file_url"} {
		t.Run(field, func(t *testing.T) {
			h, _ := newIngestHarness(t)
			transport := forbidIngestNetwork(t, h)
			entry := &types.Entry{
				ID:         uuid.NewString(),
				SpaceID:    h.library.SpaceID,
				LibraryID:  h.library.ID,
				Type:       "url",
				SourceKind: "article",
				Channel:    types.ChannelRSS,

				Source:       "https://example.org/queued-legacy",
				Title:        "Queued legacy article",
				ParseStatus:  types.ParseStatusPending,
				EnableStatus: "disabled",
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			}
			require.NoError(t, h.db.Create(entry).Error)
			raw, err := json.Marshal(map[string]any{"tenant_id": h.library.SpaceID, "knowledge_id": entry.ID, "library_id": h.library.ID, field: entry.Source})
			require.NoError(t, err)
			require.ErrorContains(t, h.service.ProcessDocument(h.ctx, taskqueue.NewTask(types.TypeDocumentProcess, raw)), "Markdown")
			require.Empty(t, h.queue.tasks)
			require.Zero(t, transport.calls.Load())
		})
	}
}

func TestLegacyQueuedURLTasksRespectCompletedCancelledAndDeletingArticles(t *testing.T) {
	for _, status := range []string{types.ParseStatusCompleted, types.ParseStatusCancelled, types.ParseStatusDeleting} {
		t.Run(status, func(t *testing.T) {
			h, _ := newIngestHarness(t)
			transport := forbidIngestNetwork(t, h)
			entry := &types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: "url", SourceKind: "article", Channel: types.ChannelRSS, Source: "https://example.org/retained-state", Title: "Retained state", ParseStatus: status, VaultBody: "Stored original body", CreatedAt: time.Now(), UpdatedAt: time.Now()}
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
	entry := &types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: "url", SourceKind: "article", Channel: types.ChannelRSS, Source: "https://example.org/stored-legacy", Title: "Stored legacy", ParseStatus: types.ParseStatusPending, VaultBody: body, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, h.db.Create(entry).Error)
	raw, err := json.Marshal(types.DocumentProcessPayload{SpaceID: h.library.SpaceID, EntryID: entry.ID, LibraryID: h.library.ID, FileURL: entry.Source})
	require.NoError(t, err)
	require.NoError(t, h.service.ProcessDocument(h.ctx, taskqueue.NewTask(types.TypeDocumentProcess, raw)))
	h.drain(t)
	stored := h.article(t, entry.ID)
	require.Equal(t, types.ParseStatusCompleted, stored.ParseStatus)
	require.Equal(t, body, stored.VaultBody)
	require.Zero(t, transport.calls.Load())
}

func TestLegacyReprocessingUsesDatabaseBodyWithoutVaultPath(t *testing.T) {
	h, _ := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	body := "An original paper preserves all instrument measurements and research observations."
	entry := &types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: "url", SourceKind: "article", Channel: types.ChannelWeb, Source: "https://example.org/database-body", Title: "Database body", ParseStatus: types.ParseStatusCompleted, VaultBody: body, CreatedAt: time.Now(), UpdatedAt: time.Now()}
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
	require.Zero(t, transport.calls.Load())
}
