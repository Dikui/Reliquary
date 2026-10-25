package service

import (
	"context"
	"reliquary/internal/types"

	"gorm.io/gorm"
)

func deleteEntryAssociations(ctx context.Context, db *gorm.DB, spaceID uint64, ids []string) error {
	if db == nil || len(ids) == 0 {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("space_id = ? AND entry_id IN ?", spaceID, ids).Delete(&types.EntryEntity{}).Error; err != nil {
			return err
		}
		return tx.Where("space_id=? AND entry_id IN ?", spaceID, ids).Delete(&types.EntryDomain{}).Error
	})
}
