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

func (s *knowledgeSearchService) mergeArticleHits(ctx context.Context, tenantID uint64, lanes [][]articleSearchHit, config *types.RetrievalConfig) ([]*types.ArticleSearchResult, error) {
	vectorWeight, keywordWeight := config.GetEffectiveRRFWeights()
	weights := []float64{vectorWeight / 2, vectorWeight / 2, keywordWeight}
	scores := map[string]float64{}
	metadataMatch := map[string]string{}
	snippets := map[string]map[string]types.ArticleSearchSnippet{}
	for lane, hits := range lanes {
		ranks := map[string]int{}
		for index, hit := range hits {
			if _, exists := ranks[hit.KnowledgeID]; !exists {
				rank := len(ranks) + 1
				ranks[hit.KnowledgeID] = rank
				scores[hit.KnowledgeID] += weights[lane] / float64(config.GetEffectiveRRFK()+rank)
			}
			if hit.ChunkID == "" {
				metadataMatch[hit.KnowledgeID] = articleSearchMatchName(hit.MatchType)
				continue
			}
			if snippets[hit.KnowledgeID] == nil {
				snippets[hit.KnowledgeID] = map[string]types.ArticleSearchSnippet{}
			}
			part := snippets[hit.KnowledgeID][hit.ChunkID]
			part.ChunkID, part.Content, part.ChunkIndex = hit.ChunkID, hit.Content, hit.ChunkIndex
			part.Score += weights[lane] / float64(config.GetEffectiveRRFK()+index+1)
			part.MatchType = articleSearchMatchName(hit.MatchType)
			snippets[hit.KnowledgeID][hit.ChunkID] = part
		}
	}
	results := make([]*types.ArticleSearchResult, 0, len(scores))
	if len(scores) == 0 {
		return results, nil
	}
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	type articleRow struct {
		types.Knowledge   `gorm:"embedded"`
		SearchPublishedAt *time.Time
	}
	var articles []articleRow
	if err := s.db.WithContext(ctx).Table("knowledges").Where("tenant_id=? AND id IN ? AND deleted_at IS NULL", tenantID, ids).Find(&articles).Error; err != nil {
		return nil, err
	}
	entities, topics, err := s.articleSearchAssociations(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	for _, article := range articles {
		source, err := articleSourceInfo(&article.Knowledge)
		if err != nil {
			return nil, err
		}
		originalURL := source.OriginalURL
		if originalURL == "" && (strings.HasPrefix(article.Source, "https://") || strings.HasPrefix(article.Source, "http://")) {
			originalURL = article.Source
		}
		result := &types.ArticleSearchResult{
			ArticleID: article.ID, Title: article.Title, Source: article.SourceKind,
			PublishedAt: article.SearchPublishedAt, ContentType: article.ContentType,
			ProcessingMode: article.ProcessingMode, Summary: article.StructuredSummary, Starred: article.Starred,
			Entities: entities[article.ID], Topics: topics[article.ID],
			Score: scores[article.ID], SimilarReports: []*types.ArticleSearchResult{},
			VaultPath: article.VaultPath, OriginalURL: originalURL,
			Snippets: []types.ArticleSearchSnippet{},
		}
		if result.Entities == nil {
			result.Entities = []types.ArticleSearchEntity{}
		}
		if result.Topics == nil {
			result.Topics = []types.ArticleSearchTopic{}
		}
		for _, snippet := range snippets[article.ID] {
			result.Snippets = append(result.Snippets, snippet)
		}
		slices.SortFunc(result.Snippets, func(a, b types.ArticleSearchSnippet) int {
			if a.Score > b.Score {
				return -1
			}
			if a.Score < b.Score {
				return 1
			}
			return strings.Compare(a.ChunkID, b.ChunkID)
		})
		if len(result.Snippets) > 3 {
			result.Snippets = result.Snippets[:3]
		}
		if len(result.Snippets) == 0 {
			text := article.StructuredSummary.RenderText()
			if text == "" {
				text = article.Description
			}
			if text == "" {
				text = article.Title
			}
			result.Snippets = append(result.Snippets, types.ArticleSearchSnippet{
				Content: text, ChunkIndex: -1, Score: result.Score, MatchType: metadataMatch[article.ID],
			})
		}
		results = append(results, result)
	}
	slices.SortFunc(results, compareArticleScores)
	return results, nil
}

func articleSearchMatchName(matchType types.MatchType) string {
	if matchType == types.MatchTypeKeywords {
		return "keyword"
	}
	return "vector"
}

func (s *knowledgeSearchService) articleSearchAssociations(ctx context.Context, tenantID uint64, ids []string) (map[string][]types.ArticleSearchEntity, map[string][]types.ArticleSearchTopic, error) {
	type entityRow struct {
		KnowledgeID               string
		types.ArticleSearchEntity `gorm:"embedded"`
	}
	var rows []entityRow
	err := s.db.WithContext(ctx).Table("entities e").
		Joins("JOIN knowledge_entities ke ON ke.entity_id=e.id AND ke.tenant_id=e.tenant_id").
		Where("e.tenant_id=? AND ke.knowledge_id IN ?", tenantID, ids).
		Select("ke.knowledge_id,e.id,e.canonical_name,e.entity_type").
		Order("e.canonical_name,e.id").Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	entityIDs := []string{}
	for _, row := range rows {
		entityIDs = append(entityIDs, row.ID)
	}
	aliases, err := s.articleEntityAliases(ctx, tenantID, entityIDs)
	if err != nil {
		return nil, nil, err
	}
	entities := map[string][]types.ArticleSearchEntity{}
	for _, row := range rows {
		row.Aliases = aliases[row.ID]
		if row.Aliases == nil {
			row.Aliases = []string{}
		}
		entities[row.KnowledgeID] = append(entities[row.KnowledgeID], row.ArticleSearchEntity)
	}
	type topicRow struct {
		KnowledgeID              string
		types.ArticleSearchTopic `gorm:"embedded"`
	}
	var topicRows []topicRow
	err = s.db.WithContext(ctx).Table("topics t").
		Joins("JOIN knowledge_topics kt ON kt.topic_id=t.id AND kt.tenant_id=t.tenant_id").
		Where("t.tenant_id=? AND kt.knowledge_id IN ?", tenantID, ids).
		Select("kt.knowledge_id,t.id,t.name").Order("t.name,t.id").Scan(&topicRows).Error
	topics := map[string][]types.ArticleSearchTopic{}
	for _, row := range topicRows {
		topics[row.KnowledgeID] = append(topics[row.KnowledgeID], row.ArticleSearchTopic)
	}
	return entities, topics, err
}

func (s *knowledgeSearchService) articleEntityAliases(ctx context.Context, tenantID uint64, ids []string) (map[string][]string, error) {
	aliases := map[string][]string{}
	if len(ids) == 0 {
		return aliases, nil
	}
	var rows []types.EntityAlias
	err := s.db.WithContext(ctx).Where("tenant_id=? AND entity_id IN ?", tenantID, ids).Order("alias,id").Find(&rows).Error
	for _, row := range rows {
		aliases[row.EntityID] = append(aliases[row.EntityID], row.Alias)
	}
	return aliases, err
}

func (s *knowledgeSearchService) Options(ctx context.Context) (*types.ArticleSearchOptions, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return nil, apperrors.NewUnauthorizedError("workspace ID not found in context")
	}
	if s.db == nil {
		return nil, fmt.Errorf("article search requires PostgreSQL")
	}
	options := &types.ArticleSearchOptions{Entities: []types.ArticleSearchEntity{}, Topics: []types.ArticleSearchTopic{}}
	if err := s.db.WithContext(ctx).Table("entities").Where("tenant_id=? AND merged_into_id=''", tenantID).
		Select("id,canonical_name,entity_type").Order("canonical_name,id").Scan(&options.Entities).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(options.Entities))
	for _, entity := range options.Entities {
		ids = append(ids, entity.ID)
	}
	aliases, err := s.articleEntityAliases(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	for i := range options.Entities {
		options.Entities[i].Aliases = aliases[options.Entities[i].ID]
		if options.Entities[i].Aliases == nil {
			options.Entities[i].Aliases = []string{}
		}
	}
	err = s.db.WithContext(ctx).Table("topics").Where("tenant_id=?", tenantID).
		Select("id,name").Order("name,id").Scan(&options.Topics).Error
	return options, err
}

