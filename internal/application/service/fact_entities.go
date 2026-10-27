package service

import (
	"context"
	"reliquary/internal/types"

	"gorm.io/gorm"
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

// Manual alias edits perform one exact pass over unlinked historical endpoints.
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
