package postgres

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/types"
)

func checkVectorModelProfile(ctx context.Context, tx *gorm.DB, parameters map[string]any) error {
	hash, _ := parameters["model_profile_hash"].(string)
	if hash == "" {
		return nil
	}
	modelID, _ := parameters["model_id"].(string)
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("embedding model persistence requires a space")
	}
	var model types.Model
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND (tenant_id=? OR is_builtin=true)", modelID, tenantID).First(&model).Error; err != nil {
		return err
	}
	if types.EmbeddingProfileHash(model.ID, model.Parameters.EmbeddingParameters.Dimension, &model) != hash {
		return fmt.Errorf("embedding model profile changed during vector generation")
	}
	return nil
}
