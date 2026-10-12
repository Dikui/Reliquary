package service

import (
	"context"
	"gorm.io/gorm"
	search "reliquary/internal/application/service/search"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type searchService struct {
	knowledgeBaseService interfaces.KnowledgeBaseService
	spaceService         interfaces.SpaceService
	modelService         interfaces.ModelService
	rerank               *search.RerankStage
	db                   *gorm.DB
}

func NewSearchService(kbService interfaces.KnowledgeBaseService, spaceService interfaces.SpaceService, modelService interfaces.ModelService, db *gorm.DB) interfaces.SearchService {
	return &searchService{
		knowledgeBaseService: kbService, spaceService: spaceService,
		modelService: modelService, db: db,
		rerank: search.NewRerankStage(modelService),
	}
}

// chunkSearchService reuses the same repositories and model configuration for
// the chunk-oriented compatibility API. The public adapters below keep KB
// settings callers independent of the retrieval implementation.
type chunkSearchService struct{ *knowledgeBaseService }

func (s *knowledgeBaseService) HybridSearch(ctx context.Context, id string, params types.SearchParams) ([]*types.SearchResult, error) {
	return (&chunkSearchService{s}).HybridSearch(ctx, id, params)
}
func (s *knowledgeBaseService) GetQueryEmbedding(ctx context.Context, kbID string, queryText string) ([]float32, error) {
	return (&chunkSearchService{s}).GetQueryEmbedding(ctx, kbID, queryText)
}
func (s *knowledgeBaseService) ResolveEmbeddingModelKeys(ctx context.Context, kbs []*types.KnowledgeBase) map[string]string {
	return (&chunkSearchService{s}).ResolveEmbeddingModelKeys(ctx, kbs)
}
