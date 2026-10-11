package knowledgesearch

import (
	"context"
	"sort"

	"reliquary/internal/types"
)

// FilterTopKStage filters search results to keep only the top K items.
type FilterTopKStage struct{}

// NewFilterTopKStage creates a retrieval result filter.
func NewFilterTopKStage() *FilterTopKStage {
	res := &FilterTopKStage{}
	return res
}

// Run filters results to keep only the top K items.
// It can filter MergeResult, RerankResult, or SearchResult depending on which is available
func (p *FilterTopKStage) Run(ctx context.Context,
	state *State, next func() *StageError,
) *StageError {
	pipelineInfo(ctx, "FilterTopK", "input", map[string]interface{}{
		"top_k":      state.RerankTopK,
		"merge_cnt":  len(state.MergeResult),
		"rerank_cnt": len(state.RerankResult),
		"search_cnt": len(state.SearchResult),
	})

	filterTopK := func(searchResult []*types.SearchResult, topK int) []*types.SearchResult {
		sortSearchResultsDeterministically(searchResult)
		if topK > 0 && len(searchResult) > topK {
			pipelineInfo(ctx, "FilterTopK", "filter", map[string]interface{}{
				"before": len(searchResult),
				"after":  topK,
			})
			searchResult = searchResult[:topK]
		}
		return searchResult
	}

	if len(state.MergeResult) > 0 {
		state.MergeResult = filterTopK(state.MergeResult, state.RerankTopK)
	} else if len(state.RerankResult) > 0 {
		state.RerankResult = filterTopK(state.RerankResult, state.RerankTopK)
	} else if len(state.SearchResult) > 0 {
		state.SearchResult = filterTopK(state.SearchResult, state.RerankTopK)
	} else {
		pipelineWarn(ctx, "FilterTopK", "skip", map[string]interface{}{
			"reason": "no_results",
		})
	}

	pipelineInfo(ctx, "FilterTopK", "output", map[string]interface{}{
		"merge_cnt":  len(state.MergeResult),
		"rerank_cnt": len(state.RerankResult),
		"search_cnt": len(state.SearchResult),
	})
	return next()
}

// sortSearchResultsDeterministically restores the global relevance order after
// merge stages group results through maps. Stable tie-breakers keep identical
// requests reproducible before TopK truncation.
func sortSearchResultsDeterministically(results []*types.SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		left, right := results[i], results[j]
		if left == nil || right == nil {
			return left != nil
		}
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		if left.KnowledgeID != right.KnowledgeID {
			return left.KnowledgeID < right.KnowledgeID
		}
		if left.ChunkType != right.ChunkType {
			return left.ChunkType < right.ChunkType
		}
		if left.ChunkIndex != right.ChunkIndex {
			return left.ChunkIndex < right.ChunkIndex
		}
		return left.ID < right.ID
	})
}
