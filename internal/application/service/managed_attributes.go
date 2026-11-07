package service

import (
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func validateManagedAttributeReferences(tx *gorm.DB, library *types.Library, attributes map[string]any) error {
	return validateAttributeReferences(tx, library.SpaceID, library.ID, library.EntitySchema.Fields, attributes)
}

func validateAttributeReferences(tx *gorm.DB, spaceID uint64, libraryID string, fields []types.EntityFieldDefinition, attributes map[string]any) error {
	for _, field := range fields {
		value, present := attributes[field.Key]
		if !present || len(field.ProductLineIDs) > 0 || (field.Type != "entity_ref" && field.Type != "attachment") {
			continue
		}
		id, ok := value.(string)
		if !ok {
			return apperrors.NewBadRequestError("invalid reference for field " + field.Key)
		}
		var count int64
		var query *gorm.DB
		if field.Type == "entity_ref" {
			query = tx.Table("entities e").Joins("JOIN libraries l ON l.id=e.library_id").Where("e.id=? AND e.space_id=? AND l.space_id=? AND l.entity_mode='managed' AND l.deleted_at IS NULL", id, spaceID, spaceID)
		} else {
			query = tx.Model(&types.EntityAttachment{}).Where("id=? AND space_id=? AND library_id=?", id, spaceID, libraryID)
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return apperrors.NewBadRequestError("reference is missing or outside the allowed library/space for field " + field.Key)
		}
	}
	return nil
}

// A historical or pending snapshot owns its references just like a current value.
func managedValueReferenced(tx *gorm.DB, spaceID uint64, id, excludingEntity string) (bool, error) {
	var referenced bool
	err := tx.Raw(`SELECT
  EXISTS (SELECT 1 FROM entities e WHERE e.space_id=? AND e.id<>? AND jsonb_path_query_array(e.attributes,'$.**') @> jsonb_build_array(?::text)) OR
  EXISTS (SELECT 1 FROM entity_attribute_versions v WHERE v.space_id=? AND v.entity_id<>? AND jsonb_path_query_array(v.attributes,'$.**') @> jsonb_build_array(?::text)) OR
  EXISTS (SELECT 1 FROM relations r WHERE r.space_id=? AND jsonb_path_query_array(r.attributes,'$.**') @> jsonb_build_array(?::text)) OR
  EXISTS (SELECT 1 FROM libraries l WHERE l.space_id=? AND jsonb_path_query_array(l.entity_schema,'$.fields[*].product_line_ids[*]') @> jsonb_build_array(?::text))`,
		spaceID, excludingEntity, id, spaceID, excludingEntity, id, spaceID, id, spaceID, id).Scan(&referenced).Error
	return referenced, err
}
