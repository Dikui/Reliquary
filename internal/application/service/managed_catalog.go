package service

import (
	"reflect"
	"slices"

	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func validateManagedCatalogChange(tx *gorm.DB, before, after *types.Library) error {
	if before.EntityMode != after.EntityMode {
		for _, model := range []any{&types.Entry{}, &types.Entity{}, &types.Relation{}} {
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
	if reflect.DeepEqual(before.EntitySchema, after.EntitySchema) && reflect.DeepEqual(before.RelationTypes, after.RelationTypes) {
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
		for _, old := range before.EntitySchema.Fields {
			if _, used := entity.Attributes[old.Key]; !used {
				continue
			}
			for _, field := range after.EntitySchema.Fields {
				if old.Key == field.Key && (old.Type != field.Type || old.Unit != field.Unit) {
					return apperrors.NewBadRequestError("used field types and units cannot change; create a new field")
				}
			}
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
