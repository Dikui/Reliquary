package service

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

// checkEmbeddingProfile is called inside the short persistence transaction.
// A client created before a model edit cannot publish vectors under its ID.
func checkEmbeddingProfile(ctx context.Context, tx *gorm.DB, embedder embedding.Embedder, spaceID uint64) error {
	profile, ok := embedder.(embedding.ProfiledEmbedder)
	if !ok || profile.GetProfileHash() == "" {
		return nil
	}
	var model types.Model
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND (space_id=? OR is_builtin=true)", embedder.GetModelID(), spaceID).First(&model).Error; err != nil {
		return err
	}
	if types.EmbeddingProfileHash(model.ID, model.Parameters.EmbeddingParameters.Dimension, &model) != profile.GetProfileHash() {
		return fmt.Errorf("embedding model profile changed during vector generation")
	}
	return nil
}
