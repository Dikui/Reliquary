package service

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/models/rerank"
	"reliquary/internal/types"
)

type entrySearchFilters struct {
	request types.EntrySearchRequest
	from    *time.Time
	to      *time.Time
	toDate  bool
}

func normalizeEntrySearch(request types.EntrySearchRequest) (entrySearchFilters, error) {
	f := entrySearchFilters{request: request}
	if len(request.GraphParameters) > 0 {
		return f, apperrors.NewBadRequestError("graph_parameters require a graph evaluation question")
	}
	if len(request.Libraries) == 0 || len(request.Libraries) > 100 {
		return f, apperrors.NewBadRequestError("libraries requires 1 to 100 library IDs or directories")
	}
	for _, library := range request.Libraries {
		if strings.TrimSpace(library) == "" {
			return f, apperrors.NewBadRequestError("libraries cannot contain an empty value")
		}
	}
	f.request.Query = strings.TrimSpace(request.Query)
	if f.request.Query == "" {
		return f, apperrors.NewBadRequestError("query is required")
	}
	if request.Limit < 0 || request.Limit > 100 {
		return f, apperrors.NewBadRequestError("limit must be between 1 and 100, or omitted")
	}
	for _, source := range request.Sources {
		if !slices.Contains([]string{"article", "mine", "report"}, source) {
			return f, apperrors.NewBadRequestError("unsupported sources value: " + source)
		}
	}
	for name, ids := range map[string][]string{"entity_ids": request.EntityIDs, "topic_ids": request.TopicIDs} {
		if len(ids) > 100 {
			return f, apperrors.NewBadRequestError(name + " accepts at most 100 IDs")
		}
		for _, id := range ids {
			if strings.TrimSpace(id) == "" {
				return f, apperrors.NewBadRequestError(name + " cannot contain an empty ID")
			}
		}
	}
	parse := func(name, value string) (*time.Time, bool, error) {
		if value == "" {
			return nil, false, nil
		}
		for _, layout := range []string{time.RFC3339Nano, time.DateOnly} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return &parsed, layout == time.DateOnly, nil
			}
		}
		return nil, false, apperrors.NewBadRequestError(name + " must be RFC3339 or YYYY-MM-DD")
	}
	var err error
	f.from, _, err = parse("published_from", request.PublishedFrom)
	if err != nil {
		return f, err
	}
	f.to, f.toDate, err = parse("published_to", request.PublishedTo)
	if err != nil {
		return f, err
	}
	if f.toDate {
		end := f.to.AddDate(0, 0, 1)
		f.to = &end
	}
	if f.from != nil && f.to != nil && (f.from.After(*f.to) || (f.toDate && f.from.Equal(*f.to))) {
		return f, apperrors.NewBadRequestError("published_from must not be after published_to")
	}
	return f, nil
}

