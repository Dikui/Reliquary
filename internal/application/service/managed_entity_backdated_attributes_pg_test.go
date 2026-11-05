package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Regression (medium): MEL-03. Backdated edits carry changed fields through all
// later snapshots, preserving unrelated values and logging each changed snapshot.
func TestManagedEntityBackdatedAttributeEditsReachLaterSnapshots(t *testing.T) {
	for _, operation := range []string{"update", "merge"} {
		t.Run(operation, func(t *testing.T) {
			db, ctx, svc := managedHistoryFixture(t)
			schema := types.EntitySchema{Kinds: []types.EntityKind{{Key: "product", Name: "Product"}}, Fields: []types.EntityFieldDefinition{{Key: "price", Name: "Price", Type: "number"}, {Key: "status", Name: "Status", Type: "text"}}}
			encoded, err := json.Marshal(schema)
			require.NoError(t, err)
			require.NoError(t, db.Model(&types.Library{}).Where("id=?", "entity-library").Update("entity_schema", string(encoded)).Error)
			now := time.Now().UTC().Truncate(time.Second)
			first, backdated, current, future := now.Add(-40*24*time.Hour), now.Add(-35*24*time.Hour), now.Add(-5*24*time.Hour), now.Add(5*24*time.Hour)
			entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Temporal product", Kind: "product", Attributes: map[string]any{"price": 100, "status": "draft"}, EffectiveAt: &first})
			require.NoError(t, err)
			for _, change := range []struct {
				at         time.Time
				attributes map[string]any
			}{
				{current, map[string]any{"price": 100, "status": "released"}},
				{future, map[string]any{"price": 200, "status": "planned"}},
			} {
				entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Attributes: &change.attributes, EffectiveAt: &change.at})
				require.NoError(t, err)
			}
			beforeHistory, err := svc.EntityAttributeHistory(ctx, entity.ID, 1, 20)
			require.NoError(t, err)
			require.Len(t, beforeHistory.Items, 3)
			attributes := map[string]any{"price": 120, "status": "draft"}
			if operation == "merge" {
				source, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Source product", Kind: "product", Attributes: map[string]any{"price": 300, "status": "draft"}})
				require.NoError(t, err)
				entity, err = svc.MergeManagedEntities(ctx, entity.ID, types.ManagedEntityMergeInput{Revision: entity.Revision, SourceID: source.ID, SourceRevision: source.Revision, Attributes: attributes, EffectiveAt: &backdated})
				require.NoError(t, err)
			} else {
				entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Attributes: &attributes, EffectiveAt: &backdated})
				require.NoError(t, err)
			}
			wantCurrent := map[string]any{"price": float64(120), "status": "released"}
			require.Equal(t, wantCurrent, entity.Attributes)
			detail, err := svc.EntityDetail(ctx, entity.ID)
			require.NoError(t, err)
			require.Equal(t, wantCurrent, detail.Entity.Attributes)
			currentAttributes, err := svc.EntityAttributes(ctx, entity.ID, now)
			require.NoError(t, err)
			require.Equal(t, wantCurrent, currentAttributes.Attributes)
			// MEL-03 also requires the stored current-value column to stay in sync.
			var stored types.Entity
			require.NoError(t, db.First(&stored, "id=?", entity.ID).Error)
			require.Equal(t, wantCurrent, stored.Attributes)
			history, err := svc.EntityAttributeHistory(ctx, entity.ID, 1, 20)
			require.NoError(t, err)
			require.EqualValues(t, 4, history.Total)
			require.Len(t, history.Items, 4)
			require.Equal(t, map[string]any{"price": float64(120), "status": "planned"}, history.Items[0].Attributes)
			require.Equal(t, wantCurrent, history.Items[1].Attributes)
			require.Equal(t, map[string]any{"price": float64(120), "status": "draft"}, history.Items[2].Attributes)
			require.Equal(t, beforeHistory.Items[2], history.Items[3], "the earlier baseline must remain unchanged")
			futureAttributes, err := svc.EntityAttributes(ctx, entity.ID, future)
			require.NoError(t, err)
			require.Equal(t, map[string]any{"price": float64(120), "status": "planned"}, futureAttributes.Attributes)
			logs, err := NewOperationLogService(db).List(ctx, "update_pending_entity_attributes", 1, 20)
			require.NoError(t, err)
			require.Len(t, logs.Items, 2)
			var loggedIDs []string
			for _, log := range logs.Items {
				require.Equal(t, entity.ID, log.ObjectID)
				var before, after types.EntityAttributeVersion
				require.NoError(t, json.Unmarshal(log.Before, &before))
				require.NoError(t, json.Unmarshal(log.After, &after))
				require.NotEqual(t, before.Attributes, after.Attributes)
				require.EqualValues(t, 120, after.Attributes["price"])
				require.Equal(t, before.Attributes["status"], after.Attributes["status"])
				after.Attributes = before.Attributes
				require.Equal(t, before, after, "propagation must retain snapshot identity and timestamps")
				loggedIDs = append(loggedIDs, before.ID)
			}
			require.ElementsMatch(t, []string{beforeHistory.Items[0].ID, beforeHistory.Items[1].ID}, loggedIDs)
		})
	}
}

// Regression (medium): MEL-03 / ADR 0022. Repeating an identical historical
// snapshot neither appends history nor rewrites later snapshots.
func TestManagedEntityIdenticalHistoricalAttributeSaveKeepsSnapshots(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	first, current, future := now.Add(-40*24*time.Hour), now.Add(-5*24*time.Hour), now.Add(5*24*time.Hour)
	entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Unchanged historical product", Kind: "product", Attributes: map[string]any{"capacity": 1}, EffectiveAt: &first})
	require.NoError(t, err)
	for _, change := range []struct {
		at    time.Time
		value int
	}{{current, 2}, {future, 3}} {
		attributes := map[string]any{"capacity": change.value}
		entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Attributes: &attributes, EffectiveAt: &change.at})
		require.NoError(t, err)
	}
	before, err := svc.EntityAttributeHistory(ctx, entity.ID, 1, 20)
	require.NoError(t, err)
	attributes := map[string]any{"capacity": 1}
	entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Attributes: &attributes, EffectiveAt: &first})
	require.NoError(t, err)
	require.EqualValues(t, 2, entity.Attributes["capacity"])
	after, err := svc.EntityAttributeHistory(ctx, entity.ID, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 3, after.Total)
	require.Equal(t, before, after)
	logs, err := NewOperationLogService(db).List(ctx, "update_pending_entity_attributes", 1, 20)
	require.NoError(t, err)
	require.Empty(t, logs.Items)
}
