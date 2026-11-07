package service

import (
	"context"
	"slices"
	"time"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (s *EntityService) shelfModelDetail(ctx context.Context, entity *types.Entity, at time.Time, combination map[string]string) (*types.ShelfModelDetail, error) {
	if entity.Kind != "model" {
		if combination != nil {
			return nil, apperrors.NewBadRequestError("a combination requires a shelf model")
		}
		return nil, nil
	}
	var library types.Library
	if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", entity.LibraryID, entity.SpaceID).First(&library).Error; err != nil {
		return nil, err
	}
	if !library.ShelfEnabled {
		if combination != nil {
			return nil, apperrors.NewBadRequestError("a combination requires a shelf model")
		}
		return nil, nil
	}
	var relations []types.Relation
	if err := s.db.WithContext(ctx).Where("library_id=? AND space_id=? AND subject_id=? AND relation_type='composed_of' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", entity.LibraryID, entity.SpaceID, entity.ID, at, at).Order("created_at,id").Find(&relations).Error; err != nil {
		return nil, err
	}
	result := &types.ShelfModelDetail{Composition: []types.ShelfComposition{}, SaleStatuses: []types.ShelfSaleStatus{}, Parameters: map[string][]types.ShelfParameterValue{}, CombinationRules: [][]types.ShelfCondition{}}
	ids := []string{}
	for _, r := range relations {
		ids = append(ids, r.ObjectID)
	}
	parts := []types.Entity{}
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Where("id IN ? AND library_id=? AND space_id=?", ids, entity.LibraryID, entity.SpaceID).Find(&parts).Error; err != nil {
			return nil, err
		}
	}
	pointers := make([]*types.Entity, len(parts))
	for i := range parts {
		pointers[i] = &parts[i]
	}
	if err := hydrateManagedEntityAttributes(ctx, s.db, pointers, at); err != nil {
		return nil, err
	}
	byID := map[string]types.Entity{}
	for _, part := range parts {
		part.KindLabel = managedKindLabel(&library, part.Kind)
		byID[part.ID] = part
	}
	for _, module := range library.ShelfConfig.ModuleTypes {
		group := types.ShelfComposition{ModuleType: module.Key, Parts: []types.Entity{}}
		for _, r := range relations {
			if r.Attributes["module_type"] == module.Key {
				if part, ok := byID[r.ObjectID]; ok {
					group.Parts = append(group.Parts, part)
				}
			}
		}
		if len(group.Parts) > 0 {
			group.Optional = len(group.Parts) > 1
			result.Composition = append(result.Composition, group)
		}
	}
	if combination != nil {
		for module, id := range combination {
			found := false
			for _, group := range result.Composition {
				if group.ModuleType == module && slices.ContainsFunc(group.Parts, func(part types.Entity) bool { return part.ID == id }) {
					found = true
				}
			}
			if !found {
				return nil, apperrors.NewBadRequestError("combination selects a part outside this model's module")
			}
		}
		combination = cloneShelfCombination(combination)
		for _, group := range result.Composition {
			if len(group.Parts) == 1 && combination[group.ModuleType] == "" {
				combination[group.ModuleType] = group.Parts[0].ID
			}
			if combination[group.ModuleType] == "" {
				return nil, apperrors.NewBadRequestError("combination must select each optional module")
			}
		}
	}
	if raw, ok := entity.Attributes["sale_statuses"]; ok {
		var rows []types.ShelfSaleStatus
		if err := types.DecodeShelfValue(raw, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if shelfConditionsMatch(row.Conditions, combination) {
				result.SaleStatuses = append(result.SaleStatuses, row)
			}
		}
		entity.Attributes["sale_statuses"] = result.SaleStatuses
	}
	for _, field := range library.EntitySchema.Fields {
		if len(field.ProductLineIDs) > 0 {
			if raw, ok := entity.Attributes[field.Key]; ok {
				var values []types.ShelfParameterValue
				if err := types.DecodeShelfValue(raw, &values); err != nil {
					return nil, err
				}
				result.Parameters[field.Key] = []types.ShelfParameterValue{}
				for _, value := range values {
					if shelfConditionsMatch(value.Conditions, combination) {
						result.Parameters[field.Key] = append(result.Parameters[field.Key], value)
					}
				}
				entity.Attributes[field.Key] = result.Parameters[field.Key]
			}
		}
	}
	return result, nil
}

func cloneShelfCombination(value map[string]string) map[string]string {
	copy := map[string]string{}
	for k, v := range value {
		copy[k] = v
	}
	return copy
}
func shelfConditionsMatch(conditions []types.ShelfCondition, combination map[string]string) bool {
	if combination == nil {
		return true
	}
	for _, c := range conditions {
		if !slices.Contains(c.PartIDs, combination[c.ModuleType]) {
			return false
		}
	}
	return true
}
