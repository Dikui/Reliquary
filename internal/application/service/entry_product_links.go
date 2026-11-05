package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/types"
)

// Product matching is separate from local extraction: it only reads master
// records and never changes the local graph's resolved entity IDs.
func (s *entryService) associateProductMentions(ctx context.Context, entry *types.Entry, body string, localIDs ...string) ([]string, error) {
	ids := []string{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpaceShared(tx, entry.SpaceID); err != nil {
			return err
		}
		var documentLibrary types.Library
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND space_id=?", entry.LibraryID, entry.SpaceID).First(&documentLibrary).Error; err != nil {
			return err
		}
		matcher, err := s.productMatchers.load(tx, &documentLibrary)
		if err != nil {
			return err
		}
		var current types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", entry.ID, entry.SpaceID).First(&current).Error; err != nil {
			return err
		}
		// Libraries without product names still need automatic-link cleanup,
		// including rebuilds that do not publish a body.
		if matcher.pattern != nil && current.VaultBody != body {
			return fmt.Errorf("entry body changed during product matching")
		}
		evidence := matcher.evidence(body)
		bodyHash := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
		for id, locations := range evidence {
			encoded, err := json.Marshal(locations)
			if err != nil {
				return err
			}
			link := types.EntryEntity{SpaceID: entry.SpaceID, EntryID: entry.ID, EntityID: id, Origin: "extracted", RelationType: "references", Evidence: locations, BodyHash: bodyHash, CreatedAt: time.Now()}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entry_id"}, {Name: "entity_id"}}, DoUpdates: clause.Assignments(map[string]any{"evidence": types.JSON(encoded), "body_hash": link.BodyHash}), Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "entry_entities.origin<>'manual' AND entry_entities.excluded=false"}}}}).Create(&link).Error; err != nil {
				return err
			}
			ids = append(ids, id)
		}
		// Replace automatic links before releasing the identity barrier, so a
		// concurrent product merge cannot have its newly moved links deleted.
		keepIDs := append(append([]string{}, localIDs...), ids...)
		query := tx.Where("space_id=? AND entry_id=? AND origin<>?", entry.SpaceID, entry.ID, "manual")
		if len(keepIDs) > 0 {
			query = query.Where("entity_id NOT IN ?", keepIDs)
		}
		return query.Delete(&types.EntryEntity{}).Error
	})
	return ids, err
}
