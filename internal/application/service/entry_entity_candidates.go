package service

import (
	"reliquary/internal/types"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The caller holds the entity directory and entry locks with the accepted understanding.
func refreshEntryEntityCandidates(tx *gorm.DB, entry types.Entry, mentions []understandingEntityMention) error {
	if err := tx.Where("space_id=? AND entry_id=?", entry.SpaceID, entry.ID).
		Delete(&types.EntityCandidateEntry{}).Error; err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, mention := range mentions {
		name, kind := strings.TrimSpace(mention.Name), types.NormalizeEntityType(mention.Type)
		key := types.NormalizeEntityKey(name)
		if mention.EntityID != "" || kind == "" || key == "" || seen[key] || strings.TrimSpace(mention.Quote) == "" ||
			utf8.RuneCountInString(name) > 512 || utf8.RuneCountInString(key) > 512 || strings.ContainsAny(name, "\r\n\x00") {
			continue
		}
		seen[key] = true
		now := time.Now()
		candidate := types.EntityCandidate{
			ID: uuid.NewString(), SpaceID: entry.SpaceID, Name: name, NormalizedName: key,
			EntityType: kind, Status: "pending", CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "space_id"}, {Name: "normalized_name"}},
			DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
		}).Create(&candidate).Error; err != nil {
			return err
		}
		candidate = types.EntityCandidate{}
		if err := tx.Where("space_id=? AND normalized_name=?", entry.SpaceID, key).First(&candidate).Error; err != nil {
			return err
		}
		if err := tx.Create(&types.EntityCandidateEntry{
			SpaceID: entry.SpaceID, CandidateID: candidate.ID, EntryID: entry.ID, Quote: mention.Quote, CreatedAt: now,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// Reviews attach only to recorded, current sources and preserve manual choices.
func attachCandidateEntries(tx *gorm.DB, candidate types.EntityCandidate) error {
	return tx.Exec(`INSERT INTO entry_entities (space_id,entry_id,entity_id,created_at)
 SELECT e.space_id,e.id,?,? FROM entity_candidate_entries ce
 JOIN entries e ON e.space_id=ce.space_id AND e.id=ce.entry_id
 WHERE ce.space_id=? AND ce.candidate_id=? AND e.deleted_at IS NULL AND NOT e.entities_manual
 AND (SELECT count(*) FROM entry_entities ee WHERE ee.space_id=e.space_id AND ee.entry_id=e.id)<3
 ON CONFLICT (entry_id,entity_id) DO NOTHING`,
		*candidate.EntityID, time.Now(), candidate.SpaceID, candidate.ID).Error
}
