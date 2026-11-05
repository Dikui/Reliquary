package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/types"
)

// Spec: managed-entity-library; descendants and parent context retain precise hits.
func TestEntrySearchManagedDescendantsAndParentContext(t *testing.T) {
	h := newEntrySearchFixture(t)
	require.NoError(t, h.db.Model(h.library).Updates(map[string]any{
		"entity_mode": "managed", "chunking_config": types.ChunkingConfig{EnableParentChild: true},
	}).Error)
	for _, id := range []string{"parent", "child", "grandchild"} {
		require.NoError(t, h.db.Create(&types.Entity{ID: id, SpaceID: 1, LibraryID: h.library.ID, CanonicalName: id, EntityType: "product", NormalizedName: id}).Error)
	}
	require.NoError(t, h.db.Create(&types.LibraryRelationType{LibraryID: h.library.ID, Name: "belongs", Enabled: true, Hierarchical: true}).Error)
	for _, edge := range []types.Relation{
		{ID: "edge1", SubjectID: "child", ObjectID: "parent"},
		{ID: "edge2", SubjectID: "grandchild", ObjectID: "child"},
	} {
		edge.SpaceID, edge.LibraryID, edge.RelationType, edge.Version = 1, h.library.ID, "belongs", 1
		require.NoError(t, h.db.Create(&edge).Error)
	}
	for _, row := range []struct {
		id, entity string
		excluded   bool
	}{
		{"one", "child", false}, {"two", "child", true}, {"three", "grandchild", false},
	} {
		h.add(t, row.id, "article", "2026-10-01", "reference", "semanticlaunch", 2)
		require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: row.id, EntityID: row.entity, Excluded: row.excluded}).Error)
	}
	location := []types.SourceLocation{{Paragraph: 1, Start: 1, End: 30}}
	parent := &types.Chunk{ID: "context", SpaceID: 1, LibraryID: h.library.ID, EntryID: "one", Content: "Surrounding context for the matched passage", ChunkType: types.ChunkTypeParentText, IsEnabled: true}
	parent.SourceLocations = location
	require.NoError(t, h.db.Create(parent).Error)
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=? AND chunk_type=?", "one", types.ChunkTypeText).Update("parent_chunk_id", parent.ID).Error)
	request := types.EntrySearchRequest{Query: "searchfixture", EntityIDs: []string{"parent"}, CollapseSimilar: entrySearchNoCollapse()}
	require.Empty(t, h.search(t, request).Results)
	request.IncludeDescendants = true
	response := h.search(t, request)
	require.Equal(t, []string{"one", "three"}, entrySearchIDs(response))
	for _, result := range response.Results {
		if result.EntryID != "one" {
			continue
		}
		contexts := 0
		for _, snippet := range result.Snippets {
			require.Contains(t, snippet.Content, "body passage")
			require.Empty(t, snippet.Locations)
			if snippet.Context != "" {
				contexts++
				require.Equal(t, parent.Content, snippet.Context)
				require.Equal(t, location, snippet.ContextLocations)
			}
		}
		require.Equal(t, 1, contexts, "a shared parent appears once")
	}
	all := h.search(t, types.EntrySearchRequest{Query: "searchfixture", CollapseSimilar: entrySearchNoCollapse()})
	for _, result := range all.Results {
		if result.EntryID == "two" {
			require.Empty(t, result.Entities)
		}
	}
	// The same stored edges do not expand an extracted library.
	require.NoError(t, h.db.Model(h.library).Update("entity_mode", "extracted").Error)
	require.Empty(t, h.search(t, request).Results)
}

// Spec: managed-entity-library; optional context failures must retain search hits.
func TestEntrySearchParentContextFailurePreservesHits(t *testing.T) {
	h := newEntrySearchFixture(t)
	h.add(t, "one", "article", "2026-10-01", "reference", "semanticlaunch", 1)
	require.NoError(t, h.db.Model(h.library).Update("chunking_config", types.ChunkingConfig{EnableParentChild: true}).Error)
	failed := false
	// Simulate a failure only in the optional parent lookup, after ordinary recall.
	require.NoError(t, h.db.Callback().Row().Before("gorm:row").Register("fail_parent_context", func(db *gorm.DB) {
		if strings.Contains(db.Statement.SQL.String(), "AS parent_id") {
			failed = true
			db.AddError(errors.New("parent storage unavailable"))
		}
	}))
	response := h.search(t, types.EntrySearchRequest{Query: "searchfixture", CollapseSimilar: entrySearchNoCollapse()})
	require.True(t, failed)
	require.Equal(t, []string{"one"}, entrySearchIDs(response))
	require.NotEmpty(t, response.Results[0].Snippets[0].Content)
	require.Empty(t, response.Results[0].Snippets[0].Context)
}

// Document library scopes do not restrict which product library may own a
// selected product or hierarchy root.
func TestEntrySearchCrossLibraryProductDescendants(t *testing.T) {
	h := newEntrySearchFixture(t)
	products := &types.Library{ID: "product-catalog", SpaceID: 1, Name: "Products", Directory: "products", EntityMode: types.EntityModeManaged}
	require.NoError(t, h.db.Create(products).Error)
	require.NoError(t, h.db.Model(h.library).Update("product_library_ids", types.JSON(`["product-catalog"]`)).Error)
	for _, id := range []string{"product-parent", "product-child"} {
		require.NoError(t, h.db.Create(&types.Entity{ID: id, SpaceID: 1, LibraryID: products.ID, CanonicalName: id, NormalizedName: id, EntityType: "product"}).Error)
	}
	require.NoError(t, h.db.Create(&types.LibraryRelationType{LibraryID: products.ID, Name: "belongs", Enabled: true, Hierarchical: true}).Error)
	require.NoError(t, h.db.Create(&types.Relation{ID: "cross-library-hierarchy", SpaceID: 1, LibraryID: products.ID, SubjectID: "product-child", ObjectID: "product-parent", RelationType: "belongs", Version: 1}).Error)
	h.add(t, "cross-library-document", "article", "2026-10-01", "reference", "semanticlaunch", 1)
	require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: "cross-library-document", EntityID: "product-child", RelationType: "test_report"}).Error)
	response := h.search(t, types.EntrySearchRequest{Query: "searchfixture", EntityIDs: []string{"product-parent"}, IncludeDescendants: true, CollapseSimilar: entrySearchNoCollapse()})
	require.Equal(t, []string{"cross-library-document"}, entrySearchIDs(response))
	require.Equal(t, products.ID, response.Results[0].Entities[0].LibraryID)
	require.Equal(t, "test_report", response.Results[0].Entities[0].RelationType)
	options, err := h.svc.Options(h.ctx, []string{h.library.ID})
	require.NoError(t, err)
	require.Len(t, options.Entities, 2)
}
