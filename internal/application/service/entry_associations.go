package service

import (
	"context"

	"reliquary/internal/types"

	"gorm.io/gorm"
)

// populateEntryLibraryNames uses stored names, matching search results. Localized
// display_name belongs to the library catalog, not article metadata.
func populateEntryLibraryNames(ctx context.Context, db *gorm.DB, entries []*types.Entry) error {
	if db == nil || len(entries) == 0 {
		return nil
	}
	type key struct {
		space   uint64
		library string
	}
	ids := map[string]bool{}
	spaces := map[uint64]bool{}
	var visit func([]*types.Entry)
	visit = func(items []*types.Entry) {
		for _, entry := range items {
			if entry == nil {
				continue
			}
			ids[entry.LibraryID] = true
			spaces[entry.SpaceID] = true
			visit(entry.SimilarReports)
		}
	}
	visit(entries)
	libraryIDs := make([]string, 0, len(ids))
	spaceIDs := make([]uint64, 0, len(spaces))
	for id := range ids {
		libraryIDs = append(libraryIDs, id)
	}
	for id := range spaces {
		spaceIDs = append(spaceIDs, id)
	}
	var libraries []types.Library
	if err := db.WithContext(ctx).Select("id", "space_id", "name").Where("space_id IN ? AND id IN ?", spaceIDs, libraryIDs).Find(&libraries).Error; err != nil {
		return err
	}
	names := map[key]string{}
	for _, library := range libraries {
		names[key{library.SpaceID, library.ID}] = library.Name
	}
	var fill func([]*types.Entry)
	fill = func(items []*types.Entry) {
		for _, entry := range items {
			if entry == nil {
				continue
			}
			entry.LibraryName = names[key{entry.SpaceID, entry.LibraryID}]
			entry.CannotBeFact = entry.SourceKind == "report"
			fill(entry.SimilarReports)
		}
	}
	fill(entries)
	return nil
}

// loadEntryAssociations batches library, entity and topic data for entry responses.
func (s *entryService) loadEntryAssociations(ctx context.Context, spaceID uint64, entries []*types.Entry) error {
	if err := populateEntryLibraryNames(ctx, s.db, entries); err != nil {
		return err
	}
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
		Where("ke.space_id=? AND ke.entry_id IN ? AND e.merged_into_id='' AND ke.excluded=false", spaceID, ids).
		Order("e.canonical_name,e.id").Find(&entityRows).Error; err != nil {
		return err
	}
	for _, row := range entityRows {
		if entry := byID[row.EntryID]; entry != nil && entry.LibraryID == row.Entity.LibraryID {
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
		if entry := byID[row.EntryID]; entry != nil && entry.LibraryID == row.Topic.LibraryID {
			entry.Topics = append(entry.Topics, row.Topic)
		}
	}
	return nil
}
