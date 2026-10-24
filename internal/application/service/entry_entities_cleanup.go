package service

import (
	"context"
	"reliquary/internal/types"

	"gorm.io/gorm"
)

// The caller holds the entity barrier and has withdrawn the old links.
func pruneUnreferencedEntities(tx *gorm.DB, spaceID uint64, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var orphans []string
	if err := tx.Model(&types.Entity{}).
		Where("space_id=? AND id IN ? AND merged_into_id=''", spaceID, ids).
		Where(`NOT EXISTS (SELECT 1 FROM entry_entities links WHERE links.space_id=entities.space_id AND links.entity_id=entities.id)`).
		Where(`NOT EXISTS (SELECT 1 FROM follows f WHERE f.space_id=entities.space_id AND f.target_type='entity' AND f.target_id=entities.id)`).
		Where(`NOT EXISTS (SELECT 1 FROM entities merged WHERE merged.space_id=entities.space_id AND merged.merged_into_id=entities.id)`).
		Where(`NOT EXISTS (SELECT 1 FROM entity_merge_records m WHERE m.space_id=entities.space_id AND (m.keep_entity_id=entities.id OR m.merge_entity_id=entities.id))`).
		Pluck("id", &orphans).Error; err != nil {
		return err
	}
	if len(orphans) == 0 {
		return nil
	}
	if err := tx.Where("space_id=? AND entity_id IN ?", spaceID, orphans).Delete(&types.EntityAlias{}).Error; err != nil {
		return err
	}
	if err := tx.Where("space_id=? AND (new_entity_id IN ? OR candidate_entity_id IN ?)", spaceID, orphans, orphans).Delete(&types.EntityMergeSuggestion{}).Error; err != nil {
		return err
	}
	if err := tx.Where("space_id=? AND id IN ?", spaceID, orphans).Delete(&types.Entity{}).Error; err != nil {
		return err
	}
	return nil
}

func deleteEntryAssociations(ctx context.Context, db *gorm.DB, spaceID uint64, ids []string) error {
	if db == nil || len(ids) == 0 {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("space_id = ? AND entry_id IN ?", spaceID, ids).Delete(&types.EntryEntity{}).Error; err != nil {
			return err
		}
		return nil
	})
}
