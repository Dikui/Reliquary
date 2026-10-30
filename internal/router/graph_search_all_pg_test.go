package router

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
)

// All mode reads PG only, so it runs without the Neo4j replica.
func TestGraphSearchAllModeReadsWholeLibrary(t *testing.T) {
	f := newMCPFixture(t)
	graph := service.NewGraphSearchService(f.db, nil)
	var lib types.Library
	require.NoError(t, f.db.Where("space_id=1 AND directory='media'").First(&lib).Error)
	for _, name := range []string{"cooperates", "supports"} {
		require.NoError(t, f.db.Create(&types.LibraryRelationType{LibraryID: lib.ID, Name: name, Enabled: true}).Error)
	}
	ids := []string{}
	for _, name := range []string{"All A", "All B", "All C", "All D", "All Merged"} {
		id := uuid.NewString()
		ids = append(ids, id)
		require.NoError(t, f.db.Create(&types.Entity{ID: id, SpaceID: 1, LibraryID: lib.ID, CanonicalName: name, NormalizedName: name, EntityType: "company"}).Error)
	}
	require.NoError(t, f.db.Model(&types.Entity{}).Where("id=?", ids[4]).Update("merged_into_id", ids[0]).Error)
	relate := func(a, b int, kind string, sources int) string {
		id := uuid.NewString()
		require.NoError(t, f.db.Create(&types.Relation{ID: id, SpaceID: 1, LibraryID: lib.ID, SubjectID: ids[a], ObjectID: ids[b], RelationType: kind, Version: 1}).Error)
		for range sources {
			entry := &types.Entry{ID: uuid.NewString(), SpaceID: 1, LibraryID: lib.ID, Title: "All source", Type: "manual", ParseStatus: "completed", EnableStatus: "enabled"}
			require.NoError(t, f.db.Create(entry).Error)
			require.NoError(t, f.db.Create(&types.RelationSource{RelationID: id, EntryID: entry.ID, RunID: "test", BodyHash: "hash", Evidence: []types.RelationEvidence{}}).Error)
		}
		return id
	}
	ab, cd := relate(0, 1, "cooperates", 1), relate(2, 3, "supports", 2)
	relate(1, 2, "cooperates", 0) // no source: hidden
	relate(0, 4, "cooperates", 1) // merged endpoint: hidden

	out, err := graph.Search(f.ctx, types.GraphSearchRequest{Libraries: []string{lib.ID}, Mode: "all", Limit: 10})
	require.NoError(t, err)
	edgeIDs := []string{}
	for _, e := range out.Edges {
		edgeIDs = append(edgeIDs, e.ID)
	}
	require.Equal(t, []string{cd, ab}, edgeIDs, "disconnected components together, best-evidenced first")
	require.Len(t, out.Entities, 4)
	require.False(t, out.Truncated)

	out, err = graph.Search(f.ctx, types.GraphSearchRequest{Libraries: []string{lib.ID}, Mode: "all", Limit: 1})
	require.NoError(t, err)
	require.Len(t, out.Edges, 1)
	require.Equal(t, cd, out.Edges[0].ID)
	require.True(t, out.Truncated)

	out, err = graph.Search(f.ctx, types.GraphSearchRequest{Libraries: []string{lib.ID}, Mode: "all", RelationTypes: []string{"cooperates"}, Limit: 10})
	require.NoError(t, err)
	require.Len(t, out.Edges, 1)
	require.Equal(t, ab, out.Edges[0].ID)

	_, err = graph.Search(f.ctx, types.GraphSearchRequest{Libraries: []string{lib.ID}, Mode: "all", Entity: types.GraphEntityInput{ID: ids[0]}})
	require.Error(t, err)
	_, err = graph.Search(f.ctx, types.GraphSearchRequest{Libraries: []string{lib.ID}, Mode: "all", Limit: 501})
	require.Error(t, err)
}
