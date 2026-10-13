package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

// pgRepository implements PostgreSQL-based retrieval operations
type pgRepository struct {
	db *gorm.DB // Database connection
}

// NewPostgresRetrieveEngineRepository creates a new PostgreSQL retriever repository
func NewPostgresRetrieveEngineRepository(db *gorm.DB) interfaces.RetrieveEngineRepository {
	logger.GetLogger(context.Background()).Info("[Postgres] Initializing PostgreSQL retriever engine repository")
	return &pgRepository{db: db}
}

// EngineType returns the retriever engine type (PostgreSQL)
func (r *pgRepository) EngineType() types.RetrieverEngineType {
	return types.PostgresRetrieverEngineType
}

// Support returns supported retriever types (keywords and vector)
func (r *pgRepository) Support() []types.RetrieverType {
	return []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType}
}

// calculateIndexStorageSize estimates embedding and HNSW storage only.
// Chunk text and BM25 are stored independently and are not repeated per vector source.
func (g *pgRepository) calculateIndexStorageSize(embeddingDB *pgVector) int64 {
	if embeddingDB.Dimension == 0 {
		return 0
	}

	// Vector storage size (2 bytes per dimension for half-precision float).
	var vectorSizeBytes int64 = 0
	if embeddingDB.Dimension > 0 {
		vectorSizeBytes = int64(embeddingDB.Dimension * 2)
	}

	// Fixed overhead for IDs and timestamps.
	metadataSizeBytes := int64(200)

	// HNSW index overhead is approximately twice the vector size.
	indexOverheadBytes := vectorSizeBytes * 2

	// Total size in bytes
	totalSizeBytes := vectorSizeBytes + metadataSizeBytes + indexOverheadBytes

	return totalSizeBytes
}

// EstimateStorageSize estimates total storage size for multiple indices
func (g *pgRepository) EstimateStorageSize(
	ctx context.Context, indexInfoList []*types.IndexInfo, additionalParams map[string]any,
) int64 {
	var totalStorageSize int64 = 0
	for _, indexInfo := range indexInfoList {
		embeddingDB := toDBVectorEmbedding(indexInfo, additionalParams)
		// The indexer keys estimate-only placeholders by chunk ID.
		if embeddingDB.Dimension == 0 && additionalParams != nil {
			if embeddings, ok := additionalParams["embedding"].(map[string][]float32); ok {
				embeddingDB.Dimension = len(embeddings[indexInfo.ChunkID])
			}
		}
		totalStorageSize += g.calculateIndexStorageSize(embeddingDB)
	}
	logger.GetLogger(ctx).Infof(
		"[Postgres] Estimated storage size for %d indices: %d bytes",
		len(indexInfoList), totalStorageSize,
	)
	return totalStorageSize
}

// Save stores a single index entry
func (g *pgRepository) Save(ctx context.Context, indexInfo *types.IndexInfo, additionalParams map[string]any) error {
	logger.GetLogger(ctx).Debugf("[Postgres] Saving index for source ID: %s", indexInfo.SourceID)
	embeddingDB := toDBVectorEmbedding(indexInfo, additionalParams)
	if embeddingDB.Dimension == 0 {
		return nil
	}
	err := g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkVectorModelProfile(ctx, tx, additionalParams); err != nil {
			return err
		}
		return tx.Create(embeddingDB).Error
	})
	if err != nil {
		logger.GetLogger(ctx).Errorf("[Postgres] Failed to save index: %v", err)
		return err
	}
	logger.GetLogger(ctx).Infof("[Postgres] Successfully saved index for source ID: %s", indexInfo.SourceID)
	return nil
}

// BatchSave stores multiple index entries in batch
func (g *pgRepository) BatchSave(
	ctx context.Context, indexInfoList []*types.IndexInfo, additionalParams map[string]any,
) error {
	logger.GetLogger(ctx).Infof("[Postgres] Batch saving %d indices", len(indexInfoList))
	indexInfoDBList := make([]*pgVector, 0, len(indexInfoList))
	for i := range indexInfoList {
		embeddingDB := toDBVectorEmbedding(indexInfoList[i], additionalParams)
		if embeddingDB.Dimension > 0 {
			indexInfoDBList = append(indexInfoDBList, embeddingDB)
		}
	}
	if len(indexInfoDBList) == 0 {
		return nil
	}
	err := g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkVectorModelProfile(ctx, tx, additionalParams); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(indexInfoDBList).Error
	})
	if err != nil {
		logger.GetLogger(ctx).Errorf("[Postgres] Batch save failed: %v", err)
		return err
	}
	logger.GetLogger(ctx).Infof("[Postgres] Successfully batch saved %d indices", len(indexInfoList))
	return nil
}

