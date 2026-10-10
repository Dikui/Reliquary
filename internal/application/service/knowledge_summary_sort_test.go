package service

import (
	"testing"

	"reliquary/internal/types"
)

func TestSortChunksForSummaryUsesStartAt(t *testing.T) {
	t.Parallel()
	chunks := []*types.Chunk{
		{ID: "later", ChunkIndex: 2, StartAt: 50, Content: "later"},
		{ID: "earlier", ChunkIndex: 1, StartAt: 0, Content: "earlier"},
	}

	sorted := sortChunksForSummary(chunks)

	if len(sorted) != 2 || sorted[0].ID != "earlier" || sorted[1].ID != "later" {
		t.Fatalf("unedited chunks should sort by start offset, got %#v", sorted)
	}
}
