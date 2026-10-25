package service

import (
	"context"
	"reliquary/internal/types"

	"gorm.io/gorm"
)

func factsWithSources(ctx context.Context, db *gorm.DB, spaceID uint64, facts []types.Fact) ([]types.StoryFact, error) {
	items := make([]types.StoryFact, 0, len(facts))
	if len(facts) == 0 {
		return items, nil
	}
	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.ID)
	}
	var sources []struct {
		FactID           string
		types.FactSource `gorm:"embedded"`
	}
	err := db.WithContext(ctx).Table("entry_facts ef").
		Select(`ef.fact_id,ef.entry_id,e.library_id,e.title,ef.role,ef.quote,ef.manual,
 COALESCE(e.metadata#>>'{intake,source_info,original_url}',e.metadata->>'original_url','') AS original_url`).
		Joins("JOIN entries e ON e.id=ef.entry_id AND e.space_id=ef.space_id AND e.deleted_at IS NULL").
		Where("ef.space_id=? AND ef.fact_id IN ? AND ef.role='source'", spaceID, ids).
		Order("ef.created_at,ef.entry_id").Scan(&sources).Error
	if err != nil {
		return nil, err
	}
	byFact := map[string][]types.FactSource{}
	for _, source := range sources {
		byFact[source.FactID] = append(byFact[source.FactID], source.FactSource)
	}
	for _, fact := range facts {
		item := types.StoryFact{Fact: fact, Sources: []types.FactSource{}, NoSource: len(byFact[fact.ID]) == 0}
		item.Sources = append(item.Sources, byFact[fact.ID]...)
		items = append(items, item)
	}
	return items, nil
}
