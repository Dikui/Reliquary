package search

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// groupAndMergeCurrentContent groups results by EntryID and ChunkType
// using two levels of Go maps. Map iteration order is non-deterministic,
// so the concatenated output can appear in arbitrary order. This test
// verifies that the post-merge sort restores deterministic ordering
// (score desc, then EntryID, ChunkType).
func TestGroupAndMergeOverlapping_DeterministicOrdering(t *testing.T) {
	plugin := &MergeStage{}

	t.Run("sorts by score descending across groups", func(t *testing.T) {
		// Same EntryID, different ChunkTypes -> separate inner-map
		// buckets whose iteration order is randomized.
		chunks := []*types.SearchResult{
			{ID: "low", EntryID: "library-001", ChunkType: "text", StartAt: 0, EndAt: 100, Score: 0.4},
			{ID: "high", EntryID: "library-001", ChunkType: "summary", StartAt: 200, EndAt: 300, Score: 0.9},
			{ID: "mid", EntryID: "library-001", ChunkType: "parent_text", StartAt: 400, EndAt: 500, Score: 0.6},
		}
		results := plugin.groupAndMergeCurrentContent(context.Background(), chunks)

		require.Len(t, results, 3)
		assert.Equal(t, "high", results[0].ID, "highest score (0.9) must be first")
		assert.Equal(t, "mid", results[1].ID, "middle score (0.6) must be second")
		assert.Equal(t, "low", results[2].ID, "lowest score (0.4) must be last")
	})

	t.Run("uses EntryID as first tie-breaker when scores equal", func(t *testing.T) {
		// Different EntryIDs -> separate outer-map buckets whose
		// iteration order is randomized.
		chunks := []*types.SearchResult{
			{ID: "ab", EntryID: "library-ab", ChunkType: "text", StartAt: 0, EndAt: 50, Score: 0.8},
			{ID: "aa", EntryID: "library-aa", ChunkType: "text", StartAt: 0, EndAt: 50, Score: 0.8},
		}
		results := plugin.groupAndMergeCurrentContent(context.Background(), chunks)
		require.Len(t, results, 2)
		assert.Equal(t, "aa", results[0].ID, "same score, library-aa < library-ab")
	})

	t.Run("merged chunk uses max score and is sorted among other groups", func(t *testing.T) {
		// Two overlapping text chunks merge into one (score = max(0.5,0.7) = 0.7),
		// then the merged result competes with a higher-scored summary chunk
		// across the inner-map boundary.
		chunks := []*types.SearchResult{
			{ID: "low-a", EntryID: "library-001", ChunkType: "text", ChunkIndex: 1, Content: "first body", StartAt: 0, EndAt: 50, Score: 0.5},
			{ID: "low-b", EntryID: "library-001", ChunkType: "text", ChunkIndex: 2, Content: "second body", StartAt: 30, EndAt: 80, Score: 0.7},
			{ID: "high", EntryID: "library-001", ChunkType: "summary", StartAt: 200, EndAt: 300, Score: 0.9},
		}
		results := plugin.groupAndMergeCurrentContent(context.Background(), chunks)
		require.Len(t, results, 2, "two text chunks merged into one, plus one summary")
		assert.Equal(t, "high", results[0].ID, "summary (0.9) beats merged text (0.7)")
	})

	t.Run("uses ChunkType as tie-breaker when score and entry match", func(t *testing.T) {
		// Same EntryID, different ChunkTypes -> separate inner-map
		// buckets whose iteration order is randomized.
		chunks := []*types.SearchResult{
			{ID: "txt", EntryID: "library-x", ChunkType: "text", StartAt: 0, EndAt: 50, Score: 0.8},
			{ID: "sum", EntryID: "library-x", ChunkType: "summary", StartAt: 100, EndAt: 150, Score: 0.8},
		}
		results := plugin.groupAndMergeCurrentContent(context.Background(), chunks)
		require.Len(t, results, 2)
		assert.Equal(t, "sum", results[0].ID, "same library+score, 'summary' < 'text'")
	})
}

func TestGroupAndMergeOverlapping_IgnoresStaleEditedRanges(t *testing.T) {
	plugin := &MergeStage{}
	chunks := []*types.SearchResult{
		{
			ID: "edited-outer", EntryID: "doc", ChunkType: "text", ChunkIndex: 1,
			StartAt: 0, EndAt: 200, Content: "completely rewritten outer content", Score: 0.4,
		},
		{
			ID: "edited-inner", EntryID: "doc", ChunkType: "text", ChunkIndex: 2,
			StartAt: 50, EndAt: 100, Content: "independent current inner content", Score: 0.9,
		},
	}

	results := plugin.groupAndMergeCurrentContent(context.Background(), chunks)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Content, chunks[0].Content)
	assert.Contains(t, results[0].Content, chunks[1].Content)
	assert.Equal(t, 0.9, results[0].Score)
}

func TestGroupAndMergeOverlapping_DoesNotMergeNonSequentialChunksFromCoordinates(t *testing.T) {
	plugin := &MergeStage{}
	chunks := []*types.SearchResult{
		{ID: "one", EntryID: "doc", ChunkType: "text", ChunkIndex: 1, StartAt: 0, EndAt: 200, Content: "one"},
		{ID: "three", EntryID: "doc", ChunkType: "text", ChunkIndex: 3, StartAt: 50, EndAt: 100, Content: "three"},
	}

	results := plugin.groupAndMergeCurrentContent(context.Background(), chunks)
	require.Len(t, results, 2)
}

// TestGroupAndMergeOverlapping_CrossEntryPreservesMergeLogic verifies
// that chunks from different EntryIDs do NOT accidentally merge across
// entry boundaries even when they share StartAt/EndAt ranges.
func TestGroupAndMergeOverlapping_CrossEntryPreservesMergeLogic(t *testing.T) {
	plugin := &MergeStage{}
	chunks := []*types.SearchResult{
		{ID: "a-1", EntryID: "library-a", ChunkType: "text", StartAt: 0, EndAt: 100, Score: 0.9},
		{ID: "b-1", EntryID: "library-b", ChunkType: "text", StartAt: 0, EndAt: 100, Score: 0.5},
	}
	results := plugin.groupAndMergeCurrentContent(context.Background(), chunks)

	require.Len(t, results, 2, "chunks from different Libraries must not merge")
	assert.Equal(t, "a-1", results[0].ID, "higher score from library-a first")
	assert.Equal(t, "b-1", results[1].ID, "lower score from library-b second")
}
