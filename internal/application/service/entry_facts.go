package service

import (
	"context"
	"encoding/json"
	"reliquary/internal/types"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The model may propose one fact per material. Invalid facts do not discard its overview.
type understandingFact struct {
	Title           string `json:"title"`
	Subject         string `json:"subject"`
	Action          string `json:"action"`
	Object          string `json:"object"`
	Conditions      string `json:"conditions"`
	OccurredAt      string `json:"occurred_at"`
	Quote           string `json:"quote"`
	SubjectEntityID string `json:"subject_entity_id"`
	ObjectEntityID  string `json:"object_entity_id"`
	SubjectType     string `json:"subject_type"`
	ObjectType      string `json:"object_type"`
}

func materialCanProduceFact(library *types.Library, entry *types.Entry) bool {
	return library.Template == types.TemplateExternalTimely && entry.SourceKind != "report" && !entry.CannotBeFact
}

func parseUnderstandingFact(raw json.RawMessage) *understandingFact {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	var fact understandingFact
	// Optional endpoint fields cannot invalidate otherwise valid evidence.
	for key, target := range map[string]*string{
		"subject_entity_id": &fact.SubjectEntityID, "object_entity_id": &fact.ObjectEntityID,
		"subject_type": &fact.SubjectType, "object_type": &fact.ObjectType,
	} {
		_ = json.Unmarshal(fields[key], target)
		delete(fields, key)
	}
	core, _ := json.Marshal(fields)
	if json.Unmarshal(core, &fact) != nil {
		return nil
	}
	return &fact
}

func qualifyUnderstandingFact(
	library *types.Library,
	entry *types.Entry,
	body string,
	fact *understandingFact,
) *understandingFact {
	if fact == nil || !materialCanProduceFact(library, entry) {
		return nil
	}
	fact.Title = strings.TrimSpace(fact.Title)
	fact.Subject = strings.TrimSpace(fact.Subject)
	fact.Action = strings.TrimSpace(fact.Action)
	if fact.Title == "" || fact.Subject == "" || fact.Action == "" || utf8.RuneCountInString(fact.Title) > 255 ||
		strings.ContainsAny(fact.Title, "\r\n") || strings.TrimSpace(fact.Quote) == "" ||
		!strings.Contains(body, fact.Quote) {
		return nil
	}
	for _, field := range []string{fact.Title, fact.Subject, fact.Action, fact.Object, fact.Conditions, fact.Quote} {
		if strings.ContainsRune(field, '\x00') {
			return nil
		}
	}
	return fact
}

func factOccurrenceTime(value string) *string {
	value = strings.TrimSpace(value)
	for _, format := range []string{time.RFC3339Nano, time.DateOnly} {
		if parsed, err := time.Parse(format, value); err == nil {
			formatted := parsed.Format(format)
			return &formatted
		}
	}
	return nil
}

func (s *entryService) saveUnderstandingFact(
	ctx context.Context, library *types.Library, entry *types.Entry, body string, result *articleProcessingResult,
) error {
	if hasExplicitFactMentions(entry) {
		return nil
	}
	fact := qualifyUnderstandingFact(library, entry, body, result.Fact)
	var existing int64
	if err := s.db.WithContext(ctx).Model(&types.EntryFact{}).
		Where("space_id=? AND entry_id=? AND (role='source' OR manual=true)", entry.SpaceID, entry.ID).
		Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	plan, err := s.planFactGrouping(ctx, library, entry, result, fact)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, entry.SpaceID); err != nil {
			return err
		}
		var current types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("space_id=? AND id=?", entry.SpaceID, entry.ID).First(&current).Error; err != nil {
			return err
		}
		if current.SourceBodyHash != entry.SourceBodyHash {
			return nil
		}
		var count int64
		if err := tx.Model(&types.EntryFact{}).Where("space_id=? AND entry_id=? AND (role='source' OR manual=true)",
			entry.SpaceID, entry.ID).Count(&count).Error; err != nil {
			return err
		}
		// A retry cannot create another fact or change the story's recent-fact time.
		if count > 0 {
			return nil
		}

		for _, decision := range plan.decisions {
			if err := tx.Create(&decision).Error; err != nil {
				return err
			}
		}

		now := time.Now()
		for _, decision := range plan.decisions {
			if decision.AppliedResult == groupingMention {
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&types.EntryFact{
					SpaceID: entry.SpaceID, EntryID: entry.ID, FactID: decision.CandidateFactID,
					Role: "mention", CreatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		}
		if plan.candidate != nil && (plan.result == groupingSameOccurrence || plan.result == groupingMention) {
			role, quote := "mention", ""
			if plan.result == groupingSameOccurrence && fact != nil && materialCanProduceFact(library, &current) {
				role, quote = "source", fact.Quote
			}
			return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&types.EntryFact{
				SpaceID: entry.SpaceID, EntryID: entry.ID, FactID: plan.candidate.ID,
				Role: role, Quote: quote, CreatedAt: now,
			}).Error
		}
		if fact == nil || !materialCanProduceFact(library, &current) {
			return nil
		}
		source, err := entrySourceInfo(&current)
		if err != nil {
			return err
		}
		if plan.result != groupingSameStory && groupingDate(source.PublishedAt) == nil {
			return nil
		}

		story := types.Story{
			ID: uuid.NewString(), SpaceID: entry.SpaceID, Title: fact.Title,
			FirstFactAt: now, LastFactAt: now,
		}
		if plan.result == groupingSameStory && plan.candidate != nil {
			story.ID = plan.candidate.StoryID
			if err := tx.Model(&types.Story{}).Where("space_id=? AND id=?", entry.SpaceID, story.ID).
				Update("last_fact_at", now).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&story).Error; err != nil {
			return err
		}
		stored := types.Fact{
			ID: uuid.NewString(), SpaceID: entry.SpaceID, StoryID: story.ID,
			Title: fact.Title, Subject: fact.Subject, Action: fact.Action, Object: fact.Object,
			Conditions: fact.Conditions, OccurredAt: factOccurrenceTime(fact.OccurredAt), CreatedAt: now,
		}
		var options articleProcessingInput
		if err := loadUnderstandingOptions(ctx, tx, entry.SpaceID, &options); err != nil {
			return err
		}
		stored.SubjectEntityID = resolveFactEntity(fact.Subject, fact.SubjectEntityID, options.EntityOptions)
		stored.ObjectEntityID = resolveFactEntity(fact.Object, fact.ObjectEntityID, options.EntityOptions)
		if err := tx.Create(&stored).Error; err != nil {
			return err
		}
		if err := recordFactCandidates(tx, stored, fact); err != nil {
			return err
		}
		return tx.Create(&types.EntryFact{
			SpaceID: entry.SpaceID, EntryID: entry.ID, FactID: stored.ID,
			Role: "source", Quote: fact.Quote, CreatedAt: now,
		}).Error
	})
}
