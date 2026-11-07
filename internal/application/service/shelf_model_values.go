package service

import (
	"context"
	"reflect"
	"slices"
	"time"

	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func shelfOptionExists(options []types.ShelfOption, key string) bool {
	return slices.ContainsFunc(options, func(option types.ShelfOption) bool { return option.Key == key })
}

func validateShelfConditions(tx *gorm.DB, library *types.Library, entityID string, conditions []types.ShelfCondition, at time.Time) error {
	if err := types.ValidateShelfConditions(conditions); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	for _, c := range conditions {
		if !shelfOptionExists(library.ShelfConfig.ModuleTypes, c.ModuleType) {
			return apperrors.NewBadRequestError("condition module type is not configured")
		}
		var count int64
		if err := tx.Model(&types.Relation{}).Where("library_id=? AND space_id=? AND subject_id=? AND relation_type='composed_of' AND attributes->>'module_type'=? AND object_id IN ? AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", library.ID, library.SpaceID, entityID, c.ModuleType, c.PartIDs, at, at).Distinct("object_id").Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(c.PartIDs)) {
			return apperrors.NewBadRequestError("condition parts must belong to the model's module at the effective time")
		}
	}
	return nil
}

// Overlapping alternatives would assign two facts to the same combination.
func shelfConditionsOverlap(a, b []types.ShelfCondition) bool {
	for _, x := range a {
		for _, y := range b {
			if x.ModuleType == y.ModuleType {
				intersects := false
				for _, id := range x.PartIDs {
					if slices.Contains(y.PartIDs, id) {
						intersects = true
					}
				}
				if !intersects {
					return false
				}
			}
		}
	}
	return true
}

func validateShelfEntityValues(tx *gorm.DB, library *types.Library, entity *types.Entity, at time.Time) error {
	if !library.ShelfEnabled {
		return nil
	}
	if value, ok := entity.Attributes["module_type"]; ok {
		module, _ := value.(string)
		if !shelfOptionExists(library.ShelfConfig.ModuleTypes, module) {
			return apperrors.NewBadRequestError("part module type is not configured")
		}
		var count int64
		if err := tx.Model(&types.Relation{}).Where("library_id=? AND object_id=? AND relation_type='composed_of' AND valid_to IS NULL AND attributes->>'module_type'<>?", library.ID, entity.ID, module).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return apperrors.NewBadRequestError("part module type conflicts with its composition relationships")
		}
	}
	if raw, ok := entity.Attributes["sale_statuses"]; ok {
		var rows []types.ShelfSaleStatus
		if err := types.DecodeShelfValue(raw, &rows); err != nil {
			return apperrors.NewBadRequestError(err.Error())
		}
		for i, row := range rows {
			if !shelfOptionExists(library.ShelfConfig.Regions, row.Region) {
				return apperrors.NewBadRequestError("sale status region is not configured")
			}
			if err := validateShelfConditions(tx, library, entity.ID, row.Conditions, at); err != nil {
				return err
			}
			for _, other := range rows[:i] {
				if row.Region == other.Region && shelfConditionsOverlap(row.Conditions, other.Conditions) {
					return apperrors.NewBadRequestError("uncertain: regional status conditions overlap")
				}
			}
		}
	}
	return validateShelfParameters(tx, library, entity, at)
}

func (s *EntityService) setShelfModelValues(ctx context.Context, draft *types.ShelfChangeset, entity *types.Entity, op types.ShelfOperation) (*types.Entity, error) {
	if entity.Kind != "model" {
		return nil, apperrors.NewBadRequestError("sale statuses and parameters belong to models")
	}
	at := op.EffectiveAt
	if draft.Source.Type == "import" {
		at = draft.Source.FileDate
	}
	if at != nil {
		if err := hydrateManagedEntityAttributes(ctx, s.db, []*types.Entity{entity}, *at); err != nil {
			return nil, err
		}
	}
	attrs := map[string]any{}
	for key, value := range entity.Attributes {
		attrs[key] = value
	}
	if op.SaleStatuses != nil {
		attrs["sale_statuses"] = *op.SaleStatuses
	}
	if op.ParameterValues != nil {
		var library types.Library
		if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", draft.LibraryID, draft.SpaceID).First(&library).Error; err != nil {
			return nil, err
		}
		for key, value := range *op.ParameterValues {
			if value == nil {
				return nil, apperrors.NewBadRequestError("uncertain: use an explicit empty array to remove a parameter")
			}
			if !slices.ContainsFunc(library.EntitySchema.Fields, func(field types.EntityFieldDefinition) bool { return field.Key == key && len(field.ProductLineIDs) > 0 }) {
				return nil, apperrors.NewBadRequestError("parameter field is not defined for a product line")
			}
			if len(value) == 0 {
				delete(attrs, key)
			} else {
				attrs[key] = value
			}
		}
	}
	return s.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: op.Revision, Attributes: &attrs, EffectiveAt: at})
}

