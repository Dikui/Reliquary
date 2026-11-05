package service

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"reliquary/internal/application/graph"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type EntityConversionPreview struct {
	EntryCount    int64            `json:"entry_count"`
	EntityCounts  map[string]int64 `json:"entity_counts"`
	RelationCount int64            `json:"relation_count"`
}

func (s *EntityService) EntityConversionPreview(ctx context.Context, id string) (*EntityConversionPreview, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	var library types.Library
	if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", id, spaceID).First(&library).Error; err != nil {
		return nil, managedWriteError(err)
	}
	result := &EntityConversionPreview{EntityCounts: map[string]int64{}}
	db := s.db.WithContext(ctx)
	if err := db.Model(&types.Entry{}).Where("library_id=? AND space_id=?", id, spaceID).Count(&result.EntryCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&types.Relation{}).Where("library_id=? AND space_id=?", id, spaceID).Count(&result.RelationCount).Error; err != nil {
		return nil, err
	}
	var counts []struct {
		EntityType string
		Count      int64
	}
	if err := db.Model(&types.Entity{}).Select("entity_type,count(*) AS count").Where("library_id=? AND space_id=?", id, spaceID).Group("entity_type").Scan(&counts).Error; err != nil {
		return nil, err
	}
	for _, row := range counts {
		result.EntityCounts[row.EntityType] = row.Count
	}
	return result, nil
}

// Conversion changes the authority of existing identities, never their IDs or
// document ownership. A caller must explicitly map every existing entity type.
func convertLibraryEntities(ctx context.Context, tx *gorm.DB, library *types.Library, mapping map[string]string) error {
	var entities []types.Entity
	if err := tx.Where("library_id=? AND space_id=?", library.ID, library.SpaceID).Find(&entities).Error; err != nil {
		return err
	}
	if err := graph.Lock(tx, library.SpaceID); err != nil {
		return err
	}
	now := time.Now()
	for _, entity := range entities {
		kind, ok := mapping[entity.EntityType]
		if !ok {
			return apperrors.NewBadRequestError("choose an entity kind for existing type " + entity.EntityType)
		}
		if err := library.EntitySchema.ValidateAttributes(kind, entity.Attributes); err != nil {
			return apperrors.NewBadRequestError(err.Error())
		}
		entity.Kind = kind
		entity.Revision++
		entity.UpdatedAt = now
		if err := tx.Save(&entity).Error; err != nil {
			return err
		}
		version := types.EntityAttributeVersion{ID: uuid.NewString(), SpaceID: library.SpaceID, EntityID: entity.ID, Revision: entity.Revision, EffectiveAt: now, RecordedAt: now, Attributes: entity.Attributes}
		if err := tx.Create(&version).Error; err != nil {
			return err
		}
		if err := tx.Model(&types.EntryEntity{}).Where("space_id=? AND entity_id=?", library.SpaceID, entity.ID).Update("origin", "manual").Error; err != nil {
			return err
		}
		if err := queueManagedEntity(tx, &entity); err != nil {
			return err
		}
	}
	return NewOperationLogService(tx).RecordTx(ctx, tx, "convert_entity_library", library.ID, nil, map[string]any{"entity_kind_mapping": mapping, "entity_count": len(entities)})
}

func validateProductLibraries(tx *gorm.DB, library *types.Library) error {
	for _, id := range library.ProductLibraryIDs {
		if id == library.ID && library.ManagesEntities() {
			continue
		}
		var count int64
		if err := tx.Model(&types.Library{}).Where("id=? AND space_id=? AND entity_mode='managed'", id, library.SpaceID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return apperrors.NewBadRequestError("product libraries must be managed libraries in this space")
		}
	}
	// In-use document relationship names cannot disappear from their product library.
	var names []string
	if err := tx.Table("entry_entities ee").Joins("JOIN entities e ON e.id=ee.entity_id").Where("e.library_id=? AND e.space_id=?", library.ID, library.SpaceID).Distinct().Pluck("ee.relation_type", &names).Error; err != nil {
		return err
	}
	for _, name := range names {
		if !slices.Contains(library.DocumentRelationTypes, name) {
			return apperrors.NewBadRequestError("document relationship type is still in use: " + name)
		}
	}
	return nil
}
