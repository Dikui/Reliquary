package knowledgesearch

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

// SearchStage implements search functionality for knowledge retrieval
type SearchStage struct {
	knowledgeBaseService interfaces.KnowledgeBaseService
	knowledgeService     interfaces.KnowledgeService
	chunkService         interfaces.ChunkService
}

func NewSearchStage(
	knowledgeBaseService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	chunkService interfaces.ChunkService,
) *SearchStage {
	res := &SearchStage{
		knowledgeBaseService: knowledgeBaseService,
		knowledgeService:     knowledgeService,
		chunkService:         chunkService,
	}
	return res
}

// Run searches the selected knowledge bases and articles.
func (p *SearchStage) Run(ctx context.Context,
	state *State, next func() *StageError,
) *StageError {
	// Check if we have knowledge retrieval targets
	hasKBTargets := types.HasKnowledgeRetrievalScope(
		state.SearchTargets,
		state.KnowledgeBaseIDs,
		state.KnowledgeIDs,
	)
	if !hasKBTargets {
		pipelineError(ctx, "Search", "kb_not_found", map[string]interface{}{})
		return nil
	}

	pipelineInfo(ctx, "Search", "input", map[string]interface{}{
		"rewrite_query":  state.RewriteQuery,
		"search_targets": len(state.SearchTargets),
		"tenant_id":      state.TenantID,
	})

	// Search the configured knowledge targets
	pipelineInfo(ctx, "Search", "plan", map[string]interface{}{
		"search_targets":    len(state.SearchTargets),
		"embedding_top_k":   state.EmbeddingTopK,
		"vector_threshold":  state.VectorThreshold,
		"keyword_threshold": state.KeywordThreshold,
	})
	allResults, kbSearchErr := p.searchByTargets(ctx, state)
	if kbSearchErr != nil && len(allResults) == 0 {
		pipelineError(ctx, "Search", "kb_search_failed", map[string]interface{}{
			"error": kbSearchErr.Error(),
		})
		return ErrSearch.WithError(kbSearchErr)
	}
	if kbSearchErr != nil {
		pipelineWarn(ctx, "Search", "kb_search_partial_failure", map[string]interface{}{
			"error":        kbSearchErr.Error(),
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
// a higher-scored chunk, even across different knowledge sources. This catches
// cross-KB duplicates and near-duplicates that exact-signature dedup misses.
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

// targetReportsEmbedFailure reports whether an embedding failure for a KB should
// be recorded as a retrieval error. KBs with disabled indexing have no vector or keyword
// index to degrade into; HybridSearch returns empty without error. When KB metadata
// is unavailable, callers still attempt keyword-degraded search.
func targetReportsEmbedFailure(kb *types.KnowledgeBase) bool {
	if kb == nil {
		return false
	}

	if kb.IsKeywordEnabled() {
		return false
	}
	return kb.IsVectorEnabled()
}

// searchByTargets performs KB searches using pre-computed SearchTargets.
// Targets sharing the same underlying embedding model (identified by model
// name + endpoint, not just model ID) are grouped so the query embedding is
// computed once per model AND all full-KB targets in a group are combined into
// a single retrieval call, reducing both embedding API calls and DB round-trips.
func (p *SearchStage) searchByTargets(
	ctx context.Context,
	state *State,
) ([]*types.SearchResult, error) {
	if len(state.SearchTargets) == 0 {
		return nil, nil
	}

	queryText := strings.TrimSpace(state.RewriteQuery)

	// Batch-fetch KB records to determine embedding model grouping.
	// On failure, all targets fall into an empty-key group and HybridSearch
	// computes the embedding per-KB (graceful degradation).
	kbIDs := make([]string, 0, len(state.SearchTargets))
	for _, t := range state.SearchTargets {
		kbIDs = append(kbIDs, t.KnowledgeBaseID)
	}
	var kbList []*types.KnowledgeBase
	kbMap := make(map[string]*types.KnowledgeBase)
	if kbs, err := p.knowledgeBaseService.GetKnowledgeBasesByIDsOnly(ctx, kbIDs); err == nil {
		kbList = kbs
		for _, kb := range kbs {
			if kb != nil {
				kbMap[kb.ID] = kb
			}
		}
	} else {
		pipelineWarn(ctx, "Search", "batch_kb_fetch_error", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Resolve actual model identities (name + endpoint) so that cross-tenant
	// KBs backed by the same physical model share one embedding computation.
	modelKeyMap := p.knowledgeBaseService.ResolveEmbeddingModelKeys(ctx, kbList)

	groups := make(map[string][]*types.SearchTarget)
	for _, t := range state.SearchTargets {
		key := modelKeyMap[t.KnowledgeBaseID] // empty string if unresolved
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
				emb, err := p.knowledgeBaseService.GetQueryEmbedding(ctx, targets[0].KnowledgeBaseID, queryText)
				if err != nil {
					searchableTargets = make([]*types.SearchTarget, 0, len(targets))
					for _, target := range targets {
						kb := kbMap[target.KnowledgeBaseID]
						if !targetReportsEmbedFailure(kb) {
							searchableTargets = append(searchableTargets, target)
							continue
						}
						recordError(fmt.Errorf("knowledge base %s has no keyword fallback: %w", target.KnowledgeBaseID, err))
					}
					pipelineWarn(ctx, "Search", "group_embed_degrade_keyword", map[string]interface{}{
						"model_key":        modelKey,
						"kb_id":            targets[0].KnowledgeBaseID,
						"error":            err.Error(),
						"fallback_targets": len(searchableTargets),
						"failed_targets":   len(targets) - len(searchableTargets),
					})
					disableVector = true
				} else {
					queryEmbedding = emb
				}
			}

			// Separate full-KB targets (can be combined into one retrieval)
			// from specific-knowledge targets (need per-target direct loading).
			var fullKBIDs []string
			var knowledgeTargets []*types.SearchTarget
			for _, t := range searchableTargets {
				if t.Type == types.SearchTargetTypeKnowledgeBase {
					fullKBIDs = append(fullKBIDs, t.KnowledgeBaseID)
				} else {
					knowledgeTargets = append(knowledgeTargets, t)
				}
			}

			pipelineInfo(ctx, "Search", "group_plan", map[string]interface{}{
				"model_key":          modelKey,
				"combined_kb_count":  len(fullKBIDs),
				"individual_targets": len(knowledgeTargets),
				"vector_len":         len(queryEmbedding),
			})

			var innerWg sync.WaitGroup

			// Combined search: one HybridSearch call spanning all full-KB targets
			if len(fullKBIDs) > 0 {
				innerWg.Add(1)
				go func() {
					defer innerWg.Done()

					params := types.SearchParams{
						QueryText:             queryText,
						QueryEmbedding:        queryEmbedding,
						KnowledgeBaseIDs:      fullKBIDs,
						VectorThreshold:       state.VectorThreshold,
						KeywordThreshold:      state.KeywordThreshold,
						MatchCount:            state.EmbeddingTopK,
						SkipContextEnrichment: true,
						DisableVectorMatch:    disableVector,
					}
					res, err := p.knowledgeBaseService.HybridSearch(ctx, fullKBIDs[0], params)
					if err != nil {
						pipelineWarn(ctx, "Search", "combined_kb_search_error", map[string]interface{}{
							"kb_ids": fullKBIDs,
							"error":  err.Error(),
						})
						recordError(err)
						return
					}
					pipelineInfo(ctx, "Search", "combined_kb_result", map[string]interface{}{
						"kb_ids":    fullKBIDs,
						"hit_count": len(res),
					})
					mu.Lock()
					results = append(results, res...)
					mu.Unlock()
				}()
			}

			// Individual search: per-target handling for specific-knowledge targets
			for _, target := range knowledgeTargets {
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

	pipelineInfo(ctx, "Search", "kb_result_summary", map[string]interface{}{
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
	if t.Type == types.SearchTargetTypeKnowledge && len(t.KnowledgeIDs) == 0 {
		return nil
	}

	vectorThreshold, keywordThreshold := t.RecallThresholds(
		state.VectorThreshold,
		state.KeywordThreshold,
	)
	if t.DisableRecallThresholds {
		pipelineInfo(ctx, "Search", "explicit_scope_threshold_override", map[string]interface{}{
			"kb_id":              t.KnowledgeBaseID,
			"knowledge_id_count": len(t.KnowledgeIDs),
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
	if t.Type == types.SearchTargetTypeKnowledge {
		params.KnowledgeIDs = t.KnowledgeIDs
	}
	res, err := p.knowledgeBaseService.HybridSearch(ctx, t.KnowledgeBaseID, params)
	if err != nil {
		pipelineWarn(ctx, "Search", "kb_search_error", map[string]interface{}{
			"kb_id":       t.KnowledgeBaseID,
			"target_type": t.Type,
			"query":       params.QueryText,
			"error":       err.Error(),
		})
		return err
	}
	pipelineInfo(ctx, "Search", "kb_result", map[string]interface{}{
		"kb_id":       t.KnowledgeBaseID,
		"target_type": t.Type,
		"hit_count":   len(res),
	})
	mu.Lock()
	*results = append(*results, res...)
	mu.Unlock()
	return nil
}
