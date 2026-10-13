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

type entrySearchHit struct {
	EntryID    string          `gorm:"column:entry_id"`
	ChunkID    string          `gorm:"column:chunk_id"`
	ChunkIndex int             `gorm:"column:chunk_index"`
	Content    string          `gorm:"column:content"`
	Score      float64         `gorm:"column:score"`
	MatchType  types.MatchType `gorm:"-"`
	ImagePath  string          `gorm:"column:image_path"`
}

const entrySearchableChunks = `c.deleted_at IS NULL AND (c.is_enabled IS NULL OR c.is_enabled = true)
	AND c.chunk_type IN ('text','summary','table_summary','table_column')`

func (s *searchService) entryChunkVectorQuery(ctx context.Context, spaceID uint64, kbID string, f entrySearchFilters, dates bool, vector []float32, config *types.RetrievalConfig) *gorm.DB {
	expression := fmt.Sprintf("e.embedding::halfvec(%d) <=> ?::halfvec(%d)", len(vector), len(vector))
	return s.eligibleEntries(ctx, spaceID, kbID, f, dates).
		Joins("JOIN chunks c ON c.entry_id=k.id AND c.space_id=k.space_id").
		Joins("JOIN embeddings e ON e.chunk_id=c.id AND e.entry_id=k.id").
		Where(entrySearchableChunks).
		Where("k.processing_mode='full' AND e.dimension=? AND e.embedding IS NOT NULL AND (e.is_enabled IS NULL OR e.is_enabled=true)", len(vector)).
		Where(expression+" <= ?", pgvector.NewHalfVector(vector), 1-config.GetEffectiveVectorThreshold())
}

func (s *searchService) articleVectorQuery(ctx context.Context, spaceID uint64, kbID string, f entrySearchFilters, dates bool, vector []float32, modelID string, config *types.RetrievalConfig) *gorm.DB {
	expression := fmt.Sprintf("a.embedding::halfvec(%d) <=> ?::halfvec(%d)", len(vector), len(vector))
	return s.eligibleEntries(ctx, spaceID, kbID, f, dates).
		Joins("JOIN article_embeddings a ON a.entry_id=k.id AND a.space_id=k.space_id").
		Where("k.source_kind IN ('article','mine') AND a.dimension=? AND a.model_id=?", len(vector), modelID).
		Where(expression+" <= ?", pgvector.NewHalfVector(vector), 1-config.GetEffectiveVectorThreshold())
}

func (s *searchService) imageVectorQuery(ctx context.Context, spaceID uint64, kbID string, f entrySearchFilters, dates bool, vector []float32, modelID string, config *types.RetrievalConfig) *gorm.DB {
	expression := fmt.Sprintf("i.embedding::halfvec(%d) <=> ?::halfvec(%d)", len(vector), len(vector))
	return s.eligibleEntries(ctx, spaceID, kbID, f, dates).
		Joins("JOIN image_embeddings i ON i.entry_id=k.id AND i.space_id=k.space_id").
		Where("k.source_kind IN ('article','mine') AND k.processing_mode='full' AND i.dimension=? AND i.model_id=?", len(vector), modelID).
		Where(expression+" <= ?", pgvector.NewHalfVector(vector), 1-config.GetEffectiveVectorThreshold())
}

func (s *searchService) imageSearchEnabled(ctx context.Context, spaceID uint64, modelID string) (bool, error) {
	var model types.Model
	query := s.db.WithContext(ctx).Where("id=? AND (space_id=? OR is_builtin=true) AND type=?", modelID, spaceID, types.ModelTypeEmbedding).Limit(1).Find(&model)
	if query.Error != nil {
		return false, query.Error
	}
	return query.RowsAffected > 0 && model.Parameters.EmbeddingParameters.SupportsImages, nil
}

func (s *searchService) entryChunkKeywordQuery(ctx context.Context, spaceID uint64, kbID string, f entrySearchFilters, dates bool) *gorm.DB {
	return s.eligibleEntries(ctx, spaceID, kbID, f, dates).
		Joins("JOIN chunks c ON c.entry_id=k.id AND c.space_id=k.space_id").
		Where(entrySearchableChunks).
		Where("k.processing_mode='full'").
		Where(`c.seq_id @@@ paradedb.match_disjunction('content', ?)`, f.request.Query)
}

func (s *searchService) entryHeaderKeywordQuery(ctx context.Context, spaceID uint64, kbID string, f entrySearchFilters, dates bool) *gorm.DB {
	return s.eligibleEntries(ctx, spaceID, kbID, f, dates).
		Where(`k.search_seq_id @@@ paradedb.boolean(should => ARRAY[
			paradedb.match_disjunction('title', ?),
			paradedb.match_disjunction('description', ?),
			paradedb.match_disjunction('search_text', ?)])`, f.request.Query, f.request.Query, f.request.Query)
}

