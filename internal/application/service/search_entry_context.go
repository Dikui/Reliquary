package service

import (
	"context"

	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// Enrich only the final hits. Follow at most two edges (image -> text -> parent)
// and return each parent once per entry, without changing the matching evidence.
func (s *searchService) expandEntryParentContext(ctx context.Context, spaceID uint64, libraries []types.Library, results []*types.EntrySearchResult) {
	enabled := map[string]bool{}
	for _, library := range libraries {
		enabled[library.ID] = library.ChunkingConfig.EnableParentChild
	}
	var hits []*types.EntrySearchResult
	var ids []string
	var collect func([]*types.EntrySearchResult)
	collect = func(rows []*types.EntrySearchResult) {
		for _, row := range rows {
			if enabled[row.LibraryID] {
				hits = append(hits, row)
				for _, snippet := range row.Snippets {
					if snippet.ChunkID != "" {
						ids = append(ids, snippet.ChunkID)
					}
				}
			}
			collect(row.SimilarReports)
		}
	}
	collect(results)
	if len(ids) == 0 {
		return
	}
	type parentLink struct{ HitID, ParentID string }
	var links []parentLink
	err := s.db.WithContext(ctx).Raw(`SELECT c.id AS hit_id,
		CASE WHEN p.chunk_type='parent_text' THEN p.id ELSE gp.id END AS parent_id
		FROM chunks c
		JOIN chunks p ON p.id=c.parent_chunk_id AND p.space_id=c.space_id
			AND p.library_id=c.library_id AND p.entry_id=c.entry_id AND p.deleted_at IS NULL AND (p.is_enabled IS NULL OR p.is_enabled=true)
		LEFT JOIN chunks gp ON gp.id=p.parent_chunk_id AND gp.space_id=c.space_id
			AND gp.library_id=c.library_id AND gp.entry_id=c.entry_id AND gp.deleted_at IS NULL
			AND (gp.is_enabled IS NULL OR gp.is_enabled=true) AND gp.chunk_type='parent_text'
		WHERE c.space_id=? AND c.id IN ? AND c.deleted_at IS NULL AND (c.is_enabled IS NULL OR c.is_enabled=true)
		AND (p.chunk_type='parent_text' OR gp.id IS NOT NULL)`, spaceID, ids).Scan(&links).Error
	if err != nil {
		logger.Warnf(ctx, "Search parent context unavailable: %v", err)
		return
	}
	parentsByHit := map[string]string{}
	parentIDs := []string{}
	seen := map[string]bool{}
	for _, link := range links {
		parentsByHit[link.HitID] = link.ParentID
		if !seen[link.ParentID] {
			parentIDs = append(parentIDs, link.ParentID)
			seen[link.ParentID] = true
		}
	}
	if len(parentIDs) == 0 {
		return
	}
	var parents []types.Chunk
	if err := s.db.WithContext(ctx).Where("space_id=? AND id IN ? AND (is_enabled IS NULL OR is_enabled=true)", spaceID, parentIDs).Find(&parents).Error; err != nil {
		logger.Warnf(ctx, "Search parent context unavailable: %v", err)
		return
	}
	byID := map[string]types.Chunk{}
	for _, parent := range parents {
		byID[parent.ID] = parent
	}
	for _, hit := range hits {
		used := map[string]bool{}
		for i := range hit.Snippets {
			snippet := &hit.Snippets[i]
			parentID := parentsByHit[snippet.ChunkID]
			parent, ok := byID[parentID]
			if !ok || used[parentID] || parent.Content == "" || parent.EntryID != hit.EntryID || parent.LibraryID != hit.LibraryID {
				continue
			}
			snippet.Context, snippet.ContextLocations = parent.Content, parent.SourceLocations
			used[parentID] = true
		}
	}
}
