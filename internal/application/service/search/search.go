package search

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"reliquary/internal/logger"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// SearchStage implements search functionality for entry retrieval
type SearchStage struct {
	libraryService interfaces.LibraryService
	entryService   interfaces.EntryService
	chunkService   interfaces.ChunkService
}

func NewSearchStage(
	libraryService interfaces.LibraryService,
	entryService interfaces.EntryService,
	chunkService interfaces.ChunkService,
) *SearchStage {
	res := &SearchStage{
		libraryService: libraryService,
		entryService:   entryService,
		chunkService:   chunkService,
	}
	return res
}

// Run searches the selected libraries and articles.
func (p *SearchStage) Run(ctx context.Context,
	state *State, next func() *StageError,
) *StageError {
	// Check if we have entry retrieval targets
	hasLibraryTargets := types.HasEntryRetrievalScope(
		state.SearchTargets,
		state.LibraryIDs,
		state.EntryIDs,
	)
	if !hasLibraryTargets {
		pipelineError(ctx, "Search", "library_not_found", map[string]interface{}{})
		return nil
	}

	pipelineInfo(ctx, "Search", "input", map[string]interface{}{
		"rewrite_query":  state.RewriteQuery,
		"search_targets": len(state.SearchTargets),
		"space_id":       state.SpaceID,
	})

	// Search the configured entry targets
	pipelineInfo(ctx, "Search", "plan", map[string]interface{}{
		"search_targets":    len(state.SearchTargets),
		"embedding_top_k":   state.EmbeddingTopK,
		"vector_threshold":  state.VectorThreshold,
		"keyword_threshold": state.KeywordThreshold,
	})
	allResults, librarySearchErr := p.searchByTargets(ctx, state)
	if librarySearchErr != nil && len(allResults) == 0 {
		pipelineError(ctx, "Search", "library_search_failed", map[string]interface{}{
			"error": librarySearchErr.Error(),
		})
		return ErrSearch.WithError(librarySearchErr)
	}
	if librarySearchErr != nil {
		pipelineWarn(ctx, "Search", "library_search_partial_failure", map[string]interface{}{
			"error":        librarySearchErr.Error(),
			"result_count": len(allResults),
		})
	}

	state.SearchResult = allResults

	logSearchScoreSample(ctx, "result_score_before_normalize", state.SearchResult)

	logSearchScoreSample(ctx, "final_score", state.SearchResult)

	// Return if we have results
	if len(state.SearchResult) != 0 {
		pipelineInfo(ctx, "Search", "output", map[string]interface{}{
			"result_count": len(state.SearchResult),
		})
		return next()
	}
	pipelineWarn(ctx, "Search", "output", map[string]interface{}{
		"result_count": 0,
	})
	return ErrSearchNothing
}

func removeDuplicateResults(results []*types.SearchResult) []*types.SearchResult {
	seen := make(map[string]bool)
	contentSig := make(map[string]string) // sig -> first chunk ID
	var uniqueResults []*types.SearchResult
	for _, r := range results {
		// Only deduplicate by exact chunk ID — do NOT treat shared ParentChunkID
		// as duplicates, because different child chunks of the same parent carry
		// different content segments that may all be relevant.
		if seen[r.ID] {
			logger.Debugf(context.Background(), "Dedup: chunk %s removed due to duplicate ID", r.ID)
			continue
		}
		sig := buildContentSignature(r.Content)
		if sig != "" {
			if firstChunk, exists := contentSig[sig]; exists {
				logger.Debugf(context.Background(), "Dedup: chunk %s removed due to content signature (dup of %s, sig prefix: %.50s...)", r.ID, firstChunk, sig)
				continue
			}
			contentSig[sig] = r.ID
		}
		seen[r.ID] = true
		uniqueResults = append(uniqueResults, r)
	}
	return uniqueResults
}

func buildContentSignature(content string) string {
	return searchutil.BuildContentSignature(content)
}

