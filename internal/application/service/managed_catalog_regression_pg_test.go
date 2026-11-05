package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service/file"
	"reliquary/internal/types"
)

// Regression: issue 4/5. Editing current values carries only changed fields into pending snapshots.
func TestManagedEntityEditsPreservePendingValues(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	schema := types.EntitySchema{Kinds: []types.EntityKind{{Key: "product", Name: "Product"}}, Fields: []types.EntityFieldDefinition{{Key: "price", Name: "Price", Type: "number"}, {Key: "status", Name: "Status", Type: "text"}}}
	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.Library{}).Where("id=?", "entity-library").Update("entity_schema", string(encoded)).Error)
	entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Scheduled product", Kind: "product", Attributes: map[string]any{"price": 100, "status": "draft"}})
	require.NoError(t, err)
	future := time.Now().Add(24 * time.Hour)
	attrs := map[string]any{"price": 200, "status": "draft"}
	entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Attributes: &attrs, EffectiveAt: &future})
	require.NoError(t, err)
	name := "Renamed product"
	attrs = map[string]any{"price": 100, "status": "draft"}
	entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Name: &name, Attributes: &attrs})
	require.NoError(t, err)
	history, err := svc.EntityAttributeHistory(ctx, entity.ID, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 2, history.Total)
	attrs["status"] = "released"
	entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Attributes: &attrs})
	require.NoError(t, err)
	result, err := svc.EntityAttributes(ctx, entity.ID, future)
	require.NoError(t, err)
	require.Equal(t, "released", result.Attributes["status"])
	require.EqualValues(t, 200, result.Attributes["price"])
}

// Regression (medium): issue 7 / MEL-03. Backdated category changes preserve
// earlier history while removing old-category fields from all later snapshots.
func TestManagedEntityKindChangePreservesHistory(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	schema := types.EntitySchema{Kinds: []types.EntityKind{{Key: "product", Name: "Product"}, {Key: "platform", Name: "Platform"}}, Fields: []types.EntityFieldDefinition{{Key: "capacity", Name: "Capacity", Type: "number", Kinds: []string{"product"}}}}
	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.Library{}).Where("id=?", "entity-library").Update("entity_schema", string(encoded)).Error)
	now := time.Now().UTC().Truncate(time.Second)
	past, backdated, current, future := now.Add(-40*24*time.Hour), now.Add(-35*24*time.Hour), now.Add(-5*24*time.Hour), now.Add(5*24*time.Hour)
	entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Changing category", Kind: "product", Attributes: map[string]any{"capacity": 1}, EffectiveAt: &past})
	require.NoError(t, err)
	attrs := map[string]any{"capacity": 2}
	entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Attributes: &attrs, EffectiveAt: &current})
	require.NoError(t, err)
	attrs = map[string]any{"capacity": 3}
	entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Attributes: &attrs, EffectiveAt: &future})
	require.NoError(t, err)
	kind, empty := "platform", map[string]any{}
	entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Kind: &kind, Attributes: &empty, EffectiveAt: &backdated})
	require.NoError(t, err)
	require.Equal(t, "platform", entity.Kind)
	require.Empty(t, entity.Attributes)
	old, err := svc.EntityAttributes(ctx, entity.ID, past)
	require.NoError(t, err)
	require.EqualValues(t, 1, old.Attributes["capacity"])
	latest, err := svc.EntityAttributes(ctx, entity.ID, current)
	require.NoError(t, err)
	require.Empty(t, latest.Attributes)
	pending, err := svc.EntityAttributes(ctx, entity.ID, future)
	require.NoError(t, err)
	require.Empty(t, pending.Attributes)
}

// Regression: issue 10. Archived names are reusable; referenced products remain protected.
func TestManagedEntityDeletionAndArchivedNameReuse(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Reusable product", Kind: "product"})
	require.NoError(t, err)
	archived := true
	entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Archived: &archived})
	require.NoError(t, err)
	replacement, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: entity.CanonicalName, Kind: "product"})
	require.NoError(t, err)
	archived = false
	_, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Archived: &archived})
	require.Error(t, err)
	entry := entityArticle(t, db, "referenced-product")
	require.NoError(t, svc.SetEntryEntityLink(ctx, entry.ID, entity.ID, true))
	require.ErrorContains(t, svc.DeleteManagedEntity(ctx, entity.ID, entity.Revision), "without documents")
	require.NoError(t, svc.DeleteManagedEntity(ctx, replacement.ID, replacement.Revision))
	var count int64
	require.NoError(t, db.Model(&types.Entity{}).Where("id=?", replacement.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.EntityAttributeVersion{}).Where("entity_id=?", replacement.ID).Count(&count).Error)
	require.Zero(t, count)
}

// Regression: issue 22. Only the owning library may reference an attachment; historical owners retain it.
func TestManagedEntityAttachmentOwnershipAndCleanup(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	files := file.NewLocalFileService(t.TempDir(), "")
	svc.SetEntityFileService(files)
	schema := types.EntitySchema{Kinds: []types.EntityKind{{Key: "product", Name: "Product"}}, Fields: []types.EntityFieldDefinition{{Key: "manual", Name: "Manual", Type: "attachment"}}}
	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.Library{}).Where("id=?", "entity-library").Update("entity_schema", string(encoded)).Error)
	other := types.Library{ID: "other-products", SpaceID: 1, Directory: "other-products", Name: "Other", EntityMode: "managed", EntitySchema: schema}
	require.NoError(t, db.Create(&other).Error)
	attachment, err := svc.UploadEntityAttachment(ctx, "entity-library", "manual.txt", []byte("Synthetic manual"))
	require.NoError(t, err)
	_, err = svc.CreateManagedEntity(ctx, other.ID, types.ManagedEntityInput{Name: "Foreign attachment", Kind: "product", Attributes: map[string]any{"manual": attachment.ID}})
	require.ErrorContains(t, err, "allowed library")
	entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Local attachment", Kind: "product", Attributes: map[string]any{"manual": attachment.ID}})
	require.NoError(t, err)
	empty := map[string]any{}
	_, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Attributes: &empty})
	require.NoError(t, err)
	require.ErrorContains(t, svc.DeleteEntityAttachment(ctx, attachment.ID), "still referenced")
	unused, err := svc.UploadEntityAttachment(ctx, "entity-library", "unused.txt", []byte("Unused manual"))
	require.NoError(t, err)
	require.NoError(t, svc.DeleteEntityAttachment(ctx, unused.ID))
	_, err = svc.EntityAttachment(ctx, unused.ID)
	require.Error(t, err)
	_, err = files.GetFile(ctx, unused.Path)
	require.Error(t, err)
}
