package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	search "reliquary/internal/application/service/search"
	apperrors "reliquary/internal/errors"
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
	f.request.Query = strings.TrimSpace(request.Query)
	if f.request.Query == "" {
		return f, apperrors.NewBadRequestError("query is required")
	}
	if request.Limit < 0 || request.Limit > 100 {
		return f, apperrors.NewBadRequestError("limit must be between 1 and 100, or omitted")
	}
	for _, contentType := range request.ContentTypes {
		if !slices.Contains(types.ContentTypes, contentType) {
			return f, apperrors.NewBadRequestError("unsupported content_types value: " + contentType)
		}
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
func (s *searchService) Search(ctx context.Context, request types.EntrySearchRequest) (*types.EntrySearchResponse, error) {
	f, err := normalizeEntrySearch(request)
	if err != nil {
		return nil, err
	}
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok {
		return nil, apperrors.NewUnauthorizedError("workspace ID not found in context")
	}
	kb, err := s.knowledgeBaseService.GetKnowledgeBase(ctx)
	if err != nil {
		return nil, err
	}
	if kb == nil || kb.SpaceID != spaceID {
		return nil, apperrors.NewForbiddenError("knowledge base is outside the space")
	}
	if s.db == nil {
		return nil, fmt.Errorf("article search requires PostgreSQL")
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
	limit := f.request.Limit
	if limit == 0 {
		limit = min(config.GetEffectiveRerankTopK(), 100)
	}
	pool := min(max(limit*5, config.GetEffectiveEmbeddingTopK()), maxRetrievalPoolSize)
	var queryVector []float32
	modelID := ""
	queryProfileHash := ""
	if kb.IsVectorEnabled() {
		modelID, err = ResolveModelTier(ctx, s.db, kb, types.ModelTierEmbedding)
		if err != nil {
			return nil, err
		}
		if modelID != "" {
			var metadata types.Model
			read := s.db.WithContext(ctx).Where("id=? AND (space_id=? OR is_builtin=true)", modelID, spaceID).Find(&metadata)
			if read.Error != nil {
				return nil, read.Error
			}
			if read.RowsAffected > 0 {
				queryProfileHash = types.EmbeddingProfileHash(modelID, metadata.Parameters.EmbeddingParameters.Dimension, &metadata)
			}
			queryVector, err = s.knowledgeBaseService.GetQueryEmbedding(ctx, kb.ID, f.request.Query)
			if err != nil {
				return nil, err
			}
		}
	}
	response := &types.EntrySearchResponse{Results: []*types.EntrySearchResult{}}
	var merged []*types.EntrySearchResult
	// Query generation happens before this short read transaction. Shared
	// metadata locks keep every recall channel in the query's model space.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(queryVector) > 0 {
			var currentKB types.KnowledgeBase
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND space_id=?", kb.ID, spaceID).First(&currentKB).Error; err != nil {
				return err
			}
			currentID, err := ResolveModelTier(ctx, tx, &currentKB, types.ModelTierEmbedding)
			if err != nil {
				return err
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
		reader.db = tx
		if f.from != nil || f.to != nil {
			var err error
			response.ExcludedMissingPublishedAt, err = reader.countMissingArticleDates(ctx, spaceID, kb, f, queryVector, modelID, config)
			if err != nil {
				return err
			}
		}
		lanes, err := reader.retrieveEntryLanes(ctx, spaceID, kb, f, queryVector, modelID, config, pool, true)
		if err != nil {
			return err
		}
		merged, err = reader.mergeEntryHits(ctx, spaceID, lanes, config)
		return err
	})
	if err != nil {
		return nil, err
	}
	merged, err = s.rerankEntries(ctx, kb, f.request.Query, merged, config, pool)
	if err != nil {
		return nil, err
	}
	if f.request.CollapseSimilar == nil || *f.request.CollapseSimilar {
		merged, err = s.collapseArticleReports(ctx, spaceID, merged, config)
		if err != nil {
			return nil, err
		}
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}
	response.Results = merged
	return response, nil
}

func (s *searchService) eligibleEntries(ctx context.Context, spaceID uint64, kbID string, f entrySearchFilters, dates bool) *gorm.DB {
	query := s.db.WithContext(ctx).Table("entries AS k").Where(`k.space_id = ? AND k.knowledge_base_id = ?
		AND k.deleted_at IS NULL AND k.parse_status = 'completed' AND k.enable_status = 'enabled'`, spaceID, kbID)
	if f.request.Starred != nil {
		query = query.Where("k.source_kind='article' AND k.starred=?", *f.request.Starred)
	}
	if len(f.request.ContentTypes) > 0 {
		query = query.Where("k.content_type IN ?", f.request.ContentTypes)
	}
	if len(f.request.Sources) > 0 {
		query = query.Where("k.source_kind IN ?", f.request.Sources)
	}
	if len(f.request.EntityIDs) > 0 {
		query = query.Where(`EXISTS (SELECT 1 FROM entry_entities ke WHERE ke.space_id=k.space_id
			AND ke.entry_id=k.id AND ke.entity_id IN ?)`, f.request.EntityIDs)
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

func (s *searchService) rerankEntries(ctx context.Context, kb *types.KnowledgeBase, query string, results []*types.EntrySearchResult, config *types.RetrievalConfig, pool int) ([]*types.EntrySearchResult, error) {
	if len(results) == 0 {
		return results, nil
	}
	modelID, err := ResolveModelTier(ctx, s.db, kb, types.ModelTierRerank)
	if err != nil || modelID == "" {
		return results, err
	}
	state := &search.State{Request: search.Request{
		Query: query, RerankModelID: modelID, RerankTopK: pool, RerankThreshold: config.GetEffectiveRerankThreshold(),
	}, Results: search.Results{RewriteQuery: query}}
	byID := make(map[string]*types.EntrySearchResult, len(results))
	for _, result := range results {
		text := result.Title + "\n" + result.Summary.RenderText()
		for _, snippet := range result.Snippets {
			text += "\n\n" + snippet.Content
		}
		state.SearchResult = append(state.SearchResult, &types.SearchResult{ID: result.EntryID, EntryID: result.EntryID, Content: text, Score: result.Score})
		byID[result.EntryID] = result
	}
	if err := s.rerank.Run(ctx, state, func() *search.StageError { return nil }); err != nil {
		if err == search.ErrSearchNothing {
			return []*types.EntrySearchResult{}, nil
		}
		return nil, err.Err
	}
	if len(state.RerankResult) == 0 {
		return results, nil
	}
	reranked := make([]*types.EntrySearchResult, 0, len(state.RerankResult))
	for _, hit := range state.RerankResult {
		if result := byID[hit.ID]; result != nil {
			result.Score = hit.Score
			reranked = append(reranked, result)
		}
	}
	// The existing stage applies diversity selection; article display uses its scores.
	slices.SortStableFunc(reranked, compareEntryScores)
	return reranked, nil
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
