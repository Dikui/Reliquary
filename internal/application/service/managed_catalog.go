package service

import (
	"slices"
	"strings"
	"time"

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
	changedFields := changedManagedFields(before.EntitySchema.Fields, after.EntitySchema.Fields)
	kindsChanged := false
	for _, old := range before.EntitySchema.Kinds {
		next, exists := after.EntitySchema.Kind(old.Key)
		if !exists || !slices.Equal(old.ParentKinds, next.ParentKinds) {
			kindsChanged = true
		}
	}
	if len(changedFields) > 0 || kindsChanged {
		var entities []types.Entity
		query := tx.Select("id", "kind", "canonical_name", "attributes").Where("library_id=?", after.ID)
		if !kindsChanged {
			keys := "{" + strings.Join(changedFields, ",") + "}"
			query = query.Where("jsonb_exists_any(attributes, ?::text[]) OR EXISTS (SELECT 1 FROM entity_attribute_versions v WHERE v.entity_id=entities.id AND jsonb_exists_any(v.attributes, ?::text[]))", keys, keys)
		}
		if err := query.Find(&entities).Error; err != nil {
			return err
		}
		pointers := make([]*types.Entity, len(entities))
		for i := range entities {
			pointers[i] = &entities[i]
		}
		if err := hydrateManagedEntityAttributes(tx.Statement.Context, tx, pointers, time.Now()); err != nil {
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
	}
	if len(changedFields) > 0 {
		var versions []types.EntityAttributeVersion
		if err := tx.Table("entity_attribute_versions v").Select("v.*").Joins("JOIN entities e ON e.id=v.entity_id").
			Where("e.library_id=? AND jsonb_exists_any(v.attributes, ?::text[])", after.ID, "{"+strings.Join(changedFields, ",")+"}").Find(&versions).Error; err != nil {
			return err
		}
		for _, version := range versions {
			// Historical attributes belong to the category at the time, not today's category.
			if err := types.ValidateFieldAttributes(after.EntitySchema.Fields, version.Attributes); err != nil {
				return apperrors.NewBadRequestError("schema would invalidate attribute history: " + err.Error())
			}
			if err := validateUsedFieldDefinitions(before.EntitySchema.Fields, after.EntitySchema.Fields, version.Attributes); err != nil {
				return err
			}
		}
	}
	oldRules, nextRules := map[string]types.LibraryRelationType{}, map[string]types.LibraryRelationType{}
	for _, rule := range before.RelationTypes {
		oldRules[rule.Name] = rule
	}
	for _, rule := range after.RelationTypes {
		nextRules[rule.Name] = rule
	}
	changedRules := []string{}
	for name, old := range oldRules {
		next, exists := nextRules[name]
		if !exists || old.Enabled != next.Enabled || old.Hierarchical != next.Hierarchical ||
			!slices.Equal(old.SubjectKinds, next.SubjectKinds) || !slices.Equal(old.ObjectKinds, next.ObjectKinds) ||
			len(changedManagedFields(old.Fields, next.Fields)) > 0 {
			changedRules = append(changedRules, name)
		}
	}
	if len(changedRules) == 0 && !kindsChanged && before.ManagesEntities() {
		return nil
	}
	var relations []types.Relation
	query := tx.Where("library_id=?", after.ID)
	if !kindsChanged && before.ManagesEntities() {
		query = query.Where("relation_type IN ?", changedRules)
	}
	if err := query.Find(&relations).Error; err != nil {
		return err
	}
	ids := []string{}
	for _, relation := range relations {
		ids = append(ids, relation.SubjectID, relation.ObjectID)
	}
	var endpoints []types.Entity
	if len(ids) > 0 {
		if err := tx.Select("id", "kind").Where("id IN ?", ids).Find(&endpoints).Error; err != nil {
			return err
		}
	}
	entitiesByID := make(map[string]*types.Entity, len(endpoints))
	for i := range endpoints {
		entitiesByID[endpoints[i].ID] = &endpoints[i]
	}
	for _, r := range relations {
		old, oldOK := oldRules[r.RelationType]
		next, nextOK := nextRules[r.RelationType]
		if !nextOK || (!next.Enabled && r.ValidTo == nil) {
			return apperrors.NewBadRequestError("a relationship type is still in use")
		}
		if oldOK && old.Hierarchical != next.Hierarchical {
			return apperrors.NewBadRequestError("the hierarchy role of a used relationship cannot change because it would alter historical structure")
		}
		if err := types.ValidateFieldAttributes(next.Fields, r.Attributes); err != nil {
			return apperrors.NewBadRequestError("schema would invalidate relationship attributes: " + err.Error())
		}
		if err := validateUsedFieldDefinitions(old.Fields, next.Fields, r.Attributes); err != nil {
			return err
		}
		subject, object := entitiesByID[r.SubjectID], entitiesByID[r.ObjectID]
		if subject != nil && object != nil && r.ValidTo == nil {
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

// Presentation changes and new optional fields cannot invalidate stored values.
func changedManagedFields(before, after []types.EntityFieldDefinition) []string {
	next := make(map[string]types.EntityFieldDefinition, len(after))
	for _, field := range after {
		next[field.Key] = field
	}
	changed := []string{}
	for _, old := range before {
		field, exists := next[old.Key]
		if !exists || old.Type != field.Type || old.Unit != field.Unit || !slices.Equal(old.Options, field.Options) || !slices.Equal(old.Kinds, field.Kinds) || !slices.Equal(old.ProductLineIDs, field.ProductLineIDs) {
			changed = append(changed, old.Key)
		}
	}
	return changed
}

func validateUsedFieldDefinitions(before, after []types.EntityFieldDefinition, attributes map[string]any) error {
	next := make(map[string]types.EntityFieldDefinition, len(after))
	for _, field := range after {
		next[field.Key] = field
	}
	for _, old := range before {
		if _, used := attributes[old.Key]; !used {
			continue
		}
		if field, exists := next[old.Key]; exists {
			for _, line := range old.ProductLineIDs {
				if !slices.Contains(field.ProductLineIDs, line) {
					return apperrors.NewBadRequestError("used parameter product-line scope cannot be removed")
				}
			}
		}
		if field, exists := next[old.Key]; exists && (old.Type != field.Type || old.Unit != field.Unit) {
			return apperrors.NewBadRequestError("used field types and units cannot change; create a new field")
		}
	}
	return nil
}
