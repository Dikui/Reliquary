package service

import (
	"context"
	"errors"
	"reliquary/internal/types"
	"time"

	werrors "reliquary/internal/errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetEntryEntities records an explicit manual choice, including the empty choice.
func (s *EntityService) SetEntryEntities(ctx context.Context, entryID string, ids []string) (*types.Entry, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated entity editor required")
	}
	if len(ids) > 3 {
		return nil, werrors.NewBadRequestError("at most 3 entities")
	}
	var entry types.Entry
	entities := []types.Entity{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=? AND id=?",
			spaceID,
			entryID).First(&entry).Error; errors.Is(
			err,
			gorm.ErrRecordNotFound,
		) {
			return werrors.NewNotFoundError("entry not found")
		} else if err != nil {
			return err
		}
		if len(ids) > 0 {
			if err := tx.Where("space_id=? AND id IN ? AND merged_into_id=''",
				spaceID,
				ids).Order("canonical_name,id").Find(&entities).Error; err != nil {
				return err
			}
			if len(entities) != len(ids) {
				return werrors.NewBadRequestError("entity IDs must be distinct existing entities")
			}
		}
		if err := tx.Where("space_id=? AND entry_id=?",
			spaceID,
			entryID).Delete(&types.EntryEntity{}).Error; err != nil {
			return err
		}
		for _, entity := range entities {
			if err := tx.Create(&types.EntryEntity{
				SpaceID:   spaceID,
				EntryID:   entryID,
				EntityID:  entity.ID,
				CreatedAt: time.Now(),
			}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&entry).Update("entities_manual", true).Error
	})
	if err != nil {
		return nil, err
	}
	entry.Entities, entry.EntitiesManual = entities, true
	return &entry, nil
}
