package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type entryLibraryLookup struct {
	interfaces.LibraryService
	expected   string
	wrongCalls int
}

func (s *entryLibraryLookup) GetLibraryByID(ctx context.Context, id string) (*types.Library, error) {
	if id != s.expected {
		s.wrongCalls++
		return nil, fmt.Errorf("stale task library was used")
	}
	return s.LibraryService.GetLibraryByID(ctx, id)
}

func TestWorkersUseEntryLibraryInsteadOfStaleTaskLibrary(t *testing.T) {
	for _, kind := range []string{"document", "manual"} {
		t.Run(kind, func(t *testing.T) {
			h := newArticleProcessingHarness(t, false)
			var entry *types.Entry
			var err error
			if kind == "document" {
				entry, err = h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, "worker.md", "# Worker\n\nOriginal worker evidence."), map[string]string{"publisher": "Fixture", "original_url": "https://example.org/worker", "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
			} else {
				entry, err = h.service.createLegacyManualFixture(h.ctx, h.library.ID, &legacyManualFixturePayload{Title: "Worker", Content: "# Worker\n\nOriginal manual evidence.", Status: types.ManualEntryStatusPublish, SourceInfo: types.SourceInfo{Publisher: "Fixture", OriginalURL: "https://example.org/worker", PublishedAt: "2026-10-01"}}, types.ChannelWeb)
			}
			require.NoError(t, err)
			require.NotEmpty(t, h.queue.tasks)
			queued := h.queue.tasks[0]
			var payload map[string]any
			require.NoError(t, json.Unmarshal(queued.Payload(), &payload))
			payload["library_id"] = "stale-library"
			raw, err := json.Marshal(payload)
			require.NoError(t, err)
			h.queue.tasks[0] = taskqueue.NewTask(queued.Type(), raw)
			lookup := &entryLibraryLookup{LibraryService: h.service.libraryService, expected: entry.LibraryID}
			h.service.libraryService = lookup
			h.drain(t)
			require.Zero(t, lookup.wrongCalls)
			require.Equal(t, types.ParseStatusCompleted, h.article(t, entry.ID).ParseStatus)
		})
	}
}
