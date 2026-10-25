package service

import (
	"context"
	"reflect"
	"reliquary/internal/config"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"time"

	"gorm.io/gorm"
)

// StoryConsolidator reconciles automatically grouped stories.
type StoryConsolidator struct {
	db     *gorm.DB
	config *config.Config
	models interfaces.ModelService
}

// NewStoryConsolidator constructs the periodic story reconciler.
func NewStoryConsolidator(db *gorm.DB, cfg *config.Config, models interfaces.ModelService) *StoryConsolidator {
	return &StoryConsolidator{db: db, config: cfg, models: models}
}

// Consolidate compares roots of bridged stories before reconciling their timelines.
// Model calls run outside the write transaction. Membership and evidence are checked again under the space lock.
func (s *StoryConsolidator) Consolidate(ctx context.Context) error {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return err
	}
	paused, err := maintenanceops.SnapshotPaused(ctx, s.db)
	if err != nil || paused {
		return err
	}
	db := s.db.WithContext(ctx)
	var pairs []storyPair
	if err := bridgePairs(db, spaceID, 1).Order("story_id,other_story_id").Scan(&pairs).Error; err != nil {
		return err
	}
	for _, pair := range pairs {
		if err := s.consolidatePair(ctx, spaceID, pair); err != nil {
			return err
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		paused, err := maintenanceops.SnapshotPaused(ctx, tx)
		if err != nil || paused {
			return err
		}
		return tx.Exec(`INSERT INTO story_links(space_id,story_id,other_story_id)
 SELECT ?,pairs.story_id,pairs.other_story_id FROM (?) pairs
 JOIN stories a ON a.id=pairs.story_id AND a.space_id=? AND a.merged_into_id=''
 JOIN stories b ON b.id=pairs.other_story_id AND b.space_id=? AND b.merged_into_id=''
 ON CONFLICT DO NOTHING`, spaceID, bridgePairs(tx, spaceID, 2), spaceID, spaceID).Error
	})
}

type consolidationRoot struct {
	types.Fact
	EntryID         string
	LibraryID       string
	SourceCreatedAt time.Time
}

func loadConsolidationRoot(db *gorm.DB, spaceID uint64, id string) (*consolidationRoot, error) {
	// Protect the whole membership, including hidden facts and manually retained source-free facts.
	var protected int64
	if err := db.Table("stories s").Where("s.space_id=? AND s.id=?", spaceID, id).
		Where(`s.merged_into_id<>'' OR s.manual_corrected OR EXISTS
 (SELECT 1 FROM facts f WHERE f.space_id=s.space_id AND f.story_id=s.id
 AND (f.manual_corrected OR EXISTS (SELECT 1 FROM entry_facts ef
 JOIN entries e ON e.id=ef.entry_id AND e.space_id=ef.space_id
 WHERE ef.space_id=f.space_id AND ef.fact_id=f.id AND (ef.manual OR e.grouping_manual))))`).
		Count(&protected).Error; err != nil {
		return nil, err
	}
	if protected > 0 {
		return nil, nil
	}
	var roots []consolidationRoot
	err := db.Table("facts f").Select("f.*,ef.entry_id,e.library_id,ef.created_at AS source_created_at").
		Joins("JOIN entry_facts ef ON ef.fact_id=f.id AND ef.space_id=f.space_id AND ef.role='source'").
		Joins("JOIN entries e ON e.id=ef.entry_id AND e.space_id=ef.space_id AND e.deleted_at IS NULL").
		Where("f.space_id=? AND f.story_id=? AND f.merged_into_id=''", spaceID, id).
		Order("f.created_at,f.id,ef.created_at,ef.entry_id").Limit(1).Scan(&roots).Error
	if err != nil || len(roots) == 0 {
		return nil, err
	}
	return &roots[0], nil
}

func (s *StoryConsolidator) consolidatePair(ctx context.Context, spaceID uint64, pair storyPair) error {
	db := s.db.WithContext(ctx)
	left, err := loadConsolidationRoot(db, spaceID, pair.StoryID)
	if err != nil || left == nil {
		return err
	}
	right, err := loadConsolidationRoot(db, spaceID, pair.OtherStoryID)
	if err != nil || right == nil {
		return err
	}
	if right.CreatedAt.Before(left.CreatedAt) ||
		(right.CreatedAt.Equal(left.CreatedAt) && right.StoryID < left.StoryID) {
		left, right = right, left
	}
	var library types.Library
	if err := db.Where("id=? AND space_id=?", left.LibraryID, spaceID).First(&library).Error; err != nil {
		return err
	}
	model, err := GetTierChatModel(ctx, db, s.models, &library, types.ModelTierGeneration)
	if err != nil {
		return err
	}
	ctx = types.WithLLMCallMetadata(ctx, "story_consolidation", "")
	_, threshold := s.config.GroupingThresholds()
	// A bridge alone cannot merge stories. Both root comparisons must agree on a direct tie.
	for _, payload := range []map[string]any{
		{"left": left.Fact, "right": right.Fact, "incoming_side": "right", "mention_only": false},
		{"left": right.Fact, "right": left.Fact, "incoming_side": "left", "mention_only": false},
	} {
		answer, err := callFactGrouping(ctx, model, payload)
		if err != nil {
			return err
		}
		if (answer.Result != groupingSameOccurrence && answer.Result != groupingSameStory) ||
			*answer.Confidence < threshold {
			return nil
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		paused, err := maintenanceops.SnapshotPaused(ctx, tx)
		if err != nil || paused {
			return err
		}
		for _, before := range []*consolidationRoot{left, right} {
			after, err := loadConsolidationRoot(tx, spaceID, before.StoryID)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(before, after) {
				return nil
			}
		}
		var count int64
		if err := tx.Table("(?) pairs", bridgePairs(tx, spaceID, 1)).
			Where("story_id=? AND other_story_id=?", pair.StoryID, pair.OtherStoryID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		return mergeStoryInto(tx, spaceID, right.StoryID, left.StoryID)
	})
}
