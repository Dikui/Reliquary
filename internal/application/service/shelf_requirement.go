package service

import (
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func validateShelfChecklistValues(tx *gorm.DB, library *types.Library, entity *types.Entity) error {
	if entity.Kind == "document_requirement" {
		if _, ok := entity.Attributes["requirement"]; !ok {
			var count int64
			if err := tx.Model(&types.EntityAttributeVersion{}).Where("entity_id=? AND jsonb_exists(attributes,'requirement')", entity.ID).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return apperrors.NewBadRequestError("requirement metadata cannot be removed; archive the requirement explicitly")
			}
		}
	}

	if raw, ok := entity.Attributes["current_stage"]; ok {
		stage, _ := raw.(string)
		if !shelfOptionExists(library.ShelfConfig.Stages, stage) {
			return apperrors.NewBadRequestError("current stage must be a configured stage key; remove it to inherit")
		}
	}
	if raw, ok := entity.Attributes["requirement"]; ok {
		var r types.ShelfRequirement
		if err := types.DecodeShelfValue(raw, &r); err != nil {
			return apperrors.NewBadRequestError(err.Error())
		}
		if !shelfOptionExists(library.ShelfConfig.Stages, r.FinalDueStage) || r.DraftDueStage != "" && !shelfOptionExists(library.ShelfConfig.Stages, r.DraftDueStage) {
			return apperrors.NewBadRequestError("requirement due stages must be configured stage keys")
		}
		positions := map[string]int{}
		for i, stage := range library.ShelfConfig.Stages {
			positions[stage.Key] = i
		}
		if r.DraftDueStage != "" && positions[r.DraftDueStage] > positions[r.FinalDueStage] {
			return apperrors.NewBadRequestError("draft due stage must not follow final due stage")
		}
		for market := range r.Markets {
			if !shelfOptionExists(library.ShelfConfig.Markets, market) {
				return apperrors.NewBadRequestError("requirement market must be a configured market key")
			}
		}
	}
	return nil
}