// removePartialOverlaps drops chunks whose content is largely contained within
// a higher-scored chunk, even across different entry sources. This catches
// cross-Library duplicates and near-duplicates that exact-signature dedup misses.
//
// Two thresholds are used:
//   - Substring containment: if the normalized short text is a literal substring
//     of the normalized long text, the shorter chunk is removed.
//   - Token overlap coefficient >= 0.85: if 85%+ of the smaller chunk's tokens
//     appear in the larger chunk, the smaller one is redundant.
//
// The input slice MUST already be deduplicated by ID/signature. Within each
// pair the chunk with the lower score is the candidate for removal; ties are
// broken by content length (longer wins).
func removePartialOverlaps(ctx context.Context, results []*types.SearchResult) []*types.SearchResult {
	const overlapThreshold = 0.85

	if len(results) <= 1 {
		return results
	}

	type normEntry struct {
		norm   string
		result *types.SearchResult
	}

	entries := make([]normEntry, 0, len(results))
	for _, r := range results {
		entries = append(entries, normEntry{
			norm:   searchutil.NormalizeContent(r.Content),
			result: r,
		})
	}

	removed := make(map[int]bool)

	for i := 0; i < len(entries); i++ {
		if removed[i] {
			continue
		}
		for j := i + 1; j < len(entries); j++ {
			if removed[j] {
				continue
			}

			a, b := entries[i], entries[j]

			shortIdx, longIdx := i, j
			if len(a.norm) > len(b.norm) {
				shortIdx, longIdx = j, i
			}

			contained := searchutil.IsContentContained(
				entries[shortIdx].norm, entries[longIdx].norm,
			)

			if !contained {
				ratio := searchutil.ContentOverlapRatio(
					entries[shortIdx].result.Content,
					entries[longIdx].result.Content,
				)
				if ratio < overlapThreshold {
					continue
				}
			}

			victim := shortIdx
			if entries[shortIdx].result.Score > entries[longIdx].result.Score {
				victim = longIdx
			}
			removed[victim] = true

			keptIdx := i
			if victim == i {
				keptIdx = j
			}
			pipelineInfo(ctx, "Merge", "partial_overlap_drop", map[string]interface{}{
				"kept_id":    entries[keptIdx].result.ID,
				"dropped_id": entries[victim].result.ID,
				"contained":  contained,
			})
		}
	}

	out := make([]*types.SearchResult, 0, len(results)-len(removed))
	for i, e := range entries {
		if !removed[i] {
			out = append(out, e.result)
		}
	}
	return out
}

func logSearchScoreSample(ctx context.Context, action string, results []*types.SearchResult) {
	const maxLogRows = 8
	limit := min(maxLogRows, len(results))
	for i := 0; i < limit; i++ {
		r := results[i]
		pipelineInfo(ctx, "Search", action, map[string]interface{}{
			"index":      i,
			"chunk_id":   r.ID,
			"score":      fmt.Sprintf("%.4f", r.Score),
			"match_type": r.MatchType,
		})
	}
	if len(results) > limit {
		pipelineInfo(ctx, "Search", action+"_summary", map[string]interface{}{
			"total":     len(results),
			"logged":    limit,
			"truncated": len(results) - limit,
		})
	}
}

// targetReportsEmbedFailure reports whether an embedding failure for a Library should
// be recorded as a retrieval error. Libraries with disabled indexing have no vector or keyword
// index to degrade into; HybridSearch returns empty without error. When Library metadata
// is unavailable, callers still attempt keyword-degraded search.
func targetReportsEmbedFailure(library *types.Library) bool {
	if library == nil {
		return false
	}

	if library.IsKeywordEnabled() {
		return false
	}
	return library.IsVectorEnabled()
}

