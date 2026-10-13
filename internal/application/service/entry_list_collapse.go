package service

import (
	"context"
	"reliquary/internal/types"
)

// Fold the complete filtered set before pagination, so a group never repeats on later pages.
func (s *entryService) collapseListedEntries(ctx context.Context, entries []*types.Entry) ([]*types.Entry, error) {
	if len(entries) < 2 {
		return entries, nil
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	var count int64
	if err := s.db.WithContext(ctx).Table("article_embeddings ae").
		Joins("JOIN entries e ON e.space_id=ae.space_id AND e.library_id=ae.library_id AND e.id=ae.entry_id").
		Where("ae.space_id=? AND ae.entry_id IN ?", types.MustSpaceIDFromContext(ctx), ids).Count(&count).Error; err != nil {
		return nil, err
	}
	if count < 2 {
		return entries, nil
	}
	byID := map[string]*types.Entry{}
	results := make([]*types.EntrySearchResult, 0, len(entries))
	for _, entry := range entries {
		source, err := entrySourceInfo(entry)
		if err != nil {
			return nil, err
		}
		byID[entry.ID] = entry
		results = append(results, &types.EntrySearchResult{EntryID: entry.ID, Source: entry.SourceKind, PublishedAt: parsePublicationTime(source.PublishedAt)})
	}
	var space types.Space
	if err := s.db.WithContext(ctx).Select("retrieval_config").Where("id=?", types.MustSpaceIDFromContext(ctx)).Take(&space).Error; err != nil {
		return nil, err
	}
	search := &searchService{db: s.db}
	groups, err := search.collapseArticleReports(ctx, types.MustSpaceIDFromContext(ctx), results, space.RetrievalConfig)
	if err != nil {
		return nil, err
	}
	folded := make([]*types.Entry, 0, len(groups))
	for _, group := range groups {
		entry := byID[group.EntryID]
		entry.SimilarReports = nil
		for _, member := range group.SimilarReports {
			entry.SimilarReports = append(entry.SimilarReports, byID[member.EntryID])
		}
		folded = append(folded, entry)
	}
	return folded, nil
}
