package service

import (
	"context"
	"fmt"
	"reliquary/internal/sourceloc"
	"reliquary/internal/types"
	"slices"
	"strings"
	"time"

	apperrors "reliquary/internal/errors"
)

func (s *searchService) mergeEntryHits(ctx context.Context, spaceID uint64, lanes [][]entrySearchHit, config *types.RetrievalConfig) ([]*types.EntrySearchResult, error) {
	vectorWeight, keywordWeight := config.GetEffectiveRRFWeights()
	weights := []float64{vectorWeight / 2, vectorWeight / 2, keywordWeight, config.GetEffectiveRRFImageWeight()}
	scores := map[string]float64{}
	metadataMatch := map[string]string{}
	snippets := map[string]map[string]types.EntrySearchSnippet{}
	for lane, hits := range lanes {
		ranks := map[string]int{}
		for index, hit := range hits {
			if _, exists := ranks[hit.EntryID]; !exists {
				rank := len(ranks) + 1
				ranks[hit.EntryID] = rank
				scores[hit.EntryID] += weights[lane] / float64(config.GetEffectiveRRFK()+rank)
			}
			if hit.ChunkID == "" && hit.ImagePath == "" {
				metadataMatch[hit.EntryID] = entrySearchMatchName(hit.MatchType)
				continue
			}
			if snippets[hit.EntryID] == nil {
				snippets[hit.EntryID] = map[string]types.EntrySearchSnippet{}
			}
			key := hit.ChunkID
			if hit.ImagePath != "" {
				key = "image:" + hit.ImagePath
			}
			part := snippets[hit.EntryID][key]
			part.ChunkID, part.Content, part.ChunkIndex = hit.ChunkID, hit.Content, hit.ChunkIndex
			part.Score += weights[lane] / float64(config.GetEffectiveRRFK()+index+1)
			part.MatchType = entrySearchMatchName(hit.MatchType)
			if hit.ImagePath != "" {
				part.ImagePath, part.ChunkIndex, part.MatchType = hit.ImagePath, -1, "image"
			}
			snippets[hit.EntryID][key] = part
		}
	}
	results := make([]*types.EntrySearchResult, 0, len(scores))
	if len(scores) == 0 {
		return results, nil
	}
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	type entryRow struct {
		types.Entry       `gorm:"embedded"`
		SearchPublishedAt *time.Time
	}
	var entries []entryRow
	if err := s.db.WithContext(ctx).Table("entries").Where("space_id=? AND id IN ? AND deleted_at IS NULL", spaceID, ids).Find(&entries).Error; err != nil {
		return nil, err
	}
	entities, err := s.entrySearchAssociations(ctx, spaceID, ids)
	if err != nil {
		return nil, err
	}
	var libraries []types.Library
	if err := s.db.WithContext(ctx).Where("space_id=?", spaceID).Find(&libraries).Error; err != nil {
		return nil, err
	}
	var chunks []types.Chunk
	chunkIDs := []string{}
	for _, parts := range snippets {
		for _, part := range parts {
			if part.ChunkID != "" {
				chunkIDs = append(chunkIDs, part.ChunkID)
			}
		}
	}
	if len(chunkIDs) > 0 {
		if err := s.db.WithContext(ctx).Select("id", "source_locations", "chunk_type", "image_info").Where("space_id=? AND id IN ?", spaceID, chunkIDs).Find(&chunks).Error; err != nil {
			return nil, err
		}
	}
	locations := map[string][]types.SourceLocation{}
	chunkTypes := map[string]string{}
	for _, c := range chunks {
		locations[c.ID] = c.SourceLocations
		chunkTypes[c.ID] = c.ChunkType
	}
	for _, entry := range entries {
		source, err := entrySourceInfo(&entry.Entry)
		if err != nil {
			return nil, err
		}
		originalURL := source.OriginalURL
		if originalURL == "" && (strings.HasPrefix(entry.Source, "https://") || strings.HasPrefix(entry.Source, "http://")) {
			originalURL = entry.Source
		}
		if entry.SourceUnverified {
			originalURL = ""
		}
		result := &types.EntrySearchResult{
			LibraryID:     entry.LibraryID,
			SourceRef:     entry.SourceRef,
			Identifier:    entry.Identifier,
			HasUpdate:     entry.HasUpdate,
			LatestEntryID: entry.LatestEntryID,

			EntryID: entry.ID, Title: entry.Title, Source: entry.SourceKind,
			PublishedAt: entry.SearchPublishedAt,
			Acquisition: entry.Acquisition, CannotBeFact: entry.SourceKind == "report",
			Summary: entry.StructuredSummary, Starred: entry.Starred,
			SourceUnverified: entry.SourceUnverified,
			Entities:         entities[entry.ID],
			Score:            scores[entry.ID],
			VaultPath:        entry.VaultPath, OriginalURL: originalURL,
			Snippets: []types.EntrySearchSnippet{},
		}
		for _, lib := range libraries {
			if lib.ID == entry.LibraryID {
				result.LibraryName, result.LibraryDirectory = lib.Name, lib.Directory
			}
		}
		if entry.SourceRef != "" {
			latest, err := types.FindLatestSourceEntry(s.db.WithContext(ctx), spaceID, entry.LibraryID, entry.SourceRef)
			if err != nil {
				return nil, err
			}
			if latest != nil {
				entry.HasUpdate, entry.LatestEntryID = latest.ID != entry.ID, latest.ID
			}
		}
		result.HasUpdate, result.LatestEntryID = entry.HasUpdate, entry.LatestEntryID
		if result.Entities == nil {
			result.Entities = []types.EntrySearchEntity{}
		}
		for _, snippet := range snippets[entry.ID] {
			snippet.Locations = locations[snippet.ChunkID]
			snippet.ChunkType = chunkTypes[snippet.ChunkID]
			if snippet.ImagePath != "" {
				for _, ref := range bodyImageReferences(entry.VaultBody) {
					if ref.URL != snippet.ImagePath {
						continue
					}
					offset := ref.Position + strings.Index(entry.VaultBody[ref.Position:], ref.URL)
					start := len([]rune(entry.VaultBody[:offset]))
					c := &types.Chunk{Content: ref.URL, StartAt: start, EndAt: start + len([]rune(ref.URL))}
					sourceloc.Assign(entry.VaultBody, []*types.Chunk{c})
					snippet.Locations = append(snippet.Locations, c.SourceLocations...)
				}
			}
			if snippet.Locations == nil {
				snippet.Locations = []types.SourceLocation{}
			}
			result.Snippets = append(result.Snippets, snippet)
		}
		slices.SortFunc(result.Snippets, func(a, b types.EntrySearchSnippet) int {
			if a.Score > b.Score {
				return -1
			}
			if a.Score < b.Score {
				return 1
			}
			return strings.Compare(a.ChunkID+a.ImagePath, b.ChunkID+b.ImagePath)
		})
		if len(result.Snippets) > 3 {
			// Preserve one matched image even when three text passages score higher.
			// The thumbnail explains why this additional channel recalled the entry.
			if !slices.ContainsFunc(result.Snippets[:3], func(snippet types.EntrySearchSnippet) bool { return snippet.ImagePath != "" }) {
				for _, snippet := range result.Snippets[3:] {
					if snippet.ImagePath != "" {
						result.Snippets[2] = snippet
						break
					}
				}
			}
			result.Snippets = result.Snippets[:3]
		}
		if len(result.Snippets) == 0 {
			text := entry.StructuredSummary.RenderText()
			if text == "" {
				text = entry.Description
			}
			if text == "" {
				text = entry.Title
			}
			result.Snippets = append(result.Snippets, types.EntrySearchSnippet{
				Locations: []types.SourceLocation{}, Content: text, ChunkIndex: -1, Score: result.Score, MatchType: metadataMatch[entry.ID],
			})
		}
		results = append(results, result)
	}
	slices.SortFunc(results, compareEntryScores)
	return results, nil
}

