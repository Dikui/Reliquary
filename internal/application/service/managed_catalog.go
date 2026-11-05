package service

import (
	"reflect"
	"slices"

	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func validateManagedCatalogChange(tx *gorm.DB, before, after *types.Library) error {
	if before.EntityMode != after.EntityMode && !after.ManagesEntities() {
		var referenced int64
		if err := tx.Model(&types.Library{}).Where("product_library_ids @> jsonb_build_array(?::text)", after.ID).Count(&referenced).Error; err != nil {
			return err
		}
		if referenced > 0 {
			return apperrors.NewBadRequestError("other libraries still use this product library")
		}
		for _, model := range []any{&types.Entry{}, &types.Entity{}, &types.Relation{}, &types.EntityAttachment{}} {
			var count int64
			if err := tx.Unscoped().Model(model).Where("library_id=?", after.ID).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return apperrors.NewBadRequestError("entity mode can only change in an empty library")
			}
		}
	}
	if !after.ManagesEntities() {
		return nil
	}
	if before.EntityMode == after.EntityMode && reflect.DeepEqual(before.EntitySchema, after.EntitySchema) && reflect.DeepEqual(before.RelationTypes, after.RelationTypes) {
		return nil
	}
	var entities []types.Entity
	if err := tx.Where("library_id=?", after.ID).Find(&entities).Error; err != nil {
		return err
	}
	for _, entity := range entities {
		if err := after.EntitySchema.ValidateAttributes(entity.Kind, entity.Attributes); err != nil {
			return apperrors.NewBadRequestError("schema would invalidate " + entity.CanonicalName + ": " + err.Error())
		}
		if err := validateUsedFieldDefinitions(before.EntitySchema.Fields, after.EntitySchema.Fields, entity.Attributes); err != nil {
			return err
		}
	}
	var versions []types.EntityAttributeVersion
	if err := tx.Table("entity_attribute_versions v").Select("v.*").Joins("JOIN entities e ON e.id=v.entity_id").Where("e.library_id=?", after.ID).Find(&versions).Error; err != nil {
		return err
	}
	for _, version := range versions {
		kind := ""
		for _, entity := range entities {
			if entity.ID == version.EntityID {
				kind = entity.Kind
				break
			}
		}
		if err := after.EntitySchema.ValidateAttributes(kind, version.Attributes); err != nil {
			return apperrors.NewBadRequestError("schema would invalidate attribute history: " + err.Error())
		}
		if err := validateUsedFieldDefinitions(before.EntitySchema.Fields, after.EntitySchema.Fields, version.Attributes); err != nil {
			return err
		}
	}

	var relations []types.Relation
	if err := tx.Where("library_id=?", after.ID).Find(&relations).Error; err != nil {
		return err
	}
	for _, r := range relations {
		old, oldOK := managedRelationType(before, r.RelationType)
		next, nextOK := managedRelationType(after, r.RelationType)
		if !nextOK || !next.Enabled {
			return apperrors.NewBadRequestError("a relationship type is still in use")
		}
		if oldOK && old.Hierarchical != next.Hierarchical {
			return apperrors.NewBadRequestError("the hierarchy role of a used relationship cannot change")
		}
		if err := types.ValidateFieldAttributes(next.Fields, r.Attributes); err != nil {
			return apperrors.NewBadRequestError("schema would invalidate relationship attributes: " + err.Error())
		}
		if err := validateUsedFieldDefinitions(old.Fields, next.Fields, r.Attributes); err != nil {
			return err
		}
		var subject, object *types.Entity
		for i := range entities {
			if entities[i].ID == r.SubjectID {
				subject = &entities[i]
			}
			if entities[i].ID == r.ObjectID {
				object = &entities[i]
			}
		}
		if subject != nil && object != nil {
			if err := validateManagedEndpoints(after, next, subject, object); err != nil {
				return err
			}
		}
	}
	return nil
}

func managedRelationType(library *types.Library, name string) (types.LibraryRelationType, bool) {
	for _, r := range library.RelationTypes {
		if r.Name == name {
			return r, true
		}
	}
	return types.LibraryRelationType{}, false
}

func validateManagedEndpoints(library *types.Library, rule types.LibraryRelationType, subject, object *types.Entity) error {
	if subject.ID == object.ID {
		return apperrors.NewBadRequestError("an entity cannot relate to itself")
	}
	if (len(rule.SubjectKinds) > 0 && !slices.Contains(rule.SubjectKinds, subject.Kind)) || (len(rule.ObjectKinds) > 0 && !slices.Contains(rule.ObjectKinds, object.Kind)) {
		return apperrors.NewBadRequestError("the entity kinds do not match this relationship rule")
	}
	if rule.Hierarchical {
		kind, _ := library.EntitySchema.Kind(subject.Kind)
		if !slices.Contains(kind.ParentKinds, object.Kind) {
			return apperrors.NewBadRequestError("this parent kind is not allowed")
		}
	}
	return nil
}

func validateUsedFieldDefinitions(before, after []types.EntityFieldDefinition, attributes map[string]any) error {
	for _, old := range before {
		if _, used := attributes[old.Key]; !used {
			continue
		}
		for _, field := range after {
			if old.Key == field.Key && (old.Type != field.Type || old.Unit != field.Unit) {
				return apperrors.NewBadRequestError("used field types and units cannot change; create a new field")
			}
		}
	}
	return nil
}
