package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// MEL-08 (medium regression): cross-category merges accept visible result fields
// plus unchanged target archived values, while preserving the source timeline.
func TestManagedEntityMergeAcrossKindsPreservesTargetArchivedAttributes(t *testing.T) {
	for _, targetLegacy := range []struct {
		name       string
		attributes map[string]any
	}{
		{name: "target has archived values", attributes: map[string]any{"legacy": "Target legacy", "empty_legacy": ""}},
		{name: "target has no archived values", attributes: map[string]any{}},
	} {
		t.Run(targetLegacy.name, func(t *testing.T) {
			db, ctx, svc := managedHistoryFixture(t)
			schema := types.EntitySchema{
				Kinds: []types.EntityKind{{Key: "product", Name: "Product"}, {Key: "line", Name: "Line"}},
				Fields: []types.EntityFieldDefinition{
					{Key: "capacity", Name: "Capacity", Type: "number"},
					{Key: "inherited", Name: "Inherited", Type: "text"},
					{Key: "source_only", Name: "Line size", Type: "number", Kinds: []string{"line"}},
					{Key: "legacy", Name: "Legacy", Type: "text"},
					{Key: "empty_legacy", Name: "Empty legacy", Type: "text"},
					{Key: "retired", Name: "Retired", Type: "list"},
				},
			}
			setSchema := func() {
				t.Helper()
				value, err := json.Marshal(schema)
				require.NoError(t, err)
				require.NoError(t, db.Model(&types.Library{}).Where("id=?", "entity-library").Update("entity_schema", string(value)).Error)
			}
			setSchema()
			targetAttributes := map[string]any{"capacity": 2}
			for key, value := range targetLegacy.attributes {
				targetAttributes[key] = value
			}
			target, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Target product", Kind: "product", Attributes: targetAttributes})
			require.NoError(t, err)
			sourceAttributes := map[string]any{"capacity": 9, "inherited": "Source value", "source_only": 17, "legacy": "Source legacy", "retired": []string{"Old value"}}
			source, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Source line", Kind: "line", Attributes: sourceAttributes})
			require.NoError(t, err)
			// Retire fields after assigning their original values, as in a maintained catalog.
			for index := 3; index < len(schema.Fields); index++ {
				schema.Fields[index].Archived = true
			}
			setSchema()
			before, err := svc.EntityAttributeHistory(ctx, source.ID, 1, 20)
			require.NoError(t, err)
			input := types.ManagedEntityMergeInput{Revision: target.Revision, SourceID: source.ID, SourceRevision: source.Revision, Attributes: sourceAttributes}
			_, err = svc.MergeManagedEntities(ctx, target.ID, input)
			require.ErrorContains(t, err, "not defined")
			input.Attributes = map[string]any{"capacity": 2, "inherited": "Source value", "legacy": "Source legacy"}
			_, err = svc.MergeManagedEntities(ctx, target.ID, input)
			require.ErrorContains(t, err, "archived fields cannot be changed")

			// This is the explicit complete payload produced by the merge dialog.
			input.Attributes = map[string]any{"capacity": 2, "inherited": "Source value"}
			for key, value := range targetLegacy.attributes {
				input.Attributes[key] = value
			}
			merged, err := svc.MergeManagedEntities(ctx, target.ID, input)
			require.NoError(t, err)
			require.Equal(t, target.ID, merged.ID)
			require.Equal(t, "product", merged.Kind)
			require.EqualValues(t, 2, merged.Attributes["capacity"])
			require.Equal(t, "Source value", merged.Attributes["inherited"])
			require.Len(t, merged.Attributes, 2+len(targetLegacy.attributes))
			for key, value := range targetLegacy.attributes {
				require.Equal(t, value, merged.Attributes[key])
			}
			after, err := svc.EntityAttributeHistory(ctx, source.ID, 1, 20)
			require.NoError(t, err)
			require.Equal(t, before, after)
			sourceSnapshot, err := svc.EntityAttributes(ctx, source.ID, time.Now())
			require.NoError(t, err)
			require.EqualValues(t, 17, sourceSnapshot.Attributes["source_only"])
			require.Equal(t, "Source legacy", sourceSnapshot.Attributes["legacy"])
			require.Equal(t, []any{"Old value"}, sourceSnapshot.Attributes["retired"])
		})
	}
}
