package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"
)

type articleSearchHit struct {
	KnowledgeID string          `gorm:"column:knowledge_id"`
	ChunkID     string          `gorm:"column:chunk_id"`
	ChunkIndex  int             `gorm:"column:chunk_index"`
	Content     string          `gorm:"column:content"`
	Score       float64         `gorm:"column:score"`
	MatchType   types.MatchType `gorm:"-"`
}

const articleSearchableChunks = `c.deleted_at IS NULL AND (c.is_enabled IS NULL OR c.is_enabled = true)
	AND c.chunk_type IN ('text','summary','table_summary','table_column')`

func (s *knowledgeSearchService) articleChunkVectorQuery(ctx context.Context, tenantID uint64, kbID string, f articleSearchFilters, dates bool, vector []float32, config *types.RetrievalConfig) *gorm.DB {
	expression := fmt.Sprintf("e.embedding::halfvec(%d) <=> ?::halfvec(%d)", len(vector), len(vector))
	return s.eligibleArticles(ctx, tenantID, kbID, f, dates).
		Joins("JOIN chunks c ON c.knowledge_id=k.id AND c.tenant_id=k.tenant_id").
		Joins("JOIN embeddings e ON e.chunk_id=c.id AND e.knowledge_id=k.id").
		Where(articleSearchableChunks).
		Where("k.processing_mode='full' AND e.dimension=? AND e.embedding IS NOT NULL AND (e.is_enabled IS NULL OR e.is_enabled=true)", len(vector)).
		Where(expression+" <= ?", pgvector.NewHalfVector(vector), 1-config.GetEffectiveVectorThreshold())
}

func (s *knowledgeSearchService) articleVectorQuery(ctx context.Context, tenantID uint64, kbID string, f articleSearchFilters, dates bool, vector []float32, modelID string, config *types.RetrievalConfig) *gorm.DB {
	expression := fmt.Sprintf("a.embedding::halfvec(%d) <=> ?::halfvec(%d)", len(vector), len(vector))
	return s.eligibleArticles(ctx, tenantID, kbID, f, dates).
		Joins("JOIN article_embeddings a ON a.knowledge_id=k.id AND a.tenant_id=k.tenant_id").
		Where("k.source_kind='article' AND a.dimension=? AND a.model_id=?", len(vector), modelID).
		Where(expression+" <= ?", pgvector.NewHalfVector(vector), 1-config.GetEffectiveVectorThreshold())
}

func (s *knowledgeSearchService) articleChunkKeywordQuery(ctx context.Context, tenantID uint64, kbID string, f articleSearchFilters, dates bool) *gorm.DB {
	return s.eligibleArticles(ctx, tenantID, kbID, f, dates).
		Joins("JOIN chunks c ON c.knowledge_id=k.id AND c.tenant_id=k.tenant_id").
		Where(articleSearchableChunks).
		Where("k.processing_mode='full'").
		Where(`c.seq_id @@@ paradedb.match_disjunction('content', ?)`, f.request.Query)
}

func (s *knowledgeSearchService) articleHeaderKeywordQuery(ctx context.Context, tenantID uint64, kbID string, f articleSearchFilters, dates bool) *gorm.DB {
	return s.eligibleArticles(ctx, tenantID, kbID, f, dates).
		Where(`k.search_seq_id @@@ paradedb.boolean(should => ARRAY[
			paradedb.match_disjunction('title', ?),
			paradedb.match_disjunction('description', ?),
			paradedb.match_disjunction('search_text', ?)])`, f.request.Query, f.request.Query, f.request.Query)
}

