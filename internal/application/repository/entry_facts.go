package repository

import (
	"context"
	"reliquary/internal/types"
	"strconv"

	"gorm.io/gorm"
)

// WithdrawEntryFacts removes both roles, including manual links. Call within the
// content replacement or deletion transaction, before changing source metadata.
func WithdrawEntryFacts(ctx context.Context, tx *gorm.DB, spaceID uint64, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	// Use the same space barrier as fact grouping and manual corrections, before entry row locks.
	if err := tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
		"reliquary:entities:team:"+strconv.FormatUint(spaceID, 10)).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Exec(`UPDATE facts f SET recall_date=COALESCE(
 (SELECT min(NULLIF(substring(e.metadata#>>'{intake,source_info,published_at}',1,10),'')::date)
  FROM entry_facts ef JOIN entries e ON e.id=ef.entry_id AND e.space_id=ef.space_id
  WHERE ef.space_id=f.space_id AND ef.fact_id=f.id AND ef.role='source' AND e.deleted_at IS NULL), f.recall_date)
 WHERE f.space_id=? AND f.id IN
 (SELECT fact_id FROM entry_facts WHERE space_id=? AND entry_id IN ? AND role='source')`,
		spaceID, spaceID, ids).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Where("space_id=? AND entry_id IN ?", spaceID, ids).Delete(&types.EntryFact{}).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Where("space_id=? AND entry_id IN ?", spaceID, ids).Delete(&types.EntityCandidateEntry{}).Error
}
