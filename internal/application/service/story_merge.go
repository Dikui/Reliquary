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
		if err := tx.Model(&types.Fact{}).Where("space_id=? AND story_id=?", spaceID, merged.ID).
			Update("story_id", keep.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&types.Story{}).Where("space_id=? AND (id=? OR merged_into_id=?)",
			spaceID, merged.ID, merged.ID).
			Updates(map[string]any{"merged_into_id": keep.ID, "manual_corrected": true}).Error; err != nil {
			return err
		}
		if err := tx.Model(&types.Story{}).Where("space_id=? AND id=?", spaceID, keep.ID).
			Update("manual_corrected", true).Error; err != nil {
			return err
		}
		if err := refreshStoryTimes(tx, spaceID, keep.ID); err != nil {
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
