package service

import (
	"context"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"

	"gorm.io/gorm"
)

// MergeStories transfers facts while retaining old story links.
func (s *StoryService) MergeStories(ctx context.Context, id, keepID string) (*types.StoryDetail, error) {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated correction actor required")
	}
	if id == keepID {
		return nil, werrors.NewBadRequestError("two distinct story IDs required")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		merged, err := canonicalStory(tx, spaceID, id)
		if err != nil {
			return err
		}
		keep, err := canonicalStory(tx, spaceID, keepID)
		if err != nil {
			return err
		}
		if merged.ID == keep.ID {
			return nil
		}
		var factIDs []string
		if err := tx.Model(&types.Fact{}).
			Where("space_id=? AND story_id IN ? AND merged_into_id=''", spaceID, []string{merged.ID, keep.ID}).
			Pluck("id", &factIDs).Error; err != nil {
			return err
		}
		if len(factIDs) > 0 {
			if err := markGroupingManual(tx, spaceID, factIDs); err != nil {
				return err
			}
		}
		if err := tx.Model(&types.Story{}).Where("space_id=? AND (id=? OR merged_into_id=?)",
			spaceID, merged.ID, merged.ID).
			Update("manual_corrected", true).Error; err != nil {
			return err
		}
		if err := mergeStoryInto(tx, spaceID, merged.ID, keep.ID); err != nil {
			return err
		}
		if err := tx.Model(&types.Story{}).Where("space_id=? AND id=?", spaceID, keep.ID).
			Update("manual_corrected", true).Error; err != nil {
			return err
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, "merge_stories", keep.ID,
			map[string]any{
				"keep":     keep,
				"merged":   merged,
				"fact_ids": factIDs,
			}, map[string]any{"keep_story_id": keep.ID, "merged_story_id": merged.ID})
	})
	if err != nil {
		return nil, err
	}
	return s.StoryDetail(ctx, keepID)
}

// mergeStoryInto is shared by deliberate corrections and the automatic reconciler.
// Callers hold the space lock and apply their own manual-protection policy.
func mergeStoryInto(tx *gorm.DB, spaceID uint64, fromID, intoID string) error {
	if err := tx.Model(&types.Fact{}).Where("space_id=? AND story_id=?", spaceID, fromID).
		Update("story_id", intoID).Error; err != nil {
		return err
	}
	if err := tx.Model(&types.Story{}).Where("space_id=? AND (id=? OR merged_into_id=?)", spaceID, fromID, fromID).
		Update("merged_into_id", intoID).Error; err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO story_links(space_id,story_id,other_story_id)
 SELECT space_id,LEAST(a,b),GREATEST(a,b) FROM
 (SELECT space_id,CASE WHEN story_id=? THEN ? ELSE story_id END AS a,
 CASE WHEN other_story_id=? THEN ? ELSE other_story_id END AS b
 FROM story_links WHERE space_id=? AND (story_id=? OR other_story_id=?)) moved
 WHERE a<>b ON CONFLICT DO NOTHING`, fromID, intoID, fromID, intoID, spaceID, fromID, fromID).Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM story_links WHERE space_id=? AND (story_id=? OR other_story_id=?)",
		spaceID, fromID, fromID).Error; err != nil {
		return err
	}
	if err := transferMergedStoryFollows(tx, spaceID, fromID, intoID); err != nil {
		return err
	}
	return refreshStoryTimes(tx, spaceID, intoID)
}

// transferMergedStoryFollows is the transactional integration point for task 13.
// Story follows do not exist yet. Transfer and deduplication must join this transaction when added.
func transferMergedStoryFollows(_ *gorm.DB, _ uint64, _, _ string) error { return nil }