func (s *searchService) retrieveEntryLanes(ctx context.Context, spaceID uint64, kb *types.KnowledgeBase, f entrySearchFilters, vector []float32, modelID string, config *types.RetrievalConfig, pool int, dates bool) ([][]entrySearchHit, error) {
	lanes := make([][]entrySearchHit, 4)
	if len(vector) > 0 {
		expression := fmt.Sprintf("e.embedding::halfvec(%d) <=> ?::halfvec(%d)", len(vector), len(vector))
		query := s.entryChunkVectorQuery(ctx, spaceID, kb.ID, f, dates, vector, config).
			Where("e.model_id=? OR (e.model_id='' AND k.embedding_model_id=?)", modelID, modelID).
			Select("k.id AS entry_id,c.id AS chunk_id,c.chunk_index,c.content,1-("+expression+") AS score,ROW_NUMBER() OVER (PARTITION BY k.id ORDER BY "+expression+",c.id) AS article_rank", pgvector.NewHalfVector(vector), pgvector.NewHalfVector(vector))
		if err := s.db.WithContext(ctx).Table("(?) AS candidates", query).Where("article_rank <= 3").
			Order("score DESC,chunk_id ASC").Limit(poolLimit(pool)).Scan(&lanes[0]).Error; err != nil {
			return nil, err
		}
		expression = fmt.Sprintf("a.embedding::halfvec(%d) <=> ?::halfvec(%d)", len(vector), len(vector))
		query = s.articleVectorQuery(ctx, spaceID, kb.ID, f, dates, vector, modelID, config)
		if err := query.Select("k.id AS entry_id,1-("+expression+") AS score", pgvector.NewHalfVector(vector)).
			Order("score DESC, k.id ASC").Limit(poolLimit(pool)).Scan(&lanes[1]).Error; err != nil {
			return nil, err
		}
		enabled, err := s.imageSearchEnabled(ctx, spaceID, modelID)
		if err != nil {
			return nil, err
		}
		if enabled {
			expression = fmt.Sprintf("i.embedding::halfvec(%d) <=> ?::halfvec(%d)", len(vector), len(vector))
			query = s.imageVectorQuery(ctx, spaceID, kb.ID, f, dates, vector, modelID, config).
				Select("k.id AS entry_id,i.attachment_path AS image_path,1-("+expression+") AS score,ROW_NUMBER() OVER (PARTITION BY k.id ORDER BY "+expression+",i.attachment_path) AS article_rank", pgvector.NewHalfVector(vector), pgvector.NewHalfVector(vector))
			if err := s.db.WithContext(ctx).Table("(?) AS candidates", query).Where("article_rank <= 3").
				Order("score DESC,image_path ASC").Limit(poolLimit(pool)).Scan(&lanes[3]).Error; err != nil {
				return nil, err
			}
		}
	}
	if kb.IsKeywordEnabled() {
		var chunks, headers []entrySearchHit
		query := s.entryChunkKeywordQuery(ctx, spaceID, kb.ID, f, dates).
			Select("k.id AS entry_id,c.id AS chunk_id,c.chunk_index,c.content,paradedb.score(c.seq_id) AS score,ROW_NUMBER() OVER (PARTITION BY k.id ORDER BY paradedb.score(c.seq_id) DESC,c.id) AS article_rank")
		if err := s.db.WithContext(ctx).Table("(?) AS candidates", query).Where("article_rank <= 3").
			Order("score DESC,chunk_id ASC").Limit(poolLimit(pool)).Scan(&chunks).Error; err != nil {
			return nil, err
		}
		if err := s.entryHeaderKeywordQuery(ctx, spaceID, kb.ID, f, dates).
			Select("k.id AS entry_id,paradedb.score(k.search_seq_id) AS score").
			Order("score DESC,k.id ASC").Limit(poolLimit(pool)).Scan(&headers).Error; err != nil {
			return nil, err
		}
		lanes[2] = append(chunks, headers...)
		slices.SortStableFunc(lanes[2], func(a, b entrySearchHit) int {
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
		pointers := make([]*entrySearchHit, len(lanes[2]))
		for i := range lanes[2] {
			pointers[i] = &lanes[2][i]
		}
		searchutil.NormalizeKeywordScores(pointers, func(*entrySearchHit) bool { return true },
			func(hit *entrySearchHit) float64 { return hit.Score },
			func(hit *entrySearchHit, score float64) { hit.Score = score }, searchutil.KeywordScoreCallbacks{})
		lanes[2] = slices.DeleteFunc(lanes[2], func(hit entrySearchHit) bool { return hit.Score < config.GetEffectiveKeywordThreshold() })
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
// Imported documents use their recorded publication date; reports are not counted.
func (s *searchService) countMissingArticleDates(ctx context.Context, spaceID uint64, kb *types.KnowledgeBase, f entrySearchFilters, vector []float32, modelID string, config *types.RetrievalConfig) (int64, error) {
	lanes, err := s.retrieveEntryLanes(ctx, spaceID, kb, f, vector, modelID, config, 0, false)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, hits := range lanes {
		for _, hit := range hits {
			if !seen[hit.EntryID] {
				seen[hit.EntryID] = true
				ids = append(ids, hit.EntryID)
			}
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	var count int64
	err = s.db.WithContext(ctx).Table("entries").Where("space_id=? AND id IN ? AND source_kind IN ('article','mine') AND search_published_at IS NULL", spaceID, ids).Count(&count).Error
	return count, err
}
