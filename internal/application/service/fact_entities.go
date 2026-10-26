package service

import (
	"context"
	"reliquary/internal/types"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Validate against the exact directory snapshot offered to each model call.
func qualifyFactEntityIDs(fact *understandingFact, options []understandingEntityOption) {
	if fact == nil {
		return
	}
	allowed := map[string]bool{}
	for _, option := range options {
		allowed[option.ID] = true
	}
	if !allowed[fact.SubjectEntityID] {
		fact.SubjectEntityID = ""
	}
	if !allowed[fact.ObjectEntityID] {
		fact.ObjectEntityID = ""
	}
}

// Explicit IDs are checked against the maintained directory. Name fallback must
// identify exactly one entity, even when several aliases share a key.
func resolveFactEntity(name, proposedID string, options []understandingEntityOption) *string {
	key := types.NormalizeEntityKey(name)
	matches := map[string]bool{}
	for _, option := range options {
		if proposedID != "" && option.ID == proposedID {
			id := option.ID
			return &id
		}
		if key == "" {
			continue
		}
		names := append([]string{option.CanonicalName}, option.Aliases...)
		for _, candidate := range names {
			if types.NormalizeEntityKey(candidate) == key {
				matches[option.ID] = true
			}
		}
	}
	if len(matches) == 1 {
		for id := range matches {
			return &id
		}
	}
	return nil
}

func recordFactCandidates(tx *gorm.DB, fact types.Fact) error {
	for _, end := range []struct {
		name, endpoint string
		id             *string
	}{
		{fact.Subject, "subject", fact.SubjectEntityID},
		{fact.Object, "object", fact.ObjectEntityID},
	} {
		name := strings.TrimSpace(end.name)
		key := types.NormalizeEntityKey(name)
		if end.id != nil || key == "" || utf8.RuneCountInString(name) > 512 ||
			utf8.RuneCountInString(key) > 512 || strings.ContainsAny(name, "\r\n\x00") {
			continue
		}
		now := time.Now()
		candidate := types.EntityCandidate{
			ID: uuid.NewString(), SpaceID: fact.SpaceID, Name: name,
			NormalizedName: key, Status: "pending", CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "space_id"}, {Name: "normalized_name"}},
			DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
		}).Create(&candidate).Error; err != nil {
			return err
		}
		candidate = types.EntityCandidate{}
		if err := tx.Where("space_id=? AND normalized_name=?", fact.SpaceID, key).First(&candidate).Error; err != nil {
			return err
		}
		occurrence := types.EntityCandidateOccurrence{
			SpaceID: fact.SpaceID, CandidateID: candidate.ID,
			FactID: fact.ID, Endpoint: end.endpoint, CreatedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&occurrence).Error; err != nil {
			return err
		}
	}
	return nil
}

// Manual alias edits perform one exact pass over unlinked historical endpoints.
// Candidate review deliberately uses occurrence records instead of this scan.
func backfillFactAliases(ctx context.Context, tx *gorm.DB, entity types.Entity, aliases []string) error {
	keys := map[string]bool{}
	for _, alias := range aliases {
		keys[types.NormalizeEntityKey(alias)] = true
	}
	if len(keys) == 0 {
		return nil
	}
	var options articleProcessingInput
	if err := loadUnderstandingOptions(ctx, tx, entity.SpaceID, &options); err != nil {
		return err
	}
	var facts []types.Fact
	if err := tx.Where("space_id=? AND (subject_entity_id IS NULL OR object_entity_id IS NULL)", entity.SpaceID).
		Find(&facts).Error; err != nil {
		return err
	}
	subjects, objects := []string{}, []string{}
	for _, fact := range facts {
		if fact.SubjectEntityID == nil && keys[types.NormalizeEntityKey(fact.Subject)] {
			if id := resolveFactEntity(fact.Subject, "", options.EntityOptions); id != nil && *id == entity.ID {
				subjects = append(subjects, fact.ID)
			}
		}
		if fact.ObjectEntityID == nil && keys[types.NormalizeEntityKey(fact.Object)] {
			if id := resolveFactEntity(fact.Object, "", options.EntityOptions); id != nil && *id == entity.ID {
				objects = append(objects, fact.ID)
			}
		}
	}
	for column, ids := range map[string][]string{"subject_entity_id": subjects, "object_entity_id": objects} {
		// Bound parameter counts for long-lived libraries.
		for start := 0; start < len(ids); start += 500 {
			if err := tx.Model(&types.Fact{}).
				Where("space_id=? AND id IN ?", entity.SpaceID, ids[start:min(start+500, len(ids))]).
				Where(column+" IS NULL").Update(column, entity.ID).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
