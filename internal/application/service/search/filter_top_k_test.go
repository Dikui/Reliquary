package search

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestFilterTopKStageSortsMergeResultsBeforeTruncation(t *testing.T) {
	state := &State{
		Request: Request{RerankTopK: 3},
		Results: Results{
			MergeResult: []*types.SearchResult{
				{ID: "low", EntryID: "doc-c", Score: 0.2},
				{ID: "high", EntryID: "doc-a", Score: 0.9},
				{ID: "medium", EntryID: "doc-b", Score: 0.5},
				{ID: "second", EntryID: "doc-d", Score: 0.8},
			},
		},
	}

	plugin := &FilterTopKStage{}
	err := plugin.Run(
		context.Background(),
		state,
		func() *StageError { return nil },
	)

	require.Nil(t, err)
	require.Len(t, state.MergeResult, 3)
	assert.Equal(t, []string{"high", "second", "medium"}, searchResultIDs(state.MergeResult))
}

func TestFilterTopKStageUsesDeterministicTieBreakers(t *testing.T) {
	state := &State{
		Request: Request{RerankTopK: 10},
		Results: Results{
			MergeResult: []*types.SearchResult{
				{ID: "chunk-b", EntryID: "doc-b", ChunkType: "text", StartAt: 10, EndAt: 20, Score: 0.8},
				{ID: "chunk-c", EntryID: "doc-a", ChunkType: "summary", StartAt: 0, EndAt: 10, Score: 0.8},
				{ID: "chunk-a", EntryID: "doc-a", ChunkType: "text", StartAt: 0, EndAt: 10, Score: 0.8},
			},
		},
	}

	plugin := &FilterTopKStage{}
	err := plugin.Run(
		context.Background(),
		state,
		func() *StageError { return nil },
	)

	require.Nil(t, err)
	assert.Equal(t, []string{"chunk-c", "chunk-a", "chunk-b"}, searchResultIDs(state.MergeResult))
}

func searchResultIDs(results []*types.SearchResult) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.ID)
	}
	return ids
}
