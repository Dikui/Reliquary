package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	knowledgesearch "reliquary/internal/application/service/knowledge_search"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type articleSearchFilters struct {
	request types.ArticleSearchRequest
	from    *time.Time
	to      *time.Time
	toDate  bool
}

func normalizeArticleSearch(request types.ArticleSearchRequest) (articleSearchFilters, error) {
	f := articleSearchFilters{request: request}
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
func (s *knowledgeSearchService) Search(ctx context.Context, request types.ArticleSearchRequest) (*types.ArticleSearchResponse, error) {
	f, err := normalizeArticleSearch(request)
	if err != nil {
		return nil, err
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return nil, apperrors.NewUnauthorizedError("workspace ID not found in context")
	}
	kb, err := s.knowledgeBaseService.GetKnowledgeBase(ctx)
	if err != nil {
		return nil, err
	}
	if kb == nil || kb.TenantID != tenantID {
		return nil, apperrors.NewForbiddenError("knowledge base is outside the space")
	}
	if s.db == nil {
		return nil, fmt.Errorf("article search requires PostgreSQL")
	}
	var config *types.RetrievalConfig
	if s.tenantService != nil {
		if tenant, readErr := s.tenantService.GetTenantByID(ctx, tenantID); readErr == nil && tenant != nil {
			config = tenant.RetrievalConfig
		}
	}
	if config == nil {
		if tenant, _ := types.TenantInfoFromContext(ctx); tenant != nil {
			config = tenant.RetrievalConfig
		}
	}
	limit := f.request.Limit
	if limit == 0 {
		limit = min(config.GetEffectiveRerankTopK(), 100)
	}
	pool := min(max(limit*5, config.GetEffectiveEmbeddingTopK()), maxRetrievalPoolSize)
	var queryVector []float32
	modelID := ""
	if kb.IsVectorEnabled() {
		modelID, err = ResolveModelTier(ctx, s.db, kb, types.ModelTierEmbedding)
		if err != nil {
			return nil, err
		}
		if modelID != "" {
			queryVector, err = s.knowledgeBaseService.GetQueryEmbedding(ctx, kb.ID, f.request.Query)
			if err != nil {
				return nil, err
			}
		}
	}
	response := &types.ArticleSearchResponse{Results: []*types.ArticleSearchResult{}}
	if f.from != nil || f.to != nil {
		response.ExcludedMissingPublishedAt, err = s.countMissingArticleDates(ctx, tenantID, kb, f, queryVector, modelID, config)
		if err != nil {
			return nil, err
		}
	}
	lanes, err := s.retrieveArticleLanes(ctx, tenantID, kb, f, queryVector, modelID, config, pool, true)
	if err != nil {
		return nil, err
	}
	merged, err := s.mergeArticleHits(ctx, tenantID, lanes, config)
	if err != nil {
		return nil, err
	}
	merged, err = s.rerankArticles(ctx, kb, f.request.Query, merged, config, pool)
	if err != nil {
		return nil, err
	}
	if f.request.CollapseSimilar == nil || *f.request.CollapseSimilar {
		merged, err = s.collapseArticleReports(ctx, tenantID, merged, config)
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

func (s *knowledgeSearchService) eligibleArticles(ctx context.Context, tenantID uint64, kbID string, f articleSearchFilters, dates bool) *gorm.DB {
	query := s.db.WithContext(ctx).Table("knowledges AS k").Where(`k.tenant_id = ? AND k.knowledge_base_id = ?
		AND k.deleted_at IS NULL AND k.parse_status = 'completed' AND k.enable_status = 'enabled'`, tenantID, kbID)
	if len(f.request.ContentTypes) > 0 {
		query = query.Where("k.content_type IN ?", f.request.ContentTypes)
	}
	if len(f.request.Sources) > 0 {
		query = query.Where("k.source_kind IN ?", f.request.Sources)
	}
	if len(f.request.EntityIDs) > 0 {
		query = query.Where(`EXISTS (SELECT 1 FROM knowledge_entities ke WHERE ke.tenant_id=k.tenant_id
			AND ke.knowledge_id=k.id AND ke.entity_id IN ?)`, f.request.EntityIDs)
	}
	if len(f.request.TopicIDs) > 0 {
		query = query.Where(`EXISTS (SELECT 1 FROM knowledge_topics kt WHERE kt.tenant_id=k.tenant_id
			AND kt.knowledge_id=k.id AND kt.topic_id IN ?)`, f.request.TopicIDs)
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

func (s *knowledgeSearchService) rerankArticles(ctx context.Context, kb *types.KnowledgeBase, query string, results []*types.ArticleSearchResult, config *types.RetrievalConfig, pool int) ([]*types.ArticleSearchResult, error) {
	if len(results) == 0 {
		return results, nil
	}
	modelID, err := ResolveModelTier(ctx, s.db, kb, types.ModelTierRerank)
	if err != nil || modelID == "" {
		return results, err
	}
	state := &knowledgesearch.State{Request: knowledgesearch.Request{
		Query: query, RerankModelID: modelID, RerankTopK: pool, RerankThreshold: config.GetEffectiveRerankThreshold(),
	}, Results: knowledgesearch.Results{RewriteQuery: query}}
	byID := make(map[string]*types.ArticleSearchResult, len(results))
	for _, result := range results {
		text := result.Title + "\n" + result.Summary.RenderText()
		for _, snippet := range result.Snippets {
			text += "\n\n" + snippet.Content
		}
		state.SearchResult = append(state.SearchResult, &types.SearchResult{ID: result.ArticleID, KnowledgeID: result.ArticleID, Content: text, Score: result.Score})
		byID[result.ArticleID] = result
	}
	if err := s.rerank.Run(ctx, state, func() *knowledgesearch.StageError { return nil }); err != nil {
		if err == knowledgesearch.ErrSearchNothing {
			return []*types.ArticleSearchResult{}, nil
		}
		return nil, err.Err
	}
	if len(state.RerankResult) == 0 {
		return results, nil
	}
	reranked := make([]*types.ArticleSearchResult, 0, len(state.RerankResult))
	for _, hit := range state.RerankResult {
		if result := byID[hit.ID]; result != nil {
			result.Score = hit.Score
			reranked = append(reranked, result)
		}
	}
	// The existing stage applies diversity selection; article display uses its scores.
	slices.SortStableFunc(reranked, compareArticleScores)
	return reranked, nil
}

func compareArticleScores(a, b *types.ArticleSearchResult) int {
	if a.Score > b.Score {
		return -1
	}
	if a.Score < b.Score {
		return 1
	}
	return strings.Compare(a.ArticleID, b.ArticleID)
}
