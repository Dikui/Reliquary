package service

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"reliquary/internal/logger"
	"reliquary/internal/models/embedding"
)

const maxRetrievalPoolSize = 500

// GetQueryEmbedding computes the query vector with the shared space model.
func (s *libraryService) GetQueryEmbedding(ctx context.Context, libraryID string, queryText string) ([]float32, error) {
	library, err := s.repo.GetLibraryByID(ctx, libraryID)
	if err != nil {
		return nil, err
	}

	var embeddingModel embedding.Embedder

	embeddingModel, err = GetTierEmbeddingModel(ctx, s.db, s.modelService, library)

	if err != nil {
		logger.Errorf(ctx, "GetQueryEmbedding: failed to get embedding model %s: %v", library.EmbeddingModelID, err)
		return nil, err
	}

	vector, err := embeddingModel.Embed(ctx, queryText)
	if err != nil {
		return nil, err
	}
	if len(vector) != embeddingModel.GetDimensions() {
		return nil, fmt.Errorf("query embedding dimension mismatch: got %d, expected %d", len(vector), embeddingModel.GetDimensions())
	}
	if s.db != nil {
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return checkEmbeddingProfile(ctx, tx, embeddingModel, library.SpaceID)
		}); err != nil {
			return nil, err
		}
	}
	return vector, nil
}
