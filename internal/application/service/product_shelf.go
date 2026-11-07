package service

import (
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func validateProductShelfChange(tx *gorm.DB, before, after *types.Library) error {
	if after.ShelfEnabled {
		for _, field := range after.EntitySchema.Fields {
			seen := map[string]bool{}
			for _, id := range field.ProductLineIDs {
				if seen[id] {
					return apperrors.NewBadRequestError("parameter product lines must be unique")
				}
				seen[id] = true
				var count int64
				if err := tx.Model(&types.Entity{}).Where("id=? AND library_id=? AND space_id=? AND kind='product_line' AND merged_into_id=''", id, after.ID, after.SpaceID).Count(&count).Error; err != nil {
					return err
				}
				if count != 1 {
					return apperrors.NewBadRequestError("parameter product line is missing or outside this library")
				}
			}
		}
	}
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
