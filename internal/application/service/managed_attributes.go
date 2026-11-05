package service

import (
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func validateManagedAttributeReferences(tx *gorm.DB, library *types.Library, attributes map[string]any) error {
	return validateAttributeReferences(tx, library.SpaceID, library.EntitySchema.Fields, attributes)
}

func validateAttributeReferences(tx *gorm.DB, spaceID uint64, fields []types.EntityFieldDefinition, attributes map[string]any) error {
	for _, field := range fields {
		value, present := attributes[field.Key]
		if !present || (field.Type != "entity_ref" && field.Type != "attachment") {
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
			query = tx.Model(&types.EntityAttachment{}).Where("id=? AND space_id=?", id, spaceID)
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return apperrors.NewBadRequestError("reference not found in this space for field " + field.Key)
		}
	}
	return nil
}
