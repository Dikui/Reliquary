package postgres

import (
	"context"

	"reliquary/internal/types"
)

// retrieveArticleSummaries uses an independent keyword index so a summary does
// not require a body chunk or an embedding row.
func (g *pgRepository) retrieveArticleSummaries(ctx context.Context, params types.RetrieveParams) ([]pgVectorWithScore, error) {
	query := g.db.WithContext(ctx).Table("entries AS k").
		Joins("JOIN libraries AS library ON library.id=k.library_id").
		Where(`k.search_seq_id @@@ paradedb.boolean(should => ARRAY[
			paradedb.match_disjunction('title', ?),
			paradedb.match_disjunction('description', ?)])`, params.Query, params.Query).
		Where(`k.deleted_at IS NULL AND library.deleted_at IS NULL
			AND k.parse_status = 'completed' AND k.enable_status = 'enabled'
			AND k.structured_summary IS NOT NULL AND COALESCE(k.description, '') <> ''
			AND (library.indexing_strategy IS NULL OR library.indexing_strategy->>'keyword_enabled' = 'true')`)
	if len(params.LibraryIDs) > 0 {
		query = query.Where("k.library_id IN ?", params.LibraryIDs)
	}
	if len(params.EntryIDs) > 0 {
		query = query.Where("k.id IN ?", params.EntryIDs)
	}
	if len(params.ExcludeEntryIDs) > 0 {
		query = query.Where("k.id NOT IN ?", params.ExcludeEntryIDs)
	}
	var rows []pgVectorWithScore
	err := query.Select(`paradedb.score(k.search_seq_id) AS score,
		k.search_seq_id AS id, k.description AS content, k.title,
		('knowledge-summary:' || k.id) AS source_id, 0 AS source_type,
		('knowledge-summary:' || k.id) AS chunk_id, k.id AS entry_id, k.library_id`).
		Order("score DESC").Limit(params.TopK).Find(&rows).Error
	return rows, err
}