// DeleteByChunkIDList deletes indices by chunk IDs
func (g *pgRepository) DeleteByChunkIDList(ctx context.Context, chunkIDList []string, dimension int, entryType string) error {
	logger.GetLogger(ctx).Infof("[Postgres] Deleting indices by chunk IDs, count: %d", len(chunkIDList))
	result := g.db.WithContext(ctx).Where("chunk_id IN ?", chunkIDList).Delete(&pgVector{})
	if result.Error != nil {
		logger.GetLogger(ctx).Errorf("[Postgres] Failed to delete indices by chunk IDs: %v", result.Error)
		return result.Error
	}
	logger.GetLogger(ctx).Infof("[Postgres] Successfully deleted %d indices by chunk IDs", result.RowsAffected)
	return nil
}

// DeleteBySourceIDList deletes indices by source IDs
func (g *pgRepository) DeleteBySourceIDList(ctx context.Context, sourceIDList []string, dimension int, entryType string) error {
	if len(sourceIDList) == 0 {
		return nil
	}
	logger.GetLogger(ctx).Infof("[Postgres] Deleting indices by source IDs, count: %d", len(sourceIDList))
	result := g.db.WithContext(ctx).Where("source_id IN ?", sourceIDList).Delete(&pgVector{})
	if result.Error != nil {
		logger.GetLogger(ctx).Errorf("[Postgres] Failed to delete indices by source IDs: %v", result.Error)
		return result.Error
	}
	logger.GetLogger(ctx).Infof("[Postgres] Successfully deleted %d indices by source IDs", result.RowsAffected)
	return nil
}

// DeleteByEntryIDList deletes indices by entry IDs
func (g *pgRepository) DeleteByEntryIDList(ctx context.Context, entryIDList []string, dimension int, entryType string) error {
	logger.GetLogger(ctx).Infof("[Postgres] Deleting indices by entry IDs, count: %d", len(entryIDList))
	result := g.db.WithContext(ctx).Where("entry_id IN ?", entryIDList).Delete(&pgVector{})
	if result.Error != nil {
		logger.GetLogger(ctx).Errorf("[Postgres] Failed to delete indices by entry IDs: %v", result.Error)
		return result.Error
	}
	logger.GetLogger(ctx).Infof("[Postgres] Successfully deleted %d indices by entry IDs", result.RowsAffected)
	return nil
}

// Retrieve handles retrieval requests and routes to appropriate method
func (g *pgRepository) Retrieve(ctx context.Context, params types.RetrieveParams) ([]*types.RetrieveResult, error) {
	logger.GetLogger(ctx).Debugf("[Postgres] Processing retrieval request of type: %s", params.RetrieverType)
	switch params.RetrieverType {
	case types.KeywordsRetrieverType:
		return g.KeywordsRetrieve(ctx, params)
	case types.VectorRetrieverType:
		return g.VectorRetrieve(ctx, params)
	}
	err := errors.New("invalid retriever type")
	logger.GetLogger(ctx).Errorf("[Postgres] %v: %s", err, params.RetrieverType)
	return nil, err
}

