package service

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	knowledgesearch "reliquary/internal/application/service/knowledge_search"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"sort"
	"strings"
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

func (s *knowledgeSearchService) SearchKnowledge(ctx context.Context, knowledgeBaseIDs, knowledgeIDs []string, query string) ([]*types.SearchResult, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("workspace ID not found in context")
	}
	targets, err := s.buildSearchTargets(ctx, tenantID, knowledgeBaseIDs, knowledgeIDs)
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
	var routingKB *types.KnowledgeBase
	if len(knowledgeBaseIDs) > 0 {
		routingKB, err = s.knowledgeBaseService.GetKnowledgeBaseByID(ctx, knowledgeBaseIDs[0])
		if err != nil {
			return nil, err
		}
	}
	tierRerank, err := ResolveModelTier(ctx, s.db, routingKB, types.ModelTierRerank)
	if err != nil {
		return nil, err
	}
	if retrieval != nil && retrieval.RerankModelID != "" {
		state.RerankModelID = retrieval.RerankModelID
	} else if tierRerank != "" {
		state.RerankModelID = tierRerank
	} else {
		models, readErr := s.modelService.ListModels(ctx)
		if readErr != nil {
			return nil, readErr
		}
		for _, model := range models {
			if model != nil && model.Type == types.ModelTypeRerank {
				state.RerankModelID = model.ID
				break
			}
		}
	}
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

func (s *knowledgeSearchService) buildSearchTargets(ctx context.Context, tenantID uint64, kbIDs, knowledgeIDs []string) (types.SearchTargets, error) {
	targets := types.SearchTargets{}
	fullKBs := map[string]bool{}
	for _, id := range kbIDs {
		id = strings.TrimSpace(id)
		if id == "" || fullKBs[id] {
			continue
		}
		fullKBs[id] = true
		targets = append(targets, &types.SearchTarget{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: id, TenantID: tenantID})
	}
	if len(knowledgeIDs) == 0 {
		return targets, nil
	}
	knowledge, err := s.knowledgeService.GetKnowledgeBatch(ctx, tenantID, knowledgeIDs)
	if err != nil {
		logger.Warnf(ctx, "Failed to get knowledge batch for search targets: %v", err)
		return targets, nil
	}
	byKB := map[string][]string{}
	for _, article := range knowledge {
		if article == nil || article.KnowledgeBaseID == "" || fullKBs[article.KnowledgeBaseID] {
			continue
		}
		byKB[article.KnowledgeBaseID] = append(byKB[article.KnowledgeBaseID], article.ID)
	}
	partialKBs := make([]string, 0, len(byKB))
	for id := range byKB {
		partialKBs = append(partialKBs, id)
	}
	sort.Strings(partialKBs)
	for _, id := range partialKBs {
		targets = append(targets, &types.SearchTarget{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: id, TenantID: tenantID, KnowledgeIDs: byKB[id], DisableRecallThresholds: true})
	}
	return targets, nil
}
