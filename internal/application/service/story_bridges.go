package service

import (
	"reliquary/internal/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func saveGroupingEvidence(tx *gorm.DB, link types.EntryFact, decisions []types.GroupingDecision) error {
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error; err != nil {
		return err
	}
	if link.Role != "source" {
		return nil
	}
	for _, d := range decisions {
		if d.AppliedResult != groupingSameOccurrence && d.AppliedResult != groupingSameStory {
			continue
		}
		if err := tx.Exec(`INSERT INTO story_bridge_evidence
 (decision_id,space_id,source_fact_id,source_created_at) VALUES(?,?,?,?)`,
			d.ID, link.SpaceID, link.FactID, link.CreatedAt).Error; err != nil {
			return err
		}
	}
	return nil
}

// Evidence follows current fact membership, including flattened manual merge redirects.
// Matching the original source timestamp prevents a later revision from reviving an old tie.
// Mentions and source-free facts never supply bridge evidence.
func validStoryBridges(db *gorm.DB, spaceID uint64) *gorm.DB {
	return db.Raw(`SELECT DISTINCT d.entry_id,
 LEAST(own.story_id,other.story_id) AS story_id,
 GREATEST(own.story_id,other.story_id) AS other_story_id
 FROM story_bridge_evidence b
 JOIN grouping_decisions d ON d.id=b.decision_id AND d.space_id=b.space_id
 JOIN facts original ON original.id=b.source_fact_id AND original.space_id=b.space_id
 JOIN facts own ON own.id=COALESCE(NULLIF(original.merged_into_id,''),original.id) AND own.space_id=b.space_id
 JOIN entry_facts ef ON ef.fact_id=own.id AND ef.space_id=b.space_id AND ef.entry_id=d.entry_id
   AND ef.role='source' AND ef.created_at=b.source_created_at
 JOIN entries e ON e.id=ef.entry_id AND e.space_id=ef.space_id AND e.deleted_at IS NULL
 JOIN facts candidate ON candidate.id=d.candidate_fact_id AND candidate.space_id=d.space_id
 JOIN facts other ON other.id=COALESCE(NULLIF(candidate.merged_into_id,''),candidate.id)
   AND other.space_id=b.space_id
 WHERE b.space_id=? AND own.story_id<>other.story_id
 AND d.applied_result IN ('same_occurrence','same_story')
 AND NOT EXISTS (SELECT 1 FROM grouping_decisions newer
   WHERE newer.space_id=d.space_id AND newer.entry_id=d.entry_id AND newer.candidate_fact_id=d.candidate_fact_id
   AND (newer.created_at,newer.id)>(d.created_at,d.id))
 AND EXISTS (SELECT 1 FROM entry_facts evidence
   JOIN entries material ON material.id=evidence.entry_id AND material.space_id=evidence.space_id
   WHERE evidence.space_id=other.space_id AND evidence.fact_id=other.id
   AND evidence.role='source' AND material.deleted_at IS NULL)`, spaceID)
}

type storyPair struct{ StoryID, OtherStoryID string }

func bridgePairs(db *gorm.DB, spaceID uint64, minimum int) *gorm.DB {
	return db.Table("(?) bridges", validStoryBridges(db, spaceID)).
		Select("story_id,other_story_id").Group("story_id,other_story_id").
		Having("count(DISTINCT entry_id)>=?", minimum)
}