// KeywordsRetrieve performs keyword-based search using PostgreSQL full-text search
func (g *pgRepository) KeywordsRetrieve(ctx context.Context,
	params types.RetrieveParams,
) ([]*types.RetrieveResult, error) {
	logger.GetLogger(ctx).Infof("[Postgres] Keywords retrieval: query=%s, topK=%d", params.Query, params.TopK)
	conds := make([]clause.Expression, 0)

	// LibraryIDs and EntryIDs use AND logic
	// - If only LibraryIDs: search entire libraries
	// - If only EntryIDs: search specific documents
	// - If both: search specific documents within the libraries (AND)
	if len(params.LibraryIDs) > 0 {
		logger.GetLogger(ctx).Debugf("[Postgres] Filtering by library IDs: %v", params.LibraryIDs)
		conds = append(conds, clause.IN{
			Column: "c.library_id",
			Values: utils.ToInterfaceSlice(params.LibraryIDs),
		})
	}
	if len(params.EntryIDs) > 0 {
		logger.GetLogger(ctx).Debugf("[Postgres] Filtering by entry IDs: %v", params.EntryIDs)
		conds = append(conds, clause.IN{
			Column: "c.entry_id",
			Values: utils.ToInterfaceSlice(params.EntryIDs),
		})
	}

	// Match searchable body text and article titles.
	conds = append(conds, clause.Expr{
		SQL: `c.seq_id @@@ paradedb.boolean(should => ARRAY[
			paradedb.match_disjunction('content', ?),
			paradedb.match_disjunction('context_header', ?),
			paradedb.match_disjunction('search_title', ?)
		])`,
		Vars: []interface{}{params.Query, params.Query, params.Query},
	})

	// A chunk is the source of keyword availability, even without a vector row.
	// Keep the parent-context exclusion explicit: ParadeDB includes heap-filter
	// clauses in its scoring plan, and this preserves existing keyword scores.
	conds = append(conds, clause.Expr{
		SQL: `c.deleted_at IS NULL
			AND (c.is_enabled IS NULL OR c.is_enabled = true)
			AND c.chunk_type <> 'parent_text'
			AND c.chunk_type IN ('text', 'summary', 'table_summary', 'table_column')
			AND k.deleted_at IS NULL AND library.deleted_at IS NULL
			AND (library.indexing_strategy IS NULL OR library.indexing_strategy->>'keyword_enabled' = 'true')`,
	})
	conds = append(conds, clause.OrderBy{Columns: []clause.OrderByColumn{
		{Column: clause.Column{Name: "score"}, Desc: true},
	}})

	var embeddingDBList []pgVectorWithScore
	err := g.db.WithContext(ctx).Table("chunks AS c").
		Joins("JOIN entries AS k ON k.id = c.entry_id").
		Joins("JOIN libraries AS library ON library.id = c.library_id").
		Clauses(conds...).
		Select([]string{
			"paradedb.score(c.seq_id) AS score",
			"c.seq_id AS id",
			"c.content", "c.context_header", "k.title", "c.metadata",
			"c.id AS source_id",
			"0 AS source_type",
			"c.id AS chunk_id",
			"c.entry_id",
			"c.library_id",
		}).
		Limit(int(params.TopK)).
		Find(&embeddingDBList).Error

	if err == gorm.ErrRecordNotFound {
		logger.GetLogger(ctx).Warnf("[Postgres] No records found for keywords query: %s", params.Query)
		return nil, nil
	}
	if err != nil {
		logger.GetLogger(ctx).Errorf("[Postgres] Keywords retrieval failed: %v", err)
		return nil, err
	}

	summaries, err := g.retrieveArticleSummaries(ctx, params)
	if err != nil {
		return nil, err
	}
	embeddingDBList = append(embeddingDBList, summaries...)
	slices.SortStableFunc(embeddingDBList, func(a, b pgVectorWithScore) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		return 0
	})
	if params.TopK > 0 && len(embeddingDBList) > params.TopK {
		embeddingDBList = embeddingDBList[:params.TopK]
	}

	logger.GetLogger(ctx).Infof("[Postgres] Keywords retrieval found %d results", len(embeddingDBList))
	results := make([]*types.IndexWithScore, 0, len(embeddingDBList))
	const maxKeywordResultLog = 8
	for i := range embeddingDBList {
		result := fromDBVectorEmbeddingWithScore(&embeddingDBList[i], types.MatchTypeKeywords)
		results = append(results, result)
		if i < maxKeywordResultLog {
			logger.GetLogger(ctx).Debugf("[Postgres] Keywords result %d: chunk=%s, score=%f",
				i, result.ChunkID, result.Score)
		}
	}
	if len(results) > maxKeywordResultLog {
		logger.GetLogger(ctx).Debugf(
			"[Postgres] Keywords result summary: total=%d logged=%d truncated=%d",
			len(results), maxKeywordResultLog, len(results)-maxKeywordResultLog,
		)
	}
	return []*types.RetrieveResult{
		{
			Results:             results,
			RetrieverEngineType: types.PostgresRetrieverEngineType,
			RetrieverType:       types.KeywordsRetrieverType,
			Error:               nil,
		},
	}, nil
}

