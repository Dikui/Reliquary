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
		for region := range r.Markets {
			if !shelfOptionExists(library.ShelfConfig.Regions, region) {
				return apperrors.NewBadRequestError("requirement market must be a configured region key")
			}
		}
	}
	return nil
}

// Module types are configuration keys, not entity identities. A self-targeted
// applies_to edge stores their explicit set; ordinary edges target product lines.
func validateShelfRequirementBinding(library *types.Library, relation types.Relation) error {
	var modules []string
	raw, has := relation.Attributes["module_types"]
	if relation.SubjectID != relation.ObjectID {
		if has {
			return apperrors.NewBadRequestError("a product line binding cannot also name module types")
		}
		return nil
	}
	if !has || types.DecodeShelfValue(raw, &modules) != nil || len(modules) == 0 {
		return apperrors.NewBadRequestError("a module binding requires a nonempty module_types set")
	}
	seen := map[string]bool{}
	for _, module := range modules {
		if seen[module] || !shelfOptionExists(library.ShelfConfig.ModuleTypes, module) {
			return apperrors.NewBadRequestError("requirement modules must be distinct configured keys")
		}
		seen[module] = true
	}
	return nil
}
