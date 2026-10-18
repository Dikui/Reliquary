package service

import (
	"context"
	"gorm.io/gorm"
	search "reliquary/internal/application/service/search"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type searchService struct {
	libraryService interfaces.LibraryService
	spaceService   interfaces.SpaceService
	modelService   interfaces.ModelService
	rerank         *search.RerankStage
	db             *gorm.DB
	vault          *VaultService
}

func (s *searchService) SetVault(v *VaultService) { s.vault = v }

func NewSearchService(libraryService interfaces.LibraryService, spaceService interfaces.SpaceService, modelService interfaces.ModelService, db *gorm.DB) interfaces.SearchService {
	return &searchService{
		libraryService: libraryService, spaceService: spaceService,
		modelService: modelService, db: db,
		rerank: search.NewRerankStage(modelService),
	}
}

// chunkSearchService reuses the same repositories and model configuration for
// the chunk-oriented compatibility API. The public adapters below keep Library
// settings callers independent of the retrieval implementation.
type chunkSearchService struct{ *libraryService }

func (s *libraryService) HybridSearch(ctx context.Context, id string, params types.SearchParams) ([]*types.SearchResult, error) {
	return (&chunkSearchService{s}).HybridSearch(ctx, id, params)
}
func (s *libraryService) GetQueryEmbedding(ctx context.Context, libraryID string, queryText string) ([]float32, error) {
	return (&chunkSearchService{s}).GetQueryEmbedding(ctx, libraryID, queryText)
}
func (s *libraryService) ResolveEmbeddingModelKeys(ctx context.Context, libraries []*types.Library) map[string]string {
	return (&chunkSearchService{s}).ResolveEmbeddingModelKeys(ctx, libraries)
}