// VectorRetrieve performs vector similarity search using pgvector
// Optimized to use HNSW index efficiently and avoid recalculating vector distance
func (g *pgRepository) VectorRetrieve(ctx context.Context,
	params types.RetrieveParams,
) ([]*types.RetrieveResult, error) {
	logger.GetLogger(ctx).Infof("[Postgres] Vector retrieval: dim=%d, topK=%d, threshold=%.4f",
		len(params.Embedding), params.TopK, params.Threshold)

	dimension := len(params.Embedding)
	queryVector := pgvector.NewHalfVector(params.Embedding)

	// Build WHERE conditions for filtering
	whereParts := make([]string, 0)
	allVars := make([]interface{}, 0)

	// Add query vector first (used in ORDER BY for HNSW index)
	allVars = append(allVars, queryVector)

	// Dimension filter (required for HNSW index WHERE clause)
	whereParts = append(whereParts, fmt.Sprintf("dimension = $%d", len(allVars)+1))
	allVars = append(allVars, dimension)

	// LibraryIDs and EntryIDs use AND logic
	// - If only LibraryIDs: search entire libraries
	// - If only EntryIDs: search specific documents
	// - If both: search specific documents within the libraries (AND)
	if len(params.LibraryIDs) > 0 {
		logger.GetLogger(ctx).Debugf(
			"[Postgres] Filtering vector search by library IDs: %v",
			params.LibraryIDs,
		)
		placeholders := make([]string, len(params.LibraryIDs))
		paramStart := len(allVars) + 1
		for i := range params.LibraryIDs {
			placeholders[i] = fmt.Sprintf("$%d", paramStart+i)
			allVars = append(allVars, params.LibraryIDs[i])
		}
		whereParts = append(whereParts, fmt.Sprintf("library_id IN (%s)",
			strings.Join(placeholders, ", ")))
	}
	if len(params.EntryIDs) > 0 {
		logger.GetLogger(ctx).Debugf(
			"[Postgres] Filtering vector search by entry IDs: %v",
			params.EntryIDs,
		)
		placeholders := make([]string, len(params.EntryIDs))
		paramStart := len(allVars) + 1
		for i := range params.EntryIDs {
			placeholders[i] = fmt.Sprintf("$%d", paramStart+i)
			allVars = append(allVars, params.EntryIDs[i])
		}
		whereParts = append(whereParts, fmt.Sprintf("entry_id IN (%s)",
			strings.Join(placeholders, ", ")))
	}

	// is_enabled filter
	whereParts = append(whereParts, fmt.Sprintf("(is_enabled IS NULL OR is_enabled = $%d)", len(allVars)+1))
	allVars = append(allVars, true)

	// Build WHERE clause string
	whereClause := ""
	if len(whereParts) > 0 {
		whereClause = "WHERE " + strings.Join(whereParts, " AND ")
	}

	// Expand TopK to get more candidates before threshold filtering.
	//
	// HNSW requires `ef_search >= LIMIT`, and a very large LIMIT (e.g. 1000)
	// forces HNSW to walk a near-exhaustive portion of the graph, often making
	// it slower than a sequential scan and pushing the planner to pick Seq Scan
	// even when an index exists. 200 is a good sweet spot: it gives enough
	// headroom for threshold/filter post-processing without ballooning ef_search.
	expandedTopK := params.TopK * 2
	if expandedTopK < 100 {
		expandedTopK = 100 // Minimum 100 candidates
	}
	if expandedTopK > 200 {
		expandedTopK = 200 // Maximum 200 candidates (keeps HNSW efficient)
	}
	if expandedTopK < params.TopK {
		expandedTopK = params.TopK // Ensure subquery limit is at least final limit
	}

	// Optimized query: Use subquery to calculate distance once.
	//
	// IMPORTANT: The HNSW index in this project is built on the EXPRESSION
	//   (embedding::halfvec(<dim>)) halfvec_cosine_ops
	// because the `embedding` column itself is `halfvec` without a fixed dimension
	// (so the table can store multiple embedding sizes such as 798 / 3584 / ...).
	//
	// pgvector requires the ORDER BY expression to match the indexed expression
	// EXACTLY, otherwise the planner falls back to a sequential scan. The
	// `embedding::halfvec(%d)` cast on both sides of `<=>` is therefore NOT
	// redundant — it is the only way to make the HNSW index get used at all.
	// See: pgvector issues #702, #835 and ParadeDB "indexing-expressions" docs.
	subqueryLimitParam := len(allVars) + 1
	thresholdParam := len(allVars) + 2
	finalLimitParam := len(allVars) + 3

	querySQL := fmt.Sprintf(`
		SELECT 
			e.id, c.content, c.context_header, k.title, c.metadata,
			e.source_id, e.source_type, e.chunk_id, e.entry_id, e.library_id,
			(1 - candidates.distance) as score
		FROM (
			SELECT 
				id,
				embedding::halfvec(%[1]d) <=> $1::halfvec(%[1]d) as distance
			FROM embeddings
			%[2]s
			ORDER BY embedding::halfvec(%[1]d) <=> $1::halfvec(%[1]d)
			LIMIT $%[3]d
		) AS candidates
		JOIN embeddings AS e ON e.id = candidates.id
		JOIN chunks AS c ON c.id = e.chunk_id
		JOIN entries AS k ON k.id = c.entry_id
		WHERE candidates.distance <= $%[4]d
			AND c.deleted_at IS NULL
			AND (c.is_enabled IS NULL OR c.is_enabled = true)
			AND c.chunk_type IN ('text', 'summary', 'table_summary', 'table_column')
			AND k.deleted_at IS NULL
		ORDER BY candidates.distance ASC
		LIMIT $%[5]d
	`, dimension, whereClause, subqueryLimitParam, thresholdParam, finalLimitParam)

	allVars = append(allVars, expandedTopK)       // LIMIT in subquery
	allVars = append(allVars, 1-params.Threshold) // Distance threshold
	allVars = append(allVars, params.TopK)        // Final LIMIT

	// HNSW's `ef_search` defaults to 40, which is much smaller than our
	// `expandedTopK` budget (up to 1000). Without raising it, HNSW would only
	// return ~40 candidates per scan and our outer threshold/filter step would
	// silently lose recall. `SET LOCAL` requires a transaction so we wrap the
	// query in one. We never write inside this transaction, so the cost is
	// negligible.
	efSearch := expandedTopK
	if efSearch < 40 {
		efSearch = 40
	}

	var embeddingDBList []pgVectorWithScore

	err := g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", efSearch)).Error; err != nil {
			// Treat as non-fatal: pgvector should always expose this GUC, but if
			// for any reason it does not we still want the query to run (just
			// with default recall). We must rollback first because a failed
			// statement aborts the transaction in PostgreSQL.
			logger.GetLogger(ctx).Warnf("[Postgres] Failed to set hnsw.ef_search=%d: %v", efSearch, err)
			return err
		}
		// pgvector >= 0.8 supports iterative scan, which keeps pulling more
		// candidates from HNSW until the post-filter (library_id /
		// entry_id / is_enabled) yields enough rows. Without it,
		// HNSW returns at most ef_search candidates and the outer filter may
		// silently lose recall when the filter is selective.
		// Best-effort: ignore failure on older pgvector versions.
		if err := tx.Exec("SET LOCAL hnsw.iterative_scan = strict_order").Error; err != nil {
			logger.GetLogger(ctx).Debugf("[Postgres] hnsw.iterative_scan not available: %v", err)
			// abort transaction and let the fallback path below handle it.
			return err
		}
		return tx.Raw(querySQL, allVars...).Scan(&embeddingDBList).Error
	})

	// Fallback: if the transaction failed because of an unsupported GUC (e.g.
	// older pgvector that doesn't have hnsw.ef_search or hnsw.iterative_scan),
	// retry the query without the SETs so we still return results.
	if err != nil && len(embeddingDBList) == 0 &&
		(strings.Contains(err.Error(), "hnsw.ef_search") ||
			strings.Contains(err.Error(), "hnsw.iterative_scan")) {
		logger.GetLogger(ctx).Warnf("[Postgres] Retrying vector query without HNSW GUC overrides: %v", err)
		err = g.db.WithContext(ctx).Raw(querySQL, allVars...).Scan(&embeddingDBList).Error
	}

	if err == gorm.ErrRecordNotFound {
		logger.GetLogger(ctx).Warnf("[Postgres] No vector matches found that meet threshold %.4f", params.Threshold)
		return nil, nil
	}
	if err != nil {
		logger.GetLogger(ctx).Errorf("[Postgres] Vector retrieval failed: %v", err)
		return nil, err
	}

	// Apply final TopK limit (in case we got more results than needed)
	if len(embeddingDBList) > int(params.TopK) {
		embeddingDBList = embeddingDBList[:params.TopK]
	}

	logger.GetLogger(ctx).Infof("[Postgres] Vector retrieval found %d results", len(embeddingDBList))
	results := make([]*types.IndexWithScore, 0, len(embeddingDBList))
	const maxVectorResultLog = 8
	for i := range embeddingDBList {
		result := fromDBVectorEmbeddingWithScore(&embeddingDBList[i], types.MatchTypeEmbedding)
		if result == nil {
			continue
		}
		results = append(results, result)
		if i < maxVectorResultLog {
			logger.GetLogger(ctx).Debugf("[Postgres] Vector search result %d: chunk_id %s, score %.4f",
				i, result.ChunkID, result.Score)
		}
	}
	if len(results) > maxVectorResultLog {
		logger.GetLogger(ctx).Debugf(
			"[Postgres] Vector search result summary: total=%d logged=%d truncated=%d",
			len(results), maxVectorResultLog, len(results)-maxVectorResultLog,
		)
	}
	return []*types.RetrieveResult{
		{
			Results:             results,
			RetrieverEngineType: types.PostgresRetrieverEngineType,
			RetrieverType:       types.VectorRetrieverType,
			Error:               nil,
		},
	}, nil
}

