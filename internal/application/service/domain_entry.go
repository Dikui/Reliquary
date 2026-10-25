package service

import (
	"context"
	"errors"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetEntryDomains records an explicit manual choice, including no domains.
func (s *DomainService) SetEntryDomains(ctx context.Context, entryID string, ids []string) (*types.Entry, error) {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated domain editor required")
	}
	if len(ids) > 3 {
		return nil, werrors.NewBadRequestError("at most 3 domains")
	}
	var entry types.Entry
	domains := []types.Domain{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockDomainSpace(tx, spaceID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("space_id=? AND id=?", spaceID, entryID).First(&entry).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("entry not found")
			}
			return err
		}
		if len(ids) > 0 {
			if err := tx.Where("space_id=? AND id IN ?", spaceID, ids).
				Order("name,id").Find(&domains).Error; err != nil {
				return err
			}
			if len(domains) != len(ids) {
				return werrors.NewBadRequestError("domain IDs must be distinct existing domains")
			}
		}
		if err := tx.Where("space_id=? AND entry_id=?", spaceID, entryID).
			Delete(&types.EntryDomain{}).Error; err != nil {
			return err
		}
		for _, domain := range domains {
			if err := tx.Create(&types.EntryDomain{
				SpaceID:   spaceID,
				EntryID:   entryID,
				DomainID:  domain.ID,
				CreatedAt: time.Now(),
			}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&entry).Update("domains_manual", true).Error
	})
	if err != nil {
		return nil, err
	}
	entry.Domains, entry.DomainsManual = domains, true
	return &entry, nil
}
