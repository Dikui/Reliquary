package service

import (
	"context"
	"slices"
	"time"

	"reliquary/internal/graphreplica"
	"reliquary/internal/types"
)

// Hydration also validates the authority before a replica edge can extend a
// frontier. A deleted source cannot remain a bridge to other documents.
func (s *GraphSearchService) hydrate(ctx context.Context, space uint64, libraries []types.Library, edges []graphreplica.Edge) ([]types.GraphSearchEdge, error) {
	out := []types.GraphSearchEdge{}
	if len(edges) == 0 {
		return out, nil
	}
	ids, libIDs := []string{}, []string{}
	libMap := map[string]types.Library{}
	for _, l := range libraries {
		libIDs = append(libIDs, l.ID)
		libMap[l.ID] = l
	}
	for _, e := range edges {
		ids = append(ids, e.ID)
	}
	var relations []types.Relation
	if err := s.db.WithContext(ctx).Where("space_id=? AND library_id IN ? AND id IN ?", space, libIDs, ids).Find(&relations).Error; err != nil {
		return nil, err
	}
	rows := map[string]types.Relation{}
	nodeIDs := []string{}
	for _, r := range relations {
		rows[r.ID] = r
		nodeIDs = append(nodeIDs, r.SubjectID, r.ObjectID)
	}
	var liveIDs []string
	if err := s.db.WithContext(ctx).Model(&types.Entity{}).Where("space_id=? AND id IN ? AND coalesce(merged_into_id,'')='' AND archived_at IS NULL", space, nodeIDs).Pluck("id", &liveIDs).Error; err != nil {
		return nil, err
	}
	var sources []types.RelationSource
	if err := s.db.WithContext(ctx).Model(&types.RelationSource{}).Joins("JOIN entries e ON e.id=relation_sources.entry_id").Where("relation_sources.relation_id IN ? AND e.space_id=? AND e.library_id IN ? AND e.deleted_at IS NULL", ids, space, libIDs).Select("relation_sources.*").Order("relation_sources.relation_id,relation_sources.entry_id").Find(&sources).Error; err != nil {
		return nil, err
	}
	entryIDs := []string{}
	for _, src := range sources {
		entryIDs = append(entryIDs, src.EntryID)
	}
	type entryRow struct {
		types.Entry       `gorm:"embedded"`
		SearchPublishedAt *time.Time
	}
	var entries []entryRow
	if err := s.db.WithContext(ctx).Table("entries").Where("space_id=? AND library_id IN ? AND id IN ? AND deleted_at IS NULL", space, libIDs, entryIDs).Find(&entries).Error; err != nil {
		return nil, err
	}
	entryMap := map[string]entryRow{}
	for _, e := range entries {
		entryMap[e.ID] = e
	}
	sourceMap := map[string][]types.GraphSource{}
	for _, src := range sources {
		e, ok := entryMap[src.EntryID]
		r := rows[src.RelationID]
		if !ok || e.LibraryID != r.LibraryID {
			continue
		}
		l := libMap[e.LibraryID]
		sourceMap[src.RelationID] = append(sourceMap[src.RelationID], types.GraphSource{EntryID: e.ID, Title: e.Title, LibraryID: e.LibraryID, LibraryName: l.Name, LibraryDirectory: l.Directory, PublishedAt: e.SearchPublishedAt, SourceUnverified: e.SourceUnverified, Acquisition: e.Acquisition, Evidence: src.Evidence})
	}
	for _, e := range edges {
		r, ok := rows[e.ID]
		if !ok || r.LibraryID != e.LibraryID || r.SubjectID != e.SubjectID || r.ObjectID != e.ObjectID || r.RelationType != e.RelationType || !slices.Contains(liveIDs, r.SubjectID) || !slices.Contains(liveIDs, r.ObjectID) || (len(sourceMap[e.ID]) == 0 && libMap[r.LibraryID].EntityMode != types.EntityModeManaged) {
			continue
		}
		out = append(out, types.GraphSearchEdge{ID: r.ID, SubjectID: r.SubjectID, ObjectID: r.ObjectID, LibraryID: r.LibraryID, RelationType: r.RelationType, Version: r.Version, Sources: sourceMap[r.ID], SourceCount: len(sourceMap[r.ID])})
	}
	return out, nil
}
func boundedGraphPaths(edges []types.GraphSearchEdge, start, target string, hops, limit int) ([]types.GraphSearchPath, string) {
	paths := []types.GraphSearchPath{}
	adjacency := map[string][]types.GraphSearchEdge{}
	for _, e := range edges {
		adjacency[e.SubjectID] = append(adjacency[e.SubjectID], e)
		adjacency[e.ObjectID] = append(adjacency[e.ObjectID], e)
	}
	// Reverse distances prune branches that cannot reach the target in time.
	distances := map[string]int{target: 0}
	frontier := []string{target}
	for depth := 1; depth <= hops; depth++ {
		next := []string{}
		for _, id := range frontier {
			for _, e := range adjacency[id] {
				other := e.SubjectID
				if other == id {
					other = e.ObjectID
				}
				if _, ok := distances[other]; !ok {
					distances[other] = depth
					next = append(next, other)
				}
			}
		}
		frontier = next
	}
	budget := 10000
	reason := ""
	var visit func([]string, []string)
	visit = func(nodes, relations []string) {
		if reason != "" {
			return
		}
		budget--
		if budget < 0 {
			reason = "path_work_limit"
			return
		}
		id := nodes[len(nodes)-1]
		if id == target {
			if len(paths) == limit {
				reason = "path_limit"
				return
			}
			paths = append(paths, types.GraphSearchPath{EntityIDs: append([]string{}, nodes...), RelationIDs: append([]string{}, relations...)})
			return
		}
		if len(relations) == hops {
			return
		}
		for _, e := range adjacency[id] {
			other := e.SubjectID
			if other == id {
				other = e.ObjectID
			}
			distance, ok := distances[other]
			if !ok || distance > hops-len(relations)-1 || slices.Contains(nodes, other) {
				continue
			}
			visit(append(append([]string{}, nodes...), other), append(append([]string{}, relations...), e.ID))
			if reason != "" {
				return
			}
		}
	}
	visit([]string{start}, []string{})
	return paths, reason
}
func graphDocuments(edges []types.GraphSearchEdge) []*types.EntrySearchResult {
	out := []*types.EntrySearchResult{}
	seen := map[string]*types.EntrySearchResult{}
	for _, edge := range edges {
		for _, source := range edge.Sources {
			d := seen[source.EntryID]
			if d == nil {
				d = &types.EntrySearchResult{EntryID: source.EntryID, Title: source.Title, LibraryID: source.LibraryID, LibraryName: source.LibraryName, LibraryDirectory: source.LibraryDirectory, PublishedAt: source.PublishedAt, SourceUnverified: source.SourceUnverified, Acquisition: source.Acquisition, Score: 1, Entities: []types.EntrySearchEntity{}, Topics: []types.EntrySearchTopic{}, Snippets: []types.EntrySearchSnippet{}, SimilarReports: []*types.EntrySearchResult{}}
				seen[source.EntryID] = d
				out = append(out, d)
			}
			for _, evidence := range source.Evidence {
				d.Snippets = append(d.Snippets, types.EntrySearchSnippet{Locations: evidence.Locations, MatchType: "graph", Score: 1})
			}
		}
	}
	return out
}
