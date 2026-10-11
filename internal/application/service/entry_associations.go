package service

import (
	"context"

	"reliquary/internal/types"
)

// loadEntryAssociations batches entity and topic display data for entry responses.
func (s *entryService) loadEntryAssociations(ctx context.Context, spaceID uint64, entries []*types.Entry) error {
	byID := make(map[string]*types.Entry, len(entries))
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		entry.Entities = []types.Entity{}
		entry.Topics = []types.Topic{}
		byID[entry.ID] = entry
		ids = append(ids, entry.ID)
	}
	if s.db == nil || len(ids) == 0 {
		return nil
	}
	var entityRows []struct {
		EntryID string       `gorm:"column:entry_id"`
		Entity  types.Entity `gorm:"embedded"`
	}
	if err := s.db.WithContext(ctx).Table("entry_entities AS ke").
		Select("ke.entry_id, e.*").Joins("JOIN entities AS e ON e.space_id=ke.space_id AND e.id=ke.entity_id").
		Where("ke.space_id=? AND ke.entry_id IN ? AND e.merged_into_id=''", spaceID, ids).
		Order("e.canonical_name,e.id").Find(&entityRows).Error; err != nil {
		return err
	}
	for _, row := range entityRows {
		if entry := byID[row.EntryID]; entry != nil {
			entry.Entities = append(entry.Entities, row.Entity)
		}
	}
	var topicRows []struct {
		EntryID string      `gorm:"column:entry_id"`
		Topic   types.Topic `gorm:"embedded"`
	}
	if err := s.db.WithContext(ctx).Table("entry_topics AS kt").
		Select("kt.entry_id, t.*").Joins("JOIN topics AS t ON t.space_id=kt.space_id AND t.id=kt.topic_id").
		Where("kt.space_id=? AND kt.entry_id IN ?", spaceID, ids).
		Order("t.name,t.id").Find(&topicRows).Error; err != nil {
		return err
	}
	for _, row := range topicRows {
		if entry := byID[row.EntryID]; entry != nil {
			entry.Topics = append(entry.Topics, row.Topic)
		}
	}
	return nil
}
