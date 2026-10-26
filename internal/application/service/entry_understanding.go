package service

import (
	"context"
	"errors"
	"reliquary/internal/types"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type domainReclassificationKey struct{}

var errManualDomainChoice = errors.New("domains_manual")

type understandingDomainOption struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

type understandingEntityOption struct {
	ID            string   `json:"id"`
	CanonicalName string   `json:"canonical_name"`
	EntityType    string   `json:"entity_type"`
	Aliases       []string `json:"aliases"        gorm:"-"`
}

func loadUnderstandingOptions(ctx context.Context, db *gorm.DB, spaceID uint64, input *articleProcessingInput) error {
	input.DomainOptions, input.EntityOptions = []understandingDomainOption{}, []understandingEntityOption{}
	if db == nil {
		return nil
	}
	if err := db.WithContext(ctx).Model(&types.Domain{}).Select("id,name,definition").
		Where("space_id=?", spaceID).Order("name,id").Find(&input.DomainOptions).Error; err != nil {
		return err
	}
	if err := db.WithContext(ctx).Model(&types.Entity{}).Select("id,canonical_name,entity_type").
		Where("space_id=? AND merged_into_id=''", spaceID).
		Order("canonical_name,id").Find(&input.EntityOptions).Error; err != nil {
		return err
	}
	var aliases []types.EntityAlias
	if err := db.WithContext(ctx).Select("entity_id,alias").Where("space_id=?", spaceID).
		Order("alias,id").Find(&aliases).Error; err != nil {
		return err
	}
	byID := map[string][]string{}
	for _, alias := range aliases {
		byID[alias.EntityID] = append(byID[alias.EntityID], alias.Alias)
	}
	for i := range input.EntityOptions {
		input.EntityOptions[i].Aliases = byID[input.EntityOptions[i].ID]
	}
	return nil
}

// Lock the maintained catalogues before the entry, as their editing services do.
// Recheck manual choices after the model finishes, including explicit empty choices.
func (s *entryService) saveUnderstandingChoices(
	ctx context.Context,
	k *types.Entry,
	result *articleProcessingResult,
	domainsOnly bool,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !domainsOnly {
			if err := lockEntitySpace(tx, k.SpaceID); err != nil {
				return err
			}
		}
		if err := lockDomainSpace(tx, k.SpaceID); err != nil {
			return err
		}
		var current types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("space_id=? AND id=?", k.SpaceID, k.ID).First(&current).Error; err != nil {
			return err
		}
		if domainsOnly && current.DomainsManual {
			return errManualDomainChoice
		}
		var input articleProcessingInput
		if err := loadUnderstandingOptions(ctx, tx, k.SpaceID, &input); err != nil {
			return err
		}
		qualifyUnderstandingChoices(result, input)
		if !current.DomainsManual {
			if err := tx.Where("space_id=? AND entry_id=?", k.SpaceID, k.ID).
				Delete(&types.EntryDomain{}).Error; err != nil {
				return err
			}
			for _, id := range result.DomainIDs {
				link := types.EntryDomain{SpaceID: k.SpaceID, EntryID: k.ID, DomainID: id, CreatedAt: time.Now()}
				if err := tx.Create(&link).Error; err != nil {
					return err
				}
			}
		}
		if !domainsOnly && !current.EntitiesManual {
			if err := tx.Where("space_id=? AND entry_id=?", k.SpaceID, k.ID).
				Delete(&types.EntryEntity{}).Error; err != nil {
				return err
			}
			for _, id := range result.EntityIDs {
				link := types.EntryEntity{SpaceID: k.SpaceID, EntryID: k.ID, EntityID: id, CreatedAt: time.Now()}
				if err := tx.Create(&link).Error; err != nil {
					return err
				}
			}
		}
		if !domainsOnly {
			return refreshEntryEntityCandidates(tx, current, result.EntityMentions)
		}
		return nil
	})
}