func (s *EntityService) shelfStatusWarnings(ctx context.Context, draft *types.ShelfChangeset, item *types.ShelfChangesetItem) ([]types.ShelfWarning, error) {
	var before, after types.Entity
	if types.DecodeShelfValue(item.Before, &before) != nil || types.DecodeShelfValue(item.After, &after) != nil || after.Kind != "model" || reflect.DeepEqual(before.Attributes["sale_statuses"], after.Attributes["sale_statuses"]) {
		return nil, nil
	}
	var parts []string
	at := time.Now()
	if err := s.db.WithContext(ctx).Model(&types.Relation{}).Where("library_id=? AND subject_id=? AND relation_type='composed_of' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", draft.LibraryID, after.ID, at, at).Pluck("object_id", &parts).Error; err != nil {
		return nil, err
	}
	if len(parts) != 1 {
		return nil, nil
	}
	var ids []string
	if err := s.db.WithContext(ctx).Table("relations r").Joins("JOIN entities e ON e.id=r.subject_id AND e.archived_at IS NULL AND e.merged_into_id=''").Where("r.library_id=? AND r.object_id=? AND r.subject_id<>? AND r.relation_type='composed_of' AND r.valid_from<=? AND (r.valid_to IS NULL OR r.valid_to>?)", draft.LibraryID, parts[0], after.ID, at, at).Distinct("r.subject_id").Order("r.subject_id").Pluck("r.subject_id", &ids).Error; err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return []types.ShelfWarning{{Type: "component_sale_status_changed", Message: "A separately sold component model changed status; review these models independently.", ModelIDs: ids}}, nil
}

func shelfProductLine(tx *gorm.DB, library *types.Library, id string, at time.Time) (string, error) {
	var line string
	err := tx.Raw(`WITH RECURSIVE path(id,depth) AS (
 SELECT ?::varchar,0 UNION ALL
 SELECT r.object_id,p.depth+1 FROM path p JOIN relations r ON r.subject_id=p.id
 WHERE r.library_id=? AND r.space_id=? AND r.relation_type='belongs_to'
 AND r.valid_from<=? AND (r.valid_to IS NULL OR r.valid_to>?) AND p.depth<3)
 SELECT e.id FROM path p JOIN entities e ON e.id=p.id WHERE e.library_id=? AND e.kind='product_line' LIMIT 1`, id, library.ID, library.SpaceID, at, at, library.ID).Scan(&line).Error
	return line, err
}

func validateShelfParameters(tx *gorm.DB, library *types.Library, entity *types.Entity, at time.Time) error {
	for _, field := range library.EntitySchema.Fields {
		raw, ok := entity.Attributes[field.Key]
		if !ok || len(field.ProductLineIDs) == 0 {
			continue
		}
		line, err := shelfProductLine(tx, library, entity.ID, at)
		if err != nil {
			return err
		}
		if !slices.Contains(field.ProductLineIDs, line) {
			return apperrors.NewBadRequestError("parameter field " + field.Key + " is outside the model's product line")
		}
		var values []types.ShelfParameterValue
		if err := types.DecodeShelfValue(raw, &values); err != nil {
			return apperrors.NewBadRequestError(err.Error())
		}
		for i, value := range values {
			if err := validateShelfConditions(tx, library, entity.ID, value.Conditions, at); err != nil {
				return err
			}
			for _, other := range values[:i] {
				if shelfConditionsOverlap(value.Conditions, other.Conditions) {
					return apperrors.NewBadRequestError("uncertain: parameter value conditions overlap")
				}
			}
			plain := field
			plain.ProductLineIDs = nil
			if err := validateAttributeReferences(tx, library.SpaceID, library.ID, []types.EntityFieldDefinition{plain}, map[string]any{field.Key: value.Value}); err != nil {
				return err
			}
		}
	}
	return nil
}

// A structural edit must not strand current or scheduled facts outside their
// product line or composition. Historical facts keep their original intervals.
func validateShelfStructure(tx *gorm.DB, library *types.Library, at time.Time) error {
	if !library.ShelfEnabled {
		return nil
	}
	var models []types.Entity
	if err := tx.Where("library_id=? AND kind='model' AND merged_into_id=''", library.ID).Find(&models).Error; err != nil {
		return err
	}
	for i := range models {
		model := &models[i]
		if err := hydrateManagedEntityAttributes(tx.Statement.Context, tx, []*types.Entity{model}, at); err != nil {
			return err
		}
		if err := validateShelfEntityValues(tx, library, model, at); err != nil {
			return err
		}
		var pending []types.EntityAttributeVersion
		if err := tx.Where("entity_id=? AND effective_at>?", model.ID, at).Find(&pending).Error; err != nil {
			return err
		}
		for _, version := range pending {
			model.Attributes = version.Attributes
			if err := validateShelfEntityValues(tx, library, model, version.EffectiveAt); err != nil {
				return err
			}
		}
	}
	return nil
}
