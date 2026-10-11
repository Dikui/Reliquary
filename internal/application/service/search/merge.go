package search

import (
	"context"
	"sort"

	"reliquary/internal/searchutil"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// MergeStage handles merging of search result chunks
type MergeStage struct {
	chunkRepo    interfaces.ChunkRepository
	chunkService interfaces.ChunkService // for parent chunk resolution
}

// NewMergeStage creates a retrieval chunk merger.
func NewMergeStage(chunkRepo interfaces.ChunkRepository, chunkService interfaces.ChunkService) *MergeStage {
	res := &MergeStage{
		chunkRepo:    chunkRepo,
		chunkService: chunkService,
	}
	return res
}

func (p *MergeStage) Run(ctx context.Context,
	state *State, next func() *StageError,
) *StageError {
	pipelineInfo(ctx, "Merge", "input", map[string]interface{}{
		"candidate_cnt": len(state.RerankResult),
	})

	// Step 1: Select input
	searchResult := p.selectInputResults(ctx, state)

	// Step 2: Initial dedup
	searchResult = p.dedup(ctx, "dedup_summary", searchResult)

	pipelineInfo(ctx, "Merge", "candidate_ready", map[string]interface{}{
		"chunk_cnt": len(searchResult),
	})

	if len(searchResult) == 0 {
		pipelineWarn(ctx, "Merge", "output", map[string]interface{}{
			"chunk_cnt": 0,
			"reason":    "no_candidates",
		})
		return next()
	}

	// Step 4: Resolve parent chunks
	searchResult = p.resolveParentChunks(ctx, state, searchResult)

	// Step 5: Group by entry/chunkType and merge overlapping ranges
	mergedChunks := p.groupAndMergeCurrentContent(ctx, searchResult)

	// Step 7: Expand short contexts
	mergedChunks = p.expandShortContextWithNeighbors(ctx, state, mergedChunks)

	// Step 7.5: Re-merge overlapping ranges introduced by expansion
	mergedChunks = p.groupAndMergeCurrentContent(ctx, mergedChunks)

	// Step 8: Final dedup — catches exact duplicates plus partial content overlaps
	mergedChunks = p.dedup(ctx, "final_dedup", mergedChunks)
	mergedChunks = removePartialOverlaps(ctx, mergedChunks)

	state.MergeResult = mergedChunks
	return next()
}

// selectInputResults picks rerank results if available, falling back to search
// results sorted by score descending.
func (p *MergeStage) selectInputResults(ctx context.Context, state *State) []*types.SearchResult {
	if len(state.RerankResult) > 0 {
		return state.RerankResult
	}
	pipelineWarn(ctx, "Merge", "fallback", map[string]interface{}{
		"reason": "empty_rerank_result",
	})
	result := state.SearchResult
	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})
	return result
}

// dedup wraps removeDuplicateResults with before/after logging.
func (p *MergeStage) dedup(ctx context.Context, label string, results []*types.SearchResult) []*types.SearchResult {
	before := len(results)
	out := removeDuplicateResults(results)
	if len(out) < before {
		pipelineInfo(ctx, "Merge", label, map[string]interface{}{
			"before": before,
			"after":  len(out),
		})
	}
	return out
}

// groupAndMergeCurrentContent groups chunks by EntryID + ChunkType, then joins
// sequential current bodies without consulting source character offsets.
func (p *MergeStage) groupAndMergeCurrentContent(ctx context.Context, results []*types.SearchResult) []*types.SearchResult {
	// Group by EntryID → ChunkType
	entryGroup := make(map[string]map[string][]*types.SearchResult)
	for _, chunk := range results {
		if _, ok := entryGroup[chunk.EntryID]; !ok {
			entryGroup[chunk.EntryID] = make(map[string][]*types.SearchResult)
		}
		entryGroup[chunk.EntryID][chunk.ChunkType] = append(
			entryGroup[chunk.EntryID][chunk.ChunkType], chunk,
		)
	}

	pipelineInfo(ctx, "Merge", "group_summary", map[string]interface{}{
		"entry_cnt": len(entryGroup),
	})

	// Flatten into independent (entryID, chunks) work units for parallel merge.
	type mergeUnit struct {
		entryID string
		chunks  []*types.SearchResult
	}
	var units []mergeUnit
	for entryID, chunkGroup := range entryGroup {
		for _, chunks := range chunkGroup {
			units = append(units, mergeUnit{entryID: entryID, chunks: chunks})
		}
	}

	groupResults := ParallelMap(units, 0, func(_ int, u mergeUnit) []*types.SearchResult {
		pipelineInfo(ctx, "Merge", "group_process", map[string]interface{}{
			"entry_id":  u.entryID,
			"chunk_cnt": len(u.chunks),
		})

		sort.Slice(u.chunks, func(i, j int) bool {
			if u.chunks[i].ChunkIndex == u.chunks[j].ChunkIndex {
				return u.chunks[i].ID < u.chunks[j].ID
			}
			return u.chunks[i].ChunkIndex < u.chunks[j].ChunkIndex
		})

		grouped := p.mergeSequentialChunks(ctx, u.entryID, u.chunks)

		pipelineInfo(ctx, "Merge", "group_output", map[string]interface{}{
			"entry_id":      u.entryID,
			"merged_chunks": len(grouped),
		})
		return grouped
	})

	var mergedChunks []*types.SearchResult
	for _, g := range groupResults {
		mergedChunks = append(mergedChunks, g...)
	}

	// Global sort restores relevance order after map-based grouping.
	// n is typically < 100, O(n log n) is negligible here.
	sortSearchResultsDeterministically(mergedChunks)

	pipelineInfo(ctx, "Merge", "output", map[string]interface{}{
		"merged_total": len(mergedChunks),
	})
	return mergedChunks
}