func entrySearchMatchName(matchType types.MatchType) string {
	if matchType == types.MatchTypeKeywords {
		return "keyword"
	}
	return "vector"
}

func (s *searchService) entrySearchAssociations(
	ctx context.Context,
	spaceID uint64,
	ids []string,
) (map[string][]types.EntrySearchEntity, error) {
	type entityRow struct {
		EntryID                 string
		types.EntrySearchEntity `gorm:"embedded"`
	}
	var rows []entityRow
	err := s.db.WithContext(ctx).Table("entities e").
		Joins("JOIN entry_entities ke ON ke.entity_id=e.id AND ke.space_id=e.space_id").
		Where("e.space_id=? AND ke.entry_id IN ?", spaceID, ids).
		Select("ke.entry_id,e.id,e.canonical_name,e.entity_type").
		Order("e.canonical_name,e.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	entityIDs := []string{}
	for _, row := range rows {
		entityIDs = append(entityIDs, row.ID)
	}
	aliases, err := s.entryEntityAliases(ctx, spaceID, entityIDs)
	if err != nil {
		return nil, err
	}
	entities := map[string][]types.EntrySearchEntity{}
	for _, row := range rows {
		row.Aliases = aliases[row.ID]
		if row.Aliases == nil {
			row.Aliases = []string{}
		}
		entities[row.EntryID] = append(entities[row.EntryID], row.EntrySearchEntity)
	}

	return entities, nil
}

func (s *searchService) entryEntityAliases(ctx context.Context, spaceID uint64, ids []string) (map[string][]string, error) {
	aliases := map[string][]string{}
	if len(ids) == 0 {
		return aliases, nil
	}
	var rows []types.EntityAlias
	err := s.db.WithContext(ctx).Where("space_id=? AND entity_id IN ?", spaceID, ids).Order("alias,id").Find(&rows).Error
	for _, row := range rows {
		aliases[row.EntityID] = append(aliases[row.EntityID], row.Alias)
	}
	return aliases, err
}

func (s *searchService) Options(ctx context.Context) (*types.EntrySearchOptions, error) {
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok {
		return nil, apperrors.NewUnauthorizedError("workspace ID not found in context")
	}
	if s.db == nil {
		return nil, fmt.Errorf("entry search requires PostgreSQL")
	}
	options := &types.EntrySearchOptions{Entities: []types.EntrySearchEntity{}}
	if err := s.db.WithContext(ctx).Table("entities").Where("space_id=? AND merged_into_id=''", spaceID).
		Select("id,canonical_name,entity_type").Order("canonical_name,id").Scan(&options.Entities).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(options.Entities))
	for _, entity := range options.Entities {
		ids = append(ids, entity.ID)
	}
	aliases, err := s.entryEntityAliases(ctx, spaceID, ids)
	if err != nil {
		return nil, err
	}
	for i := range options.Entities {
		options.Entities[i].Aliases = aliases[options.Entities[i].ID]
		if options.Entities[i].Aliases == nil {
			options.Entities[i].Aliases = []string{}
		}
	}

	return options, nil
}