// BatchUpdateChunkEnabledStatus updates the enabled status of chunks in batch
func (g *pgRepository) BatchUpdateChunkEnabledStatus(ctx context.Context, chunkStatusMap map[string]bool) error {
	if len(chunkStatusMap) == 0 {
		logger.GetLogger(ctx).Warnf("[Postgres] Chunk status map is empty, skipping update")
		return nil
	}

	logger.GetLogger(ctx).Infof("[Postgres] Batch updating chunk enabled status, count: %d", len(chunkStatusMap))

	// Group chunks by enabled status for batch updates
	enabledChunkIDs := make([]string, 0)
	disabledChunkIDs := make([]string, 0)

	for chunkID, enabled := range chunkStatusMap {
		if enabled {
			enabledChunkIDs = append(enabledChunkIDs, chunkID)
		} else {
			disabledChunkIDs = append(disabledChunkIDs, chunkID)
		}
	}

	// Batch update enabled chunks
	if len(enabledChunkIDs) > 0 {
		result := g.db.WithContext(ctx).Model(&pgVector{}).
			Where("chunk_id IN ?", enabledChunkIDs).
			Update("is_enabled", true)
		if result.Error != nil {
			logger.GetLogger(ctx).Errorf("[Postgres] Failed to update enabled chunks: %v", result.Error)
			return result.Error
		}
		logger.GetLogger(ctx).
			Infof("[Postgres] Updated %d chunks to enabled, rows affected: %d", len(enabledChunkIDs), result.RowsAffected)
	}

	// Batch update disabled chunks
	if len(disabledChunkIDs) > 0 {
		result := g.db.WithContext(ctx).Model(&pgVector{}).
			Where("chunk_id IN ?", disabledChunkIDs).
			Update("is_enabled", false)
		if result.Error != nil {
			logger.GetLogger(ctx).Errorf("[Postgres] Failed to update disabled chunks: %v", result.Error)
			return result.Error
		}
		logger.GetLogger(ctx).
			Infof("[Postgres] Updated %d chunks to disabled, rows affected: %d", len(disabledChunkIDs), result.RowsAffected)
	}

	logger.GetLogger(ctx).Infof("[Postgres] Successfully batch updated chunk enabled status")
	return nil
}