// resolveParentChunks expands parent-child retrieval results with current
// parent_text context. Text children and image grandchildren share the same
// behavior, while image Markdown is scoped to the matched text child by
// durable URLs rather than parser coordinates.
func (p *MergeStage) resolveParentChunks(
	ctx context.Context,
	state *State,
	results []*types.SearchResult,
) []*types.SearchResult {
	if len(results) == 0 || p.chunkRepo == nil {
		return results
	}

	spaceID, _ := types.SpaceIDFromContext(ctx)
	if spaceID == 0 && state != nil {
		spaceID = state.SpaceID
	}
	if spaceID == 0 {
		pipelineWarn(ctx, "Merge", "parent_resolve_skip", map[string]interface{}{
			"reason": "missing_space",
		})
		return results
	}

	// Collect unique parent chunk IDs
	parentIDs := make(map[string]struct{})
	for _, r := range results {
		if r.ParentChunkID != "" {
			parentIDs[r.ParentChunkID] = struct{}{}
		}
	}

	if len(parentIDs) == 0 {
		return results
	}

	// Batch fetch parent chunks
	ids := make([]string, 0, len(parentIDs))
	for id := range parentIDs {
		ids = append(ids, id)
	}
	parentChunks, err := p.chunkRepo.ListChunksByID(ctx, spaceID, ids)
	if err != nil {
		pipelineWarn(ctx, "Merge", "parent_resolve_failed", map[string]interface{}{
			"error": err.Error(),
		})
		return results
	}

	parentMap := make(map[string]*types.Chunk, len(parentChunks))
	for _, c := range parentChunks {
		parentMap[c.ID] = c
	}

	// Batch-fetch image_info scoped to matched text children only.
	textChildIDs := collectScopedTextChildIDs(results, parentMap)
	var scopedImageInfo map[string]string
	if len(textChildIDs) > 0 {
		scopedImageInfo = searchutil.CollectImageInfoByChunkIDs(ctx, p.chunkRepo, spaceID, textChildIDs)
	}

	for _, r := range results {
		if r.ParentChunkID == "" {
			continue
		}

		switch r.ChunkType {
		case string(types.ChunkTypeText):
			// text → parent_text: expand to full parent for surrounding context
			// (the core parent-child value). Scope ImageInfo to this child only so
			// image-heavy parents do not inject every sibling page's OCR/Caption.
			parent, ok := parentMap[r.ParentChunkID]
			if !ok || parent.Content == "" || parent.ChunkType != types.ChunkTypeParentText {
				continue
			}
			pipelineInfo(ctx, "Merge", "parent_resolve", map[string]interface{}{
				"child_id":   r.ID,
				"parent_id":  r.ParentChunkID,
				"child_len":  runeLen(r.Content),
				"parent_len": runeLen(parent.Content),
				"scoped_img": true,
			})
			assignScopedImageInfo(r, scopedImageInfo, r.ID)
			parentContent := searchutil.PruneMarkdownImagesByImageInfo(parent.Content, r.ImageInfo)
			r.Content = searchutil.JoinChunkContent(parentContent, r.Content, "\n\n")
			r.ContentRewritten = true
			if !containsID(r.SubChunkID, r.ID) {
				r.SubChunkID = append(r.SubChunkID, r.ID)
			}

		}
	}

	return results
}

// collectScopedTextChildIDs returns text chunk IDs whose image_info should be
// loaded for parent-child merge scoping.
func collectScopedTextChildIDs(
	results []*types.SearchResult,
	parentMap map[string]*types.Chunk,
) []string {
	seen := make(map[string]struct{})
	var ids []string
	for _, r := range results {
		if r.ParentChunkID == "" {
			continue
		}
		switch r.ChunkType {
		case string(types.ChunkTypeText):
			parent := parentMap[r.ParentChunkID]
			if parent == nil || parent.ChunkType != types.ChunkTypeParentText {
				continue
			}
			if _, ok := seen[r.ID]; ok {
				continue
			}
			seen[r.ID] = struct{}{}
			ids = append(ids, r.ID)

		}
	}
	return ids
}

// assignScopedImageInfo sets ImageInfo from the per-text-child map, falling
// back to URLs present in the result content.
func assignScopedImageInfo(r *types.SearchResult, scoped map[string]string, textChildID string) {
	if scoped != nil {
		if info, ok := scoped[textChildID]; ok && info != "" {
			r.ImageInfo = info
			return
		}
	}
	if r.ImageInfo != "" {
		r.ImageInfo = searchutil.FilterImageInfoByContentURLs(r.Content, r.ImageInfo)
	}
}
