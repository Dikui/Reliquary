package service

import (
	"context"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"

	"gorm.io/gorm"
)

// MergeFacts retains one fact and moves all evidence, mentions and candidate occurrences to it.
func (s *StoryService) MergeFacts(ctx context.Context, id, keepID string) (*types.StoryFact, error) {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated correction actor required")
	}
	if id == keepID {
		return nil, werrors.NewBadRequestError("two distinct fact IDs required")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		merged, err := canonicalFact(tx, spaceID, id)
		if err != nil {
			return err
		}
		keep, err := canonicalFact(tx, spaceID, keepID)
		if err != nil {
			return err
		}
		if merged.ID == keep.ID {
			return nil
		}
		if err := markGroupingManual(tx, spaceID, []string{merged.ID, keep.ID}); err != nil {
			return err
		}
		// Delete the donor sources before inserting: each material has at most one source fact.
		if err := tx.Exec(`WITH moved AS (
 DELETE FROM entry_facts WHERE space_id=? AND fact_id=? RETURNING *)
 INSERT INTO entry_facts(space_id,entry_id,fact_id,role,quote,manual,created_at)
 SELECT space_id,entry_id,?,role,quote,true,created_at FROM moved
 ON CONFLICT(entry_id,fact_id) DO UPDATE SET
 role=CASE WHEN EXCLUDED.role='source' THEN 'source' ELSE entry_facts.role END,
 quote=CASE WHEN EXCLUDED.role='source' THEN EXCLUDED.quote ELSE entry_facts.quote END,
 manual=true,created_at=LEAST(entry_facts.created_at,EXCLUDED.created_at)`,
			spaceID, merged.ID, keep.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&types.Fact{}).Where("space_id=? AND (id=? OR merged_into_id=?)",
			spaceID, merged.ID, merged.ID).
			Updates(map[string]any{"merged_into_id": keep.ID, "manual_corrected": true}).Error; err != nil {
			return err
		}
		for _, sid := range []string{merged.StoryID, keep.StoryID} {
			if err := tx.Model(&types.Story{}).Where("space_id=? AND id=?", spaceID, sid).
				Update("manual_corrected", true).Error; err != nil {
				return err
			}
			if err := refreshStoryTimes(tx, spaceID, sid); err != nil {
				return err
			}
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, "merge_facts", keep.ID,
			map[string]any{
				"keep":   keep,
				"merged": merged,
			}, map[string]any{"keep_fact_id": keep.ID, "merged_fact_id": merged.ID})
	})
	if err != nil {
		return nil, err
	}
	return s.FactDetail(ctx, keepID)
}
