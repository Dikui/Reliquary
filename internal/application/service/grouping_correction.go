package service

import (
	"context"
	"errors"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func canonicalStory(tx *gorm.DB, spaceID uint64, id string) (types.Story, error) {
	var item types.Story
	seen := map[string]bool{}
	for !seen[id] {
		seen[id] = true
		item = types.Story{}
		if err := tx.Where("space_id=? AND id=?", spaceID, id).First(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return item, werrors.NewNotFoundError("story not found")
			}
			return item, err
		}
		if item.MergedIntoID == "" {
			return item, nil
		}
		id = item.MergedIntoID
	}
	return item, werrors.NewBadRequestError("cyclic story redirect")
}

func canonicalFact(tx *gorm.DB, spaceID uint64, id string) (types.Fact, error) {
	var item types.Fact
	seen := map[string]bool{}
	for !seen[id] {
		seen[id] = true
		item = types.Fact{}
		if err := tx.Where("space_id=? AND id=?", spaceID, id).First(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return item, werrors.NewNotFoundError("fact not found")
			}
			return item, err
		}
		if item.MergedIntoID == "" {
			return item, nil
		}
		id = item.MergedIntoID
	}
	return item, werrors.NewBadRequestError("cyclic fact redirect")
}

// FactDetail resolves old merged IDs to the retained fact, including its evidence.
func (s *StoryService) FactDetail(ctx context.Context, id string) (*types.StoryFact, error) {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return nil, err
	}
	fact, err := canonicalFact(s.db.WithContext(ctx), spaceID, id)
	if err != nil {
		return nil, err
	}
	detail, err := s.StoryDetail(ctx, fact.StoryID)
	if err != nil {
		return nil, err
	}
	for _, item := range detail.Facts {
		if item.ID == fact.ID {
			return &item, nil
		}
	}
	return nil, werrors.NewNotFoundError("fact is hidden without source evidence")
}

func markGroupingManual(tx *gorm.DB, spaceID uint64, factIDs []string) error {
	if err := tx.Model(&types.Entry{}).Where(`space_id=? AND id IN
 (SELECT entry_id FROM entry_facts WHERE space_id=? AND fact_id IN ?)`, spaceID, spaceID, factIDs).
		Update("grouping_manual", true).Error; err != nil {
		return err
	}
	if err := tx.Model(&types.EntryFact{}).Where("space_id=? AND fact_id IN ?", spaceID, factIDs).
		Update("manual", true).Error; err != nil {
		return err
	}
	return tx.Model(&types.Fact{}).Where("space_id=? AND id IN ?", spaceID, factIDs).
		Update("manual_corrected", true).Error
}

func refreshStoryTimes(tx *gorm.DB, spaceID uint64, id string) error {
	return tx.Exec(`UPDATE stories SET
 first_fact_at=COALESCE((SELECT min(created_at) FROM facts
 WHERE space_id=? AND story_id=? AND merged_into_id=''),first_fact_at),
 last_fact_at=COALESCE((SELECT max(created_at) FROM facts
 WHERE space_id=? AND story_id=? AND merged_into_id=''),last_fact_at)
 WHERE space_id=? AND id=?`, spaceID, id, spaceID, id, spaceID, id).Error
}

// MoveFact changes a fact's story and protects its material associations from automatic regrouping.
func (s *StoryService) MoveFact(ctx context.Context, id, storyID string) (*types.StoryFact, error) {
	return s.relocateFact(ctx, id, storyID, "")
}

// SplitFact moves one fact into a new event with a user supplied title.
func (s *StoryService) SplitFact(ctx context.Context, id, title string) (*types.StoryFact, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 255 || strings.ContainsAny(title, "\r\n\x00") {
		return nil, werrors.NewBadRequestError("story title must be a single line of 1 to 255 characters")
	}
	return s.relocateFact(ctx, id, "", title)
}

func (s *StoryService) relocateFact(ctx context.Context, id, storyID, title string) (*types.StoryFact, error) {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated correction actor required")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		fact, err := canonicalFact(tx, spaceID, id)
		if err != nil {
			return err
		}
		var story types.Story
		operation := "move_fact"
		if title != "" {
			operation = "split_fact"
			story = types.Story{
				ID: uuid.NewString(), SpaceID: spaceID, Title: title,
				FirstFactAt: fact.CreatedAt, LastFactAt: fact.CreatedAt, ManualCorrected: true,
			}
			if err := tx.Create(&story).Error; err != nil {
				return err
			}
		} else {
			story, err = canonicalStory(tx, spaceID, storyID)
			if err != nil {
				return err
			}
		}
		if fact.StoryID == story.ID {
			return nil
		}
		if err := markGroupingManual(tx, spaceID, []string{fact.ID}); err != nil {
			return err
		}
		if err := tx.Model(&types.Fact{}).Where("space_id=? AND id=?", spaceID, fact.ID).
			Update("story_id", story.ID).Error; err != nil {
			return err
		}
		for _, sid := range []string{fact.StoryID, story.ID} {
			if err := tx.Model(&types.Story{}).Where("space_id=? AND id=?", spaceID, sid).
				Update("manual_corrected", true).Error; err != nil {
				return err
			}
			if err := refreshStoryTimes(tx, spaceID, sid); err != nil {
				return err
			}
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, operation, fact.ID,
			map[string]any{"story_id": fact.StoryID}, map[string]any{"story_id": story.ID})
	})
	if err != nil {
		return nil, err
	}
	return s.FactDetail(ctx, id)
}