func (s *knowledgeSearchService) collapseArticleReports(ctx context.Context, tenantID uint64, results []*types.ArticleSearchResult, config *types.RetrievalConfig) ([]*types.ArticleSearchResult, error) {
	ids := []string{}
	for _, result := range results {
		if result.Source == "article" && result.PublishedAt != nil {
			ids = append(ids, result.ArticleID)
		}
	}
	if len(ids) < 2 {
		return results, nil
	}
	type vectorRow struct {
		KnowledgeID string
		ModelID     string
		Dimension   int
		Embedding   pgvector.HalfVector
	}
	var rows []vectorRow
	if err := s.db.WithContext(ctx).Table("article_embeddings").Where("tenant_id=? AND knowledge_id IN ?", tenantID, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	vectors := map[string]vectorRow{}
	for _, row := range rows {
		vectors[row.KnowledgeID] = row
	}
	window := time.Duration(config.GetEffectiveSimilarReportsWindowDays()) * 24 * time.Hour
	threshold := config.GetEffectiveSimilarReportsThreshold()
	similar := func(a, b *types.ArticleSearchResult) bool {
		if a.Source != "article" || b.Source != "article" || a.PublishedAt == nil || b.PublishedAt == nil {
			return false
		}
		delta := a.PublishedAt.Sub(*b.PublishedAt)
		if delta > window || delta < -window {
			return false
		}
		av, aok := vectors[a.ArticleID]
		bv, bok := vectors[b.ArticleID]
		return aok && bok && av.ModelID == bv.ModelID && av.Dimension == bv.Dimension &&
			articleCosineSimilarity(av.Embedding.Slice(), bv.Embedding.Slice()) >= threshold
	}
	groups := []*types.ArticleSearchResult{}
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
