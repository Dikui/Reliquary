package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
)

// Regression (medium), MEL-09: withdrawing a document's sources must not
// update an ended relationship's version or modification time.
func TestManagedRelationHistoryUnchangedWhenSourceEntryIsDeleted(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	require.NoError(t, db.Create(&types.LibraryRelationType{LibraryID: "entity-library", Name: "connects", Enabled: true}).Error)
	a, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Atlas", Kind: "product"})
	require.NoError(t, err)
	b, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Beacon", Kind: "product"})
	require.NoError(t, err)
	start := time.Now().Add(-2 * time.Hour)
	old, err := svc.SaveManagedRelation(ctx, "entity-library", "", types.ManagedRelationInput{SubjectID: a.ID, ObjectID: b.ID, RelationType: "connects", ValidFrom: &start})
	require.NoError(t, err)
	entry := entityArticle(t, db, "source-entry")
	require.NoError(t, db.Create(&types.RelationSource{RelationID: old.ID, EntryID: entry.ID, Evidence: []types.RelationEvidence{}}).Error)
	change := start.Add(time.Hour)
	current, err := svc.SaveManagedRelation(ctx, "entity-library", old.ID, types.ManagedRelationInput{Version: old.Version, SubjectID: a.ID, ObjectID: b.ID, RelationType: "connects", ValidFrom: &change})
	require.NoError(t, err)
	before, err := svc.ListRelations(ctx, a.ID, "", start)
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Len(t, before[0].Relations, 1)
	require.NoError(t, repository.NewEntryRepository(db).DeleteEntry(ctx, 1, entry.ID))
	after, err := svc.ListRelations(ctx, a.ID, "", start)
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Len(t, after[0].Relations, 1)
	require.Equal(t, before[0].Relations[0].Relation, after[0].Relations[0].Relation)
	require.Empty(t, after[0].Relations[0].Sources)
	live, err := svc.ListRelations(ctx, a.ID, "")
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.Len(t, live[0].Relations, 1)
	require.Equal(t, current.ID, live[0].Relations[0].ID)
	require.Equal(t, current.Version+1, live[0].Relations[0].Version)
	require.Empty(t, live[0].Relations[0].Sources)
}

// Regression (medium), MEL-09: suppressions retained after conversion must not
// delete current or historical managed relationships when another library merges.
func TestOtherLibraryMergesPreserveManagedRelationHistory(t *testing.T) {
	for _, operation := range []string{"entity", "relation_type"} {
		t.Run(operation, func(t *testing.T) {
			db, ctx, svc, entries := graphFixture(t)
			entry := graphEntry(t, db, "extracted-source", "", time.Now())
			graphPublish(t, db, ctx, entries, entry, graphInput("a", "合作", "b"))
			library := types.Library{ID: "managed-products", SpaceID: 1, Name: "Products", Directory: "products", EntityMode: types.EntityModeManaged, EntitySchema: types.EntitySchema{Kinds: []types.EntityKind{{Key: "product", Name: "Product"}}}}
			require.NoError(t, db.Create(&library).Error)
			require.NoError(t, db.Create(&types.LibraryRelationType{LibraryID: library.ID, Name: "connects", Enabled: true}).Error)
			a, err := svc.CreateManagedEntity(ctx, library.ID, types.ManagedEntityInput{Name: "Atlas", Kind: "product"})
			require.NoError(t, err)
			b, err := svc.CreateManagedEntity(ctx, library.ID, types.ManagedEntityInput{Name: "Beacon", Kind: "product"})
			require.NoError(t, err)
			// Conversion retains suppressions from earlier extracted relationships.
			require.NoError(t, db.Create(&types.RelationSuppression{SpaceID: 1, LibraryID: library.ID, SubjectID: a.ID, ObjectID: b.ID, RelationType: "connects", Actor: "editor"}).Error)
			start := time.Now().Add(-2 * time.Hour)
			old, err := svc.SaveManagedRelation(ctx, library.ID, "", types.ManagedRelationInput{SubjectID: a.ID, ObjectID: b.ID, RelationType: "connects", ValidFrom: &start})
			require.NoError(t, err)
			change := start.Add(time.Hour)
			_, err = svc.SaveManagedRelation(ctx, library.ID, old.ID, types.ManagedRelationInput{Version: old.Version, SubjectID: a.ID, ObjectID: b.ID, RelationType: "connects", ValidFrom: &change})
			require.NoError(t, err)
			history, err := svc.ListRelations(ctx, a.ID, "", start)
			require.NoError(t, err)
			current, err := svc.ListRelations(ctx, a.ID, "")
			require.NoError(t, err)
			require.Len(t, history, 1)
			require.Len(t, current, 1)
			if operation == "entity" {
				require.NoError(t, svc.MergeEntities(ctx, "a", "c"))
			} else {
				require.NoError(t, svc.MergeRelationTypes(ctx, "entity-library", "合作", "投资"))
			}
			afterHistory, err := svc.ListRelations(ctx, a.ID, "", start)
			require.NoError(t, err)
			require.Equal(t, history, afterHistory)
			afterCurrent, err := svc.ListRelations(ctx, a.ID, "")
			require.NoError(t, err)
			require.Equal(t, current, afterCurrent)
		})
	}
}