// searchByTargets performs Library searches using pre-computed SearchTargets.
// Targets sharing the same underlying embedding model (identified by model
// name + endpoint, not just model ID) are grouped so the query embedding is
// computed once per model AND all full-Library targets in a group are combined into
// a single retrieval call, reducing both embedding API calls and DB round-trips.
func (p *SearchStage) searchByTargets(
	ctx context.Context,
	state *State,
) ([]*types.SearchResult, error) {
	if len(state.SearchTargets) == 0 {
		return nil, nil
	}

	queryText := strings.TrimSpace(state.RewriteQuery)

	// Batch-fetch Library records to determine embedding model grouping.
	// On failure, all targets fall into an empty-key group and HybridSearch
	// computes the embedding per-Library (graceful degradation).
	libraryIDs := make([]string, 0, len(state.SearchTargets))
	for _, t := range state.SearchTargets {
		libraryIDs = append(libraryIDs, t.LibraryID)
	}
	var libraryList []*types.Library
	libraryMap := make(map[string]*types.Library)
	if libraries, err := p.libraryService.GetLibrariesByIDsOnly(ctx, libraryIDs); err == nil {
		libraryList = libraries
		for _, library := range libraries {
			if library != nil {
				libraryMap[library.ID] = library
			}
		}
	} else {
		pipelineWarn(ctx, "Search", "batch_library_fetch_error", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Resolve actual model identities (name + endpoint) so that cross-space
	// Libraries backed by the same physical model share one embedding computation.
	modelKeyMap := p.libraryService.ResolveEmbeddingModelKeys(ctx, libraryList)

	groups := make(map[string][]*types.SearchTarget)
	for _, t := range state.SearchTargets {
		key := modelKeyMap[t.LibraryID] // empty string if unresolved
		groups[key] = append(groups[key], t)
	}

	pipelineInfo(ctx, "Search", "embedding_groups", map[string]interface{}{
		"total_targets": len(state.SearchTargets),
		"unique_models": len(groups),
	})

	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []*types.SearchResult
	var firstErr error
	var errOnce sync.Once
	recordError := func(err error) {
		if err != nil {
			errOnce.Do(func() { firstErr = err })
		}
	}

	for modelKey, targets := range groups {
		wg.Add(1)
		go func(modelKey string, targets []*types.SearchTarget) {
			defer wg.Done()

			// Compute embedding once for this model group. When that fails, retain
			// only targets that have a real keyword index; vector-only targets must
			// propagate the embedding failure instead of looking like empty recall.
			var queryEmbedding []float32
			disableVector := false
			searchableTargets := targets
			if modelKey != "" {
				emb, err := p.libraryService.GetQueryEmbedding(ctx, targets[0].LibraryID, queryText)
				if err != nil {
					searchableTargets = make([]*types.SearchTarget, 0, len(targets))
					for _, target := range targets {
						library := libraryMap[target.LibraryID]
						if !targetReportsEmbedFailure(library) {
							searchableTargets = append(searchableTargets, target)
							continue
						}
						recordError(fmt.Errorf("library %s has no keyword fallback: %w", target.LibraryID, err))
					}
					pipelineWarn(ctx, "Search", "group_embed_degrade_keyword", map[string]interface{}{
						"model_key":        modelKey,
						"library_id":       targets[0].LibraryID,
						"error":            err.Error(),
						"fallback_targets": len(searchableTargets),
						"failed_targets":   len(targets) - len(searchableTargets),
					})
					disableVector = true
				} else {
					queryEmbedding = emb
				}
			}

			// Separate full-Library targets (can be combined into one retrieval)
			// from specific-entry targets (need per-target direct loading).
			var fullLibraryIDs []string
			var entryTargets []*types.SearchTarget
			for _, t := range searchableTargets {
				if t.Type == types.SearchTargetTypeLibrary {
					fullLibraryIDs = append(fullLibraryIDs, t.LibraryID)
				} else {
					entryTargets = append(entryTargets, t)
				}
			}

			pipelineInfo(ctx, "Search", "group_plan", map[string]interface{}{
				"model_key":              modelKey,
				"combined_library_count": len(fullLibraryIDs),
				"individual_targets":     len(entryTargets),
				"vector_len":             len(queryEmbedding),
			})

			var innerWg sync.WaitGroup

			// Combined search: one HybridSearch call spanning all full-Library targets
			if len(fullLibraryIDs) > 0 {
				innerWg.Add(1)
				go func() {
					defer innerWg.Done()

					params := types.SearchParams{
						QueryText:             queryText,
						QueryEmbedding:        queryEmbedding,
						VectorThreshold:       state.VectorThreshold,
						KeywordThreshold:      state.KeywordThreshold,
						MatchCount:            state.EmbeddingTopK,
						SkipContextEnrichment: true,
						DisableVectorMatch:    disableVector,
					}
					res, err := p.libraryService.HybridSearch(ctx, fullLibraryIDs[0], params)
					if err != nil {
						pipelineWarn(ctx, "Search", "combined_library_search_error", map[string]interface{}{
							"library_ids": fullLibraryIDs,
							"error":       err.Error(),
						})
						recordError(err)
						return
					}
					pipelineInfo(ctx, "Search", "combined_library_result", map[string]interface{}{
						"library_ids": fullLibraryIDs,
						"hit_count":   len(res),
					})
					mu.Lock()
					results = append(results, res...)
					mu.Unlock()
				}()
			}

			// Individual search: per-target handling for specific-entry targets
			for _, target := range entryTargets {
				innerWg.Add(1)
				go func(t *types.SearchTarget) {
					defer innerWg.Done()
					recordError(p.searchSingleTarget(
						ctx, state, t, queryText, queryEmbedding, disableVector, &mu, &results,
					))
				}(target)
			}

			innerWg.Wait()
		}(modelKey, targets)
	}

	wg.Wait()

	pipelineInfo(ctx, "Search", "library_result_summary", map[string]interface{}{
		"total_hits": len(results),
	})
	return results, firstErr
}

// searchSingleTarget performs hybrid retrieval inside one constrained target.
func (p *SearchStage) searchSingleTarget(
	ctx context.Context,
	state *State,
	t *types.SearchTarget,
	queryText string,
	queryEmbedding []float32,
	disableVector bool,
	mu *sync.Mutex,
	results *[]*types.SearchResult,
) error {
	if t.Type == types.SearchTargetTypeEntry && len(t.EntryIDs) == 0 {
		return nil
	}

	vectorThreshold, keywordThreshold := t.RecallThresholds(
		state.VectorThreshold,
		state.KeywordThreshold,
	)
	if t.DisableRecallThresholds {
		pipelineInfo(ctx, "Search", "explicit_scope_threshold_override", map[string]interface{}{
			"library_id":     t.LibraryID,
			"entry_id_count": len(t.EntryIDs),
		})
	}
	params := types.SearchParams{
		QueryText:             queryText,
		QueryEmbedding:        queryEmbedding,
		VectorThreshold:       vectorThreshold,
		KeywordThreshold:      keywordThreshold,
		MatchCount:            state.EmbeddingTopK,
		SkipContextEnrichment: true,
		DisableVectorMatch:    disableVector,
	}
	if t.Type == types.SearchTargetTypeEntry {
		params.EntryIDs = t.EntryIDs
	}
	res, err := p.libraryService.HybridSearch(ctx, t.LibraryID, params)
	if err != nil {
		pipelineWarn(ctx, "Search", "library_search_error", map[string]interface{}{
			"library_id":  t.LibraryID,
			"target_type": t.Type,
			"query":       params.QueryText,
			"error":       err.Error(),
		})
		return err
	}
	pipelineInfo(ctx, "Search", "library_result", map[string]interface{}{
		"library_id":  t.LibraryID,
		"target_type": t.Type,
		"hit_count":   len(res),
	})
	mu.Lock()
	*results = append(*results, res...)
	mu.Unlock()
	return nil
}