// Search is the article retrieval entry point used by HTTP and external adapters.
func (s *searchService) Search(ctx context.Context, request types.EntrySearchRequest) (out *types.EntrySearchResponse, resultErr error) {
	started := time.Now()
	f, err := normalizeEntrySearch(request)
	if err != nil {
		return nil, err
	}
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok {
		return nil, apperrors.NewUnauthorizedError("workspace ID not found in context")
	}
	if s.db == nil {
		return nil, fmt.Errorf("article search requires PostgreSQL")
	}
	defer func() {
		if err := recordOrdinarySearch(ctx, s.db, request, out, started, resultErr); err != nil {
			resultErr = fmt.Errorf("persist search record: %w", err)
		}
	}()
	var selected []types.Library
	if err := s.db.WithContext(ctx).Where("space_id=? AND (id IN ? OR directory IN ?)", spaceID, request.Libraries, request.Libraries).Order("id").Find(&selected).Error; err != nil {
		return nil, err
	}
	ids := []string{}
	for _, requested := range request.Libraries {
		found := false
		for _, lib := range selected {
			if requested == lib.ID || requested == lib.Directory {
				found = true
			}
		}
		if !found {
			return nil, apperrors.NewBadRequestError("unknown library: " + requested)
		}
	}
	for _, lib := range selected {
		ids = append(ids, lib.ID)
	}
	f.request.Libraries = ids
	request.Libraries = ids
	keywordEnabled := false
	for _, lib := range selected {
		keywordEnabled = keywordEnabled || lib.IsKeywordEnabled()
	}
	library := &selected[0]
	for i := range selected {
		if selected[i].IsVectorEnabled() {
			library = &selected[i]
			break
		}
	}
	var config *types.RetrievalConfig
	if s.spaceService != nil {
		if space, readErr := s.spaceService.GetSpaceByID(ctx, spaceID); readErr == nil && space != nil {
			config = space.RetrievalConfig
		}
	}
	if config == nil {
		if space, _ := types.SpaceInfoFromContext(ctx); space != nil {
			config = space.RetrievalConfig
		}
	}
	execution, hasExecution := types.SearchExecution(ctx)
	if hasExecution && execution.RetrievalConfig != nil {
		config = execution.RetrievalConfig
	}
	set, err := resolveSearchVectorSet(ctx, s.db, spaceID, request.VectorSet, false)
	if err != nil {
		return nil, err
	}
	f.request.VectorSet = set.Selector
	limit := f.request.Limit
	if limit == 0 {
		limit = min(config.GetEffectiveRerankTopK(), 100)
	}
	pool := min(max(limit*5, config.GetEffectiveEmbeddingTopK()), maxRetrievalPoolSize)
	var queryVector []float32
	modelID := ""
	queryProfileHash := ""
	modelDimension := 0
	vectorUnavailable := false
	if library.IsVectorEnabled() {
		modelID, err = ResolveModelTier(ctx, s.db, library, types.ModelTierEmbedding)
		if err != nil {
			return nil, err
		}
		if set.ModelID != "" {
			modelID = set.ModelID
		}
		if execution.ExpectedEmbeddingModelID != "" && modelID != execution.ExpectedEmbeddingModelID {
			return nil, fmt.Errorf("embedding model changed during evaluation")
		}
		if modelID != "" {
			var metadata types.Model
			read := s.db.WithContext(ctx).Where("id=? AND (space_id=? OR is_builtin=true)", modelID, spaceID).Find(&metadata)
			if read.Error != nil {
				return nil, read.Error
			}
			if read.RowsAffected > 0 {
				queryProfileHash = types.EmbeddingProfileHash(modelID, metadata.Parameters.EmbeddingParameters.Dimension, &metadata)
				modelDimension = metadata.Parameters.EmbeddingParameters.Dimension
			}
			if execution.ExpectedEmbeddingProfileHash != "" && queryProfileHash != execution.ExpectedEmbeddingProfileHash {
				return nil, fmt.Errorf("embedding profile changed during evaluation")
			}
			queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if set.Selector == "active" {
				queryVector, err = s.libraryService.GetQueryEmbedding(queryCtx, library.ID, f.request.Query)
			} else if s.modelService != nil {
				model, modelErr := s.modelService.GetEmbeddingModel(queryCtx, modelID)
				if modelErr != nil {
					err = modelErr
				} else if model == nil {
					err = fmt.Errorf("embedding model unavailable")
				} else {
					queryVector, err = model.Embed(queryCtx, f.request.Query)
				}
			} else {
				err = fmt.Errorf("embedding model unavailable")
			}
			cancel()
			invalid := len(queryVector) == 0 || (metadata.Parameters.EmbeddingParameters.Dimension > 0 && len(queryVector) != metadata.Parameters.EmbeddingParameters.Dimension)
			norm := float64(0)
			for _, v := range queryVector {
				n := float64(v)
				invalid = invalid || math.IsNaN(n) || math.IsInf(n, 0)
				norm += n * n
			}
			if err != nil || invalid || norm == 0 {
				vectorUnavailable = true
				queryVector = nil
			}
		} else {
			vectorUnavailable = true
		}
	}
	response := &types.EntrySearchResponse{Results: []*types.EntrySearchResult{}, VectorUnavailable: vectorUnavailable, RerankStatus: "unconfigured"}
	var merged []*types.EntrySearchResult
	// Query generation happens before this short read transaction. Shared
	// metadata locks keep every recall channel in the query's model space.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(queryVector) > 0 {
			var lockedSpace types.Space
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=?", spaceID).First(&lockedSpace).Error; err != nil {
				return err
			}
			var currentLibrary types.Library
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND space_id=?", library.ID, spaceID).First(&currentLibrary).Error; err != nil {
				return err
			}
			currentID, err := ResolveModelTier(ctx, tx, &currentLibrary, types.ModelTierEmbedding)
			if err != nil {
				return err
			}
			if set.Selector != "active" {
				lockedSet, setErr := resolveSearchVectorSet(ctx, tx, spaceID, set.Selector, true)
				if setErr != nil {
					return setErr
				}
				currentID = lockedSet.ModelID
			}
			if currentID != modelID {
				return fmt.Errorf("embedding model selection changed during query generation")
			}
			if queryProfileHash != "" {
				var metadata types.Model
				if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND (space_id=? OR is_builtin=true)", modelID, spaceID).First(&metadata).Error; err != nil {
					return err
				}
				if types.EmbeddingProfileHash(modelID, metadata.Parameters.EmbeddingParameters.Dimension, &metadata) != queryProfileHash {
					return fmt.Errorf("embedding model profile changed during query generation")
				}
			}
		}
		reader := *s
		// Candidate queries share the validated library set in f.
		retrievalLibrary := *library
		retrievalLibrary.ID = ""
		retrievalLibrary.IndexingStrategy.KeywordEnabled = keywordEnabled || vectorUnavailable
		reader.db = tx
		if f.from != nil || f.to != nil {
			var err error
			response.ExcludedMissingPublishedAt, err = reader.countMissingArticleDates(ctx, spaceID, &retrievalLibrary, f, queryVector, modelID, config)
			if err != nil {
				return err
			}
		}
		lanes, err := reader.retrieveEntryLanes(ctx, spaceID, &retrievalLibrary, f, queryVector, modelID, config, pool, true)
		if err != nil {
			return err
		}
		merged, err = reader.mergeEntryHits(ctx, spaceID, lanes, config)
		return err
	})
	if err != nil {
		return nil, err
	}
	rerankID, resolveErr := ResolveModelTier(ctx, s.db, library, types.ModelTierRerank)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if hasExecution && execution.RerankModelID != nil {
		rerankID = *execution.RerankModelID
	}
	execution.RerankModelID = &rerankID
	ctx = types.WithSearchExecution(ctx, execution)
	merged, response.RerankStatus = s.rerankEntryResults(ctx, library, f.request.Query, merged)
	if f.request.CollapseSimilar == nil || *f.request.CollapseSimilar {
		set.ModelID = modelID
		merged, err = s.collapseArticleReports(ctx, spaceID, merged, config, set)
		if err != nil {
			return nil, err
		}
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}
	s.expandEntryParentContext(ctx, spaceID, selected, merged)
	response.Results = merged
	f.request.Limit = limit
	if f.request.CollapseSimilar == nil {
		enabled := true
		f.request.CollapseSimilar = &enabled
	}
	if set.Selector == "active" {
		set.ModelID = modelID
		set.ProfileHash = queryProfileHash
		set.Dimension = modelDimension
	}
	response.Snapshot = searchSnapshot(config, selected, set, f.request, rerankID)
	request = f.request
	return response, nil
}

