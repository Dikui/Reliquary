package service

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
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
	entities, topics, err := s.entrySearchAssociations(ctx, spaceID, ids)
	if err != nil {
		return nil, err
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
			EntryID: entry.ID, Title: entry.Title, Source: entry.SourceKind,
			PublishedAt: entry.SearchPublishedAt, ContentType: entry.ContentType,
			Acquisition:    entry.Acquisition,
			Incomplete:     (entry.SourceKind == "article" || entry.SourceKind == "mine") && entry.Incomplete,
			ProcessingMode: entry.ProcessingMode, Summary: entry.StructuredSummary, Starred: entry.Starred,
			SourceUnverified: entry.SourceUnverified,
			Entities:         entities[entry.ID], Topics: topics[entry.ID],
			Score: scores[entry.ID], SimilarReports: []*types.EntrySearchResult{},
			VaultPath: entry.VaultPath, OriginalURL: originalURL,
			Snippets: []types.EntrySearchSnippet{},
		}
		if result.Entities == nil {
			result.Entities = []types.EntrySearchEntity{}
		}
		if result.Topics == nil {
			result.Topics = []types.EntrySearchTopic{}
		}
		for _, snippet := range snippets[entry.ID] {
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
				Content: text, ChunkIndex: -1, Score: result.Score, MatchType: metadataMatch[entry.ID],
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

func (s *searchService) entrySearchAssociations(ctx context.Context, spaceID uint64, ids []string) (map[string][]types.EntrySearchEntity, map[string][]types.EntrySearchTopic, error) {
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
		return nil, nil, err
	}
	entityIDs := []string{}
	for _, row := range rows {
		entityIDs = append(entityIDs, row.ID)
	}
	aliases, err := s.entryEntityAliases(ctx, spaceID, entityIDs)
	if err != nil {
		return nil, nil, err
	}
	entities := map[string][]types.EntrySearchEntity{}
	for _, row := range rows {
		row.Aliases = aliases[row.ID]
		if row.Aliases == nil {
			row.Aliases = []string{}
		}
		entities[row.EntryID] = append(entities[row.EntryID], row.EntrySearchEntity)
	}
	type topicRow struct {
		EntryID                string
		types.EntrySearchTopic `gorm:"embedded"`
	}
	var topicRows []topicRow
	err = s.db.WithContext(ctx).Table("topics t").
		Joins("JOIN entry_topics kt ON kt.topic_id=t.id AND kt.space_id=t.space_id").
		Where("t.space_id=? AND kt.entry_id IN ?", spaceID, ids).
		Select("kt.entry_id,t.id,t.name").Order("t.name,t.id").Scan(&topicRows).Error
	topics := map[string][]types.EntrySearchTopic{}
	for _, row := range topicRows {
		topics[row.EntryID] = append(topics[row.EntryID], row.EntrySearchTopic)
	}
	return entities, topics, err
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
	options := &types.EntrySearchOptions{Entities: []types.EntrySearchEntity{}, Topics: []types.EntrySearchTopic{}}
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
	err = s.db.WithContext(ctx).Table("topics").Where("space_id=?", spaceID).
		Select("id,name").Order("name,id").Scan(&options.Topics).Error
	return options, err
}

func (s *searchService) collapseArticleReports(ctx context.Context, spaceID uint64, results []*types.EntrySearchResult, config *types.RetrievalConfig) ([]*types.EntrySearchResult, error) {
	ids := []string{}
	for _, result := range results {
		if (result.Source == "article" || result.Source == "mine") && result.PublishedAt != nil {
			ids = append(ids, result.EntryID)
		}
	}
	if len(ids) < 2 {
		return results, nil
	}
	type vectorRow struct {
		EntryID   string
		ModelID   string
		Dimension int
		Embedding pgvector.HalfVector
	}
	var rows []vectorRow
	modelID, err := ResolveModelTier(ctx, s.db, nil, types.ModelTierEmbedding)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Table("article_embeddings").Where("space_id=? AND entry_id IN ? AND model_id=?", spaceID, ids, modelID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	vectors := map[string]vectorRow{}
	for _, row := range rows {
		vectors[row.EntryID] = row
	}
	window := time.Duration(config.GetEffectiveSimilarReportsWindowDays()) * 24 * time.Hour
	threshold := config.GetEffectiveSimilarReportsThreshold()
	similar := func(a, b *types.EntrySearchResult) bool {
		if a.Source == "report" || b.Source == "report" || a.PublishedAt == nil || b.PublishedAt == nil {
			return false
		}
		delta := a.PublishedAt.Sub(*b.PublishedAt)
		if delta > window || delta < -window {
			return false
		}
		av, aok := vectors[a.EntryID]
		bv, bok := vectors[b.EntryID]
		return aok && bok && av.ModelID == bv.ModelID && av.Dimension == bv.Dimension &&
			articleCosineSimilarity(av.Embedding.Slice(), bv.Embedding.Slice()) >= threshold
	}
	groups := []*types.EntrySearchResult{}
	for _, result := range results {
		folded := false
		for _, representative := range groups {
			if !similar(representative, result) {
				continue
			}
			// Every pair must satisfy both rules, avoiding long similarity chains.
			compatible := true
			for _, member := range representative.SimilarReports {
				if !similar(member, result) {
					compatible = false
					break
				}
			}
			if compatible {
				representative.SimilarReports = append(representative.SimilarReports, result)
				folded = true
				break
			}
		}
		if !folded {
			groups = append(groups, result)
		}
	}
	return groups, nil
}

func articleCosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return -1
	}
	var dot, aa, bb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		aa += x * x
		bb += y * y
	}
	if aa == 0 || bb == 0 {
		return -1
	}
	return dot / math.Sqrt(aa*bb)
}
