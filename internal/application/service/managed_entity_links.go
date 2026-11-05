package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type ManagedEntityLink struct {
	Entity   types.Entity           `json:"entity"`
	Origin   string                 `json:"origin"`
	Excluded bool                   `json:"excluded"`
	Evidence []types.SourceLocation `json:"evidence"`
	BodyHash string                 `json:"body_hash"`
}

func (s *EntityService) EntryEntityLinks(ctx context.Context, entryID string) ([]ManagedEntityLink, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	var entry types.Entry
	if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", entryID, spaceID).First(&entry).Error; err != nil {
		return nil, managedWriteError(err)
	}
	var links []types.EntryEntity
	if err := s.db.WithContext(ctx).Where("entry_id=? AND space_id=?", entryID, spaceID).Order("entity_id").Find(&links).Error; err != nil {
		return nil, err
	}
	result := make([]ManagedEntityLink, 0, len(links))
	for _, link := range links {
		var entity types.Entity
		if err := s.db.WithContext(ctx).Where("id=? AND library_id=? AND space_id=?", link.EntityID, entry.LibraryID, spaceID).First(&entity).Error; err != nil {
			return nil, err
		}
		if link.Evidence == nil || link.BodyHash != fmt.Sprintf("%x", sha256.Sum256([]byte(entry.VaultBody))) {
			link.Evidence = []types.SourceLocation{}
		}
		result = append(result, ManagedEntityLink{Entity: entity, Origin: link.Origin, Excluded: link.Excluded, Evidence: link.Evidence, BodyHash: link.BodyHash})
	}
	return result, nil
}

func (s *EntityService) SetEntryEntityLink(ctx context.Context, entryID, entityID string, excluded bool) error {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return apperrors.NewUnauthorizedError("entity editor required")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		var entry types.Entry
		if err := tx.Where("id=? AND space_id=?", entryID, spaceID).First(&entry).Error; err != nil {
			return err
		}
		if _, err := managedLibrary(tx, spaceID, entry.LibraryID); err != nil {
			return err
		}
		// The entity barrier precedes the entry lock, matching reprocessing.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", entryID, spaceID).First(&entry).Error; err != nil {
			return err
		}
		var entity types.Entity
		if err := tx.Where("id=? AND space_id=? AND library_id=? AND merged_into_id=''", entityID, spaceID, entry.LibraryID).First(&entity).Error; err != nil {
			return err
		}
		if !excluded && entity.ArchivedAt != nil {
			return apperrors.NewBadRequestError("archived entities cannot receive new document associations")
		}
		var before types.EntryEntity
		err := tx.Where("entry_id=? AND entity_id=? AND space_id=?", entryID, entityID, spaceID).First(&before).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		link := types.EntryEntity{SpaceID: spaceID, EntryID: entryID, EntityID: entityID, Origin: "manual", Excluded: excluded, Evidence: []types.SourceLocation{}, BodyHash: fmt.Sprintf("%x", sha256.Sum256([]byte(entry.VaultBody))), CreatedAt: time.Now()}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entry_id"}, {Name: "entity_id"}}, DoUpdates: clause.AssignmentColumns([]string{"origin", "excluded", "evidence", "body_hash"})}).Create(&link).Error; err != nil {
			return err
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, "set_entity_link", entryID, before, link)
	})
	if err == nil && s.headerRefresher != nil {
		s.headerRefresher.ScheduleRefresh(ctx, entryID)
	}
	return managedWriteError(err)
}