func (s *searchService) eligibleEntries(ctx context.Context, spaceID uint64, libraryID string, f entrySearchFilters, dates bool) *gorm.DB {
	query := s.db.WithContext(ctx).Table("entries AS k").Where(`k.space_id = ?
		AND k.deleted_at IS NULL AND k.parse_status = 'completed' AND k.enable_status = 'enabled'`, spaceID)
	if len(f.request.Libraries) > 0 {
		query = query.Where("k.library_id IN ?", f.request.Libraries)
	}
	if libraryID != "" {
		query = query.Where("k.library_id=?", libraryID)
	}
	if f.request.Starred != nil {
		query = query.Where("k.source_kind IN ('article','mine') AND k.starred=?", *f.request.Starred)
	}
	if len(f.request.ContentTypes) > 0 {
		query = query.Where("k.content_type IN ?", f.request.ContentTypes)
	}
	if len(f.request.Sources) > 0 {
		query = query.Where("k.source_kind IN ?", f.request.Sources)
	}
	if len(f.request.EntityIDs) > 0 {
		if f.request.IncludeDescendants {
			query = query.Where(`EXISTS (SELECT 1 FROM entry_entities ke WHERE ke.space_id=k.space_id
				AND ke.entry_id=k.id AND ke.excluded=false AND (ke.entity_id IN ? OR ke.entity_id IN (
				WITH RECURSIVE descendants(id, library_id) AS (
					SELECT e.id, e.library_id FROM entities e JOIN libraries l ON l.id=e.library_id AND l.space_id=e.space_id
					WHERE e.space_id=? AND e.id IN ? AND e.library_id IN ? AND l.entity_mode='managed'
					AND e.archived_at IS NULL AND l.deleted_at IS NULL
					UNION
					SELECT child.id, child.library_id FROM descendants d
					JOIN relations r ON r.object_id=d.id AND r.library_id=d.library_id AND r.space_id=?
					JOIN library_relation_types rt ON rt.library_id=r.library_id AND rt.name=r.relation_type AND rt.enabled=true AND rt.hierarchical=true
					JOIN entities child ON child.id=r.subject_id AND child.library_id=d.library_id AND child.space_id=r.space_id AND child.archived_at IS NULL
				) SELECT id FROM descendants)))`, f.request.EntityIDs, spaceID, f.request.EntityIDs, f.request.Libraries, spaceID)
		} else {
			query = query.Where(`EXISTS (SELECT 1 FROM entry_entities ke WHERE ke.space_id=k.space_id
			AND ke.entry_id=k.id AND ke.excluded=false AND ke.entity_id IN ?)`, f.request.EntityIDs)
		}
	}
	if len(f.request.TopicIDs) > 0 {
		query = query.Where(`EXISTS (SELECT 1 FROM entry_topics kt WHERE kt.space_id=k.space_id
			AND kt.entry_id=k.id AND kt.topic_id IN ?)`, f.request.TopicIDs)
	}
	if dates {
		if f.from != nil || f.to != nil {
			query = query.Where("k.search_published_at IS NOT NULL")
		}
		if f.from != nil {
			query = query.Where("k.search_published_at >= ?", *f.from)
		}
		if f.to != nil {
			op := " <= ?"
			if f.toDate {
				op = " < ?"
			}
			query = query.Where("k.search_published_at"+op, *f.to)
		}
	}
	return query
}

