package retriever

import (
	"context"
	"slices"

	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

// PostgresRetrievalService reads and maintains the application's PostgreSQL indexes.
type PostgresRetrievalService struct {
	interfaces.RetrieveEngineService
}

func NewPostgresRetrievalService(repository interfaces.RetrieveEngineRepository) interfaces.RetrievalService {
	return &PostgresRetrievalService{NewKVHybridRetrieveEngine(repository, types.PostgresRetrieverEngineType)}
}

func (s *PostgresRetrievalService) Retrieve(ctx context.Context, params []types.RetrieveParams) ([]*types.RetrieveResult, error) {
	var results []*types.RetrieveResult
	for _, param := range params {
		result, err := s.RetrieveEngineService.Retrieve(ctx, param)
		if err != nil {
			return nil, err
		}
		results = append(results, result...)
	}
	return results, nil
}

func (s *PostgresRetrievalService) SupportRetriever(kind types.RetrieverType) bool {
	return slices.Contains(s.Support(), kind)
}
func (s *PostgresRetrievalService) Index(ctx context.Context, embedder embedding.Embedder, index *types.IndexInfo) error {
	return s.RetrieveEngineService.Index(ctx, embedder, index, s.Support())
}
func (s *PostgresRetrievalService) BatchIndex(ctx context.Context, embedder embedding.Embedder, indices []*types.IndexInfo) error {
	indices = utils.Deduplicate(func(index *types.IndexInfo) string { return index.SourceID }, indices...)
	return s.RetrieveEngineService.BatchIndex(ctx, embedder, indices, s.Support())
}
func (s *PostgresRetrievalService) EstimateStorageSize(ctx context.Context, embedder embedding.Embedder, indices []*types.IndexInfo) int64 {
	return s.RetrieveEngineService.EstimateStorageSize(ctx, embedder, indices, s.Support())
}
