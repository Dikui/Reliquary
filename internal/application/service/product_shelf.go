package service

import (
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func validateProductShelfChange(tx *gorm.DB, before, after *types.Library) error {
	if before.ShelfEnabled && !after.ShelfEnabled {
		return apperrors.NewBadRequestError("an enabled shelf cannot be disabled")
	}
	if after.ShelfEnabled && !before.ShelfEnabled {
		var count int64
		if err := tx.Model(&types.Library{}).Where("id<>? AND product_library_ids @> jsonb_build_array(?::text)", after.ID, after.ID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return apperrors.NewBadRequestError("remove this library from automatic product matching targets before enabling its shelf")
		}
	}
	for i, previous := range [][]types.ShelfOption{before.ShelfConfig.ModuleTypes, before.ShelfConfig.Regions, before.ShelfConfig.Stages} {
		next := [][]types.ShelfOption{after.ShelfConfig.ModuleTypes, after.ShelfConfig.Regions, after.ShelfConfig.Stages}[i]
		keys := map[string]bool{}
		for _, item := range next {
			keys[item.Key] = true
		}
		for _, item := range previous {
			if !keys[item.Key] {
				return apperrors.NewBadRequestError("shelf list values may be appended, renamed or reordered; existing keys must be retained")
			}
		}
	}
	return nil
}
