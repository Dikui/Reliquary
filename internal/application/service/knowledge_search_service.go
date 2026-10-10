package service

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	knowledgesearch "reliquary/internal/application/service/knowledge_search"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type knowledgeSearchService struct {
	knowledgeBaseService interfaces.KnowledgeBaseService
	knowledgeService     interfaces.KnowledgeService
	tenantService        interfaces.TenantService
	modelService         interfaces.ModelService
	search               *knowledgesearch.SearchStage
	rerank               *knowledgesearch.RerankStage
	merge                *knowledgesearch.MergeStage
	filter               *knowledgesearch.FilterTopKStage
	db                   *gorm.DB
}

func NewKnowledgeSearchService(kbService interfaces.KnowledgeBaseService, knowledgeService interfaces.KnowledgeService, tenantService interfaces.TenantService, modelService interfaces.ModelService, chunkRepo interfaces.ChunkRepository, chunkService interfaces.ChunkService, db *gorm.DB) interfaces.KnowledgeSearchService {
	return &knowledgeSearchService{
		knowledgeBaseService: kbService, knowledgeService: knowledgeService, tenantService: tenantService, modelService: modelService, db: db,
		search: knowledgesearch.NewSearchStage(kbService, knowledgeService, chunkService),
		rerank: knowledgesearch.NewRerankStage(modelService),
		merge:  knowledgesearch.NewMergeStage(chunkRepo, chunkService),
		filter: knowledgesearch.NewFilterTopKStage(),
	}
}

func (s *knowledgeSearchService) SearchKnowledge(ctx context.Context, knowledgeIDs []string, query string) ([]*types.SearchResult, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("workspace ID not found in context")
	}
	kb, err := s.knowledgeBaseService.GetKnowledgeBase(ctx)
	if err != nil {
		return nil, err
	}
	knowledgeBaseIDs := []string{kb.ID}
	targets, err := s.buildSearchTargets(ctx, tenantID, kb.ID, knowledgeIDs)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return []*types.SearchResult{}, nil
	}
	var retrieval *types.RetrievalConfig
	if tenant, readErr := s.tenantService.GetTenantByID(ctx, tenantID); readErr == nil && tenant != nil {
		retrieval = tenant.RetrievalConfig
	}
	state := &knowledgesearch.State{
		Request: knowledgesearch.Request{Query: query, TenantID: tenantID, KnowledgeBaseIDs: knowledgeBaseIDs, KnowledgeIDs: knowledgeIDs, SearchTargets: targets,
			EmbeddingTopK: retrieval.GetEffectiveEmbeddingTopK(), VectorThreshold: retrieval.GetEffectiveVectorThreshold(), KeywordThreshold: retrieval.GetEffectiveKeywordThreshold(), RerankTopK: retrieval.GetEffectiveRerankTopK(), RerankThreshold: retrieval.GetEffectiveRerankThreshold()},
		Results: knowledgesearch.Results{RewriteQuery: query},
	}
	tierRerank, err := ResolveModelTier(ctx, s.db, kb, types.ModelTierRerank)
	if err != nil {
		return nil, err
	}
	state.RerankModelID = tierRerank
	next := func() *knowledgesearch.StageError { return nil }
	for _, stage := range []func(context.Context, *knowledgesearch.State, func() *knowledgesearch.StageError) *knowledgesearch.StageError{s.search.Run, s.rerank.Run, s.merge.Run, s.filter.Run} {
		if stageErr := stage(ctx, state, next); stageErr != nil {
			if stageErr == knowledgesearch.ErrSearchNothing {
				return []*types.SearchResult{}, nil
			}
			return nil, stageErr.Err
		}
	}
	return state.MergeResult, nil
}

// buildSearchTargets confines article filters to the sole knowledge base.
func (s *knowledgeSearchService) buildSearchTargets(ctx context.Context, tenantID uint64, kbID string, knowledgeIDs []string) (types.SearchTargets, error) {
	if len(knowledgeIDs) == 0 {
		return types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: kbID, TenantID: tenantID}}, nil
	}
	articles, err := s.knowledgeService.GetKnowledgeBatch(ctx, tenantID, knowledgeIDs)
	if err != nil {
		logger.Warnf(ctx, "Failed to get knowledge batch for search targets: %v", err)
		return nil, err
	}
	ids := []string{}
	for _, article := range articles {
		if article != nil && article.KnowledgeBaseID == kbID {
			ids = append(ids, article.ID)
		}
	}
	if len(ids) == 0 {
		return types.SearchTargets{}, nil
	}
	return types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: kbID, TenantID: tenantID, KnowledgeIDs: ids, DisableRecallThresholds: true}}, nil
}
