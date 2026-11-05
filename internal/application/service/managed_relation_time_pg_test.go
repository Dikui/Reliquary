package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Spec (medium), MEL-04: a hierarchy cycle must exist at one common instant.
func TestManagedRelationCyclesRespectHistoricalIntervals(t *testing.T) {
	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		secondFrom time.Time
		newFrom    time.Time
		cycle      bool
	}{
		{"overlapping_history", start.Add(24 * time.Hour), start, true},
		{"adjacent_path_edges", start.Add(48 * time.Hour), start, false},
		{"after_historical_path", start.Add(24 * time.Hour), start.Add(72 * time.Hour), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, ctx, svc := managedHistoryFixture(t)
			require.NoError(t, db.Create(&types.LibraryRelationType{LibraryID: "entity-library", Name: "belongs", Enabled: true, Hierarchical: true}).Error)
			create := func(name string) *types.Entity {
				entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: name, Kind: "product"})
				require.NoError(t, err)
				return entity
			}
			a, b, c := create("A"), create("B"), create("C")
			ended := func(subject, object string, from, to time.Time) {
				relation, err := svc.SaveManagedRelation(ctx, "entity-library", "", types.ManagedRelationInput{SubjectID: subject, ObjectID: object, RelationType: "belongs", ValidFrom: &from})
				require.NoError(t, err)
				require.NoError(t, svc.DeleteManagedRelation(ctx, relation.ID, relation.Version, to))
			}
			ended(b.ID, c.ID, start, start.Add(48*time.Hour))
			ended(c.ID, a.ID, test.secondFrom, start.Add(72*time.Hour))
			_, err := svc.SaveManagedRelation(ctx, "entity-library", "", types.ManagedRelationInput{SubjectID: a.ID, ObjectID: b.ID, RelationType: "belongs", ValidFrom: &test.newFrom})
			if test.cycle {
				require.ErrorContains(t, err, "cycle")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Regression (medium), MEL-04: rejecting a backdated edit retains the old row.
func TestManagedRelationConflictingBackdatedPatchRollsBack(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	require.NoError(t, db.Create(&types.LibraryRelationType{LibraryID: "entity-library", Name: "belongs", Enabled: true, Hierarchical: true}).Error)
	create := func(name string) *types.Entity {
		entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: name, Kind: "product"})
		require.NoError(t, err)
		return entity
	}
	a, b, c := create("A"), create("B"), create("C")
	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	old, err := svc.SaveManagedRelation(ctx, "entity-library", "", types.ManagedRelationInput{SubjectID: a.ID, ObjectID: b.ID, RelationType: "belongs", ValidFrom: &start})
	require.NoError(t, err)
	change := start.Add(24 * time.Hour)
	// C already had a parent when the proposed C -> A successor would start.
	_, err = svc.SaveManagedRelation(ctx, "entity-library", "", types.ManagedRelationInput{SubjectID: c.ID, ObjectID: b.ID, RelationType: "belongs", ValidFrom: &start})
	require.NoError(t, err)
	_, err = svc.SaveManagedRelation(ctx, "entity-library", old.ID, types.ManagedRelationInput{Version: old.Version, SubjectID: c.ID, ObjectID: a.ID, RelationType: "belongs", ValidFrom: &change})
	require.ErrorContains(t, err, "only one hierarchy parent")
	groups, err := svc.ListRelations(ctx, a.ID, "", change)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Relations, 1)
	require.Equal(t, old.ID, groups[0].Relations[0].ID)
	require.Nil(t, groups[0].Relations[0].ValidTo)
	require.Equal(t, old.Version, groups[0].Relations[0].Version)
}