func (s *knowledgeSearchService) retrieveArticleLanes(ctx context.Context, tenantID uint64, kb *types.KnowledgeBase, f articleSearchFilters, vector []float32, modelID string, config *types.RetrievalConfig, pool int, dates bool) ([][]articleSearchHit, error) {
	lanes := make([][]articleSearchHit, 3)
	if len(vector) > 0 {
		expression := fmt.Sprintf("e.embedding::halfvec(%d) <=> ?::halfvec(%d)", len(vector), len(vector))
		query := s.articleChunkVectorQuery(ctx, tenantID, kb.ID, f, dates, vector, config).
			Where("k.embedding_model_id=?", modelID).
			Select("k.id AS knowledge_id,c.id AS chunk_id,c.chunk_index,c.content,1-("+expression+") AS score,ROW_NUMBER() OVER (PARTITION BY k.id ORDER BY "+expression+",c.id) AS article_rank", pgvector.NewHalfVector(vector), pgvector.NewHalfVector(vector))
		if err := s.db.WithContext(ctx).Table("(?) AS candidates", query).Where("article_rank <= 3").
			Order("score DESC,chunk_id ASC").Limit(poolLimit(pool)).Scan(&lanes[0]).Error; err != nil {
			return nil, err
		}
		expression = fmt.Sprintf("a.embedding::halfvec(%d) <=> ?::halfvec(%d)", len(vector), len(vector))
		query = s.articleVectorQuery(ctx, tenantID, kb.ID, f, dates, vector, modelID, config)
		if err := query.Select("k.id AS knowledge_id,1-("+expression+") AS score", pgvector.NewHalfVector(vector)).
			Order("score DESC, k.id ASC").Limit(poolLimit(pool)).Scan(&lanes[1]).Error; err != nil {
			return nil, err
		}
	}
	if kb.IsKeywordEnabled() {
		var chunks, headers []articleSearchHit
		query := s.articleChunkKeywordQuery(ctx, tenantID, kb.ID, f, dates).
			Select("k.id AS knowledge_id,c.id AS chunk_id,c.chunk_index,c.content,paradedb.score(c.seq_id) AS score,ROW_NUMBER() OVER (PARTITION BY k.id ORDER BY paradedb.score(c.seq_id) DESC,c.id) AS article_rank")
		if err := s.db.WithContext(ctx).Table("(?) AS candidates", query).Where("article_rank <= 3").
			Order("score DESC,chunk_id ASC").Limit(poolLimit(pool)).Scan(&chunks).Error; err != nil {
			return nil, err
		}
		if err := s.articleHeaderKeywordQuery(ctx, tenantID, kb.ID, f, dates).
			Select("k.id AS knowledge_id,paradedb.score(k.search_seq_id) AS score").
			Order("score DESC,k.id ASC").Limit(poolLimit(pool)).Scan(&headers).Error; err != nil {
			return nil, err
		}
		lanes[2] = append(chunks, headers...)
		slices.SortStableFunc(lanes[2], func(a, b articleSearchHit) int {
			if a.Score > b.Score {
				return -1
			}
			if a.Score < b.Score {
				return 1
			}
			return 0
		})
		if pool > 0 && len(lanes[2]) > pool {
			lanes[2] = lanes[2][:pool]
		}
		pointers := make([]*articleSearchHit, len(lanes[2]))
		for i := range lanes[2] {
			pointers[i] = &lanes[2][i]
		}
		searchutil.NormalizeKeywordScores(pointers, func(*articleSearchHit) bool { return true },
			func(hit *articleSearchHit) float64 { return hit.Score },
			func(hit *articleSearchHit, score float64) { hit.Score = score }, searchutil.KeywordScoreCallbacks{})
		lanes[2] = slices.DeleteFunc(lanes[2], func(hit articleSearchHit) bool { return hit.Score < config.GetEffectiveKeywordThreshold() })
	}
	for lane, hits := range lanes {
		for i := range hits {
			lanes[lane][i].MatchType = types.MatchTypeEmbedding
			if lane == 2 {
				lanes[lane][i].MatchType = types.MatchTypeKeywords
			}
		}
	}
	return lanes, nil
}

func poolLimit(pool int) int {
	if pool == 0 {
		return -1
	}
	return pool
}

// Count query matches before publication filtering, ranking limits, and folding.
// Documents in mine/reports have no article publication date and are not counted.
func (s *knowledgeSearchService) countMissingArticleDates(ctx context.Context, tenantID uint64, kb *types.KnowledgeBase, f articleSearchFilters, vector []float32, modelID string, config *types.RetrievalConfig) (int64, error) {
	lanes, err := s.retrieveArticleLanes(ctx, tenantID, kb, f, vector, modelID, config, 0, false)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, hits := range lanes {
		for _, hit := range hits {
			if !seen[hit.KnowledgeID] {
				seen[hit.KnowledgeID] = true
				ids = append(ids, hit.KnowledgeID)
			}
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	var count int64
	err = s.db.WithContext(ctx).Table("knowledges").Where("tenant_id=? AND id IN ? AND source_kind='article' AND search_published_at IS NULL", tenantID, ids).Count(&count).Error
	return count, err
}
