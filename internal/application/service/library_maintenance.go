package service

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/types"
)

func libraryWritePaused(state maintenanceops.State, includesModelTiers bool) error {
	err := state.BusinessWriteError()
	if includesModelTiers {
		err.Message += "。请单独提交模型档位，业务设置须在解除暂停后修改。"
	}
	return err
}

func lockedLibrarySettings(ctx context.Context, tx *gorm.DB, id string, spaceID uint64) (*types.Library, error) {
	var locked types.Library
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&locked, "id=? AND space_id=?", id, spaceID).Error; err != nil {
		return nil, err
	}
	return repository.NewLibraryRepository(tx).GetLibraryByID(ctx, id)
}

// Recovery configuration updates only model columns. In particular, saving a
// shelf's model bindings does not write a changeset or replace its catalogs.
func savePausedLibraryModels(ctx context.Context, tx *gorm.DB, library *types.Library, tiers types.ModelTierConfig, replaceSharedEmbedding bool) (*types.Library, error) {
	previous := library.ModelTiers
	validation := tiers
	if !replaceSharedEmbedding {
		// A catalog patch may supply just its processing settings. Keep the
		// existing shared model unless it explicitly selects a different one.
		if tiers.ModelID(types.ModelTierEmbedding) == "" {
			tiers.Embedding = previous.Embedding
		}
		if tiers.ModelID(types.ModelTierEmbedding) == previous.ModelID(types.ModelTierEmbedding) {
			validation.Embedding = ""
		}
	}
	if err := ValidateModelTierConfig(ctx, tx, validation); err != nil {
		return nil, apperrors.NewBadRequestError(err.Error())
	}
	if tiers == previous {
		return library, nil
	}
	var raw *string
	if err := tx.Raw(`SELECT model_tiers::text FROM libraries WHERE id=?`, library.ID).Row().Scan(&raw); err != nil {
		return nil, err
	}
	merged, err := preserveUneditedSettingsJSON(raw, previous, tiers)
	if err != nil {
		return nil, err
	}
	if tiers.Embedding != previous.Embedding {
		if err := tx.Model(&types.Space{}).Where("id=?", library.SpaceID).Update("embedding_model_id", tiers.Embedding).Error; err != nil {
			return nil, err
		}
	}
	updates := map[string]any{"model_tiers": gorm.Expr("?::jsonb", merged)}
	if tiers.Generation != previous.Generation {
		updates["summary_model_id"] = tiers.ModelID(types.ModelTierGeneration)
	}
	if err := tx.Model(library).Updates(updates).Error; err != nil {
		return nil, err
	}
	return repository.NewLibraryRepository(tx).GetLibraryByID(ctx, library.ID)
}