// rerankEntries retains the internal compatibility signature.
func (s *searchService) rerankEntries(ctx context.Context, library *types.Library, query string, results []*types.EntrySearchResult, _ *types.RetrievalConfig, _ int) ([]*types.EntrySearchResult, error) {
	out, _ := s.rerankEntryResults(ctx, library, query, results)
	return out, nil
}
func (s *searchService) rerankEntryResults(ctx context.Context, library *types.Library, query string, results []*types.EntrySearchResult) ([]*types.EntrySearchResult, string) {
	modelID, err := ResolveModelTier(ctx, s.db, library, types.ModelTierRerank)
	if err != nil {
		return results, "unavailable"
	}
	if execution, ok := types.SearchExecution(ctx); ok && execution.RerankModelID != nil {
		modelID = *execution.RerankModelID
	}
	if modelID == "" {
		return results, "unconfigured"
	}
	if len(results) == 0 {
		return results, "no_candidates"
	}
	if s.modelService == nil {
		return results, "unavailable"
	}
	hasImages := slices.ContainsFunc(results, func(result *types.EntrySearchResult) bool {
		return slices.ContainsFunc(result.Snippets, func(snippet types.EntrySearchSnippet) bool { return snippet.ImagePath != "" })
	})
	timeout := 5 * time.Second
	if hasImages {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	model, err := s.modelService.GetRerankModel(ctx, modelID)
	if err != nil || model == nil {
		return results, "unavailable"
	}
	candidates := results
	if hasImages {
		// Scores are comparable only within one request. Rerank a complete
		// prefix within the mixed-input limit; retain the fused tail order.
		items, end := 0, 0
		for end < len(results) {
			count := 1
			if slices.ContainsFunc(results[end].Snippets, func(snippet types.EntrySearchSnippet) bool { return snippet.ImagePath != "" }) {
				count++
			}
			if items+count > maxMultimodalRerankDocuments {
				break
			}
			items += count
			end++
		}
		candidates = results[:end]
	}
	documents := make([]string, len(candidates))
	for i, result := range candidates {
		documents[i] = result.Title + "\n" + result.Summary.RenderText()
		for _, snippet := range result.Snippets {
			documents[i] += "\n\n" + snippet.Content
		}
	}
	var ranks []rerank.RankResult
	if hasImages {
		images, ok := model.(rerank.MultimodalReranker)
		if !ok || !images.SupportsImages() {
			return results, "unavailable"
		}
		ranks, err = s.rerankImageEntryResults(ctx, images, query, candidates, documents)
	} else {
		ranks, err = model.Rerank(ctx, query, documents)
	}
	if err != nil || len(ranks) != len(candidates) {
		return results, "unavailable"
	}
	seen := map[int]bool{}
	for _, rank := range ranks {
		if rank.Index < 0 || rank.Index >= len(candidates) || seen[rank.Index] || math.IsNaN(rank.RelevanceScore) || math.IsInf(rank.RelevanceScore, 0) {
			return results, "unavailable"
		}
		seen[rank.Index] = true
	}
	for _, rank := range ranks {
		candidates[rank.Index].Score = rank.RelevanceScore
	}
	slices.SortStableFunc(candidates, compareEntryScores)
	return results, "applied"
}

func compareEntryScores(a, b *types.EntrySearchResult) int {
	if a.Score > b.Score {
		return -1
	}
	if a.Score < b.Score {
		return 1
	}
	return strings.Compare(a.EntryID, b.EntryID)
}
