package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/types"
)

func managedHistoryFixture(t *testing.T) (*gorm.DB, context.Context, *EntityService) {
	t.Helper()
	db, ctx, svc := entityFixture(t)
	schema, _ := json.Marshal(types.EntitySchema{Kinds: []types.EntityKind{{Key: "product", Name: "Product", ParentKinds: []string{"product"}}}, Fields: []types.EntityFieldDefinition{{Key: "capacity", Name: "Capacity", Type: "number"}}})
	require.NoError(t, db.Model(&types.Library{}).Where("id=?", "entity-library").Updates(map[string]any{"entity_mode": "managed", "entity_schema": string(schema)}).Error)
	return db, ctx, svc
}

// Spec: managed catalog evolution. Effective dates order snapshots; revisions
// break same-date ties and stale writes cannot append history.
func TestManagedEntityAttributeTimeline(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	first, backdated, current, future := now.Add(-72*time.Hour), now.Add(-48*time.Hour), now.Add(-24*time.Hour), now.Add(24*time.Hour)
	entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Temporal product", Kind: "product", Attributes: map[string]any{"capacity": 1}, EffectiveAt: &first})
	require.NoError(t, err)
	for _, change := range []struct {
		at    time.Time
		value int
		want  int
	}{{current, 2, 2}, {backdated, 3, 2}, {future, 4, 2}, {current, 5, 5}} {
		attrs := map[string]any{"capacity": change.value}
		entity, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Attributes: &attrs, EffectiveAt: &change.at})
		require.NoError(t, err)
		require.EqualValues(t, change.want, entity.Attributes["capacity"])
	}
	for _, point := range []struct {
		at    time.Time
		known bool
		value int
	}{{first.Add(-time.Second), false, 0}, {first, true, 1}, {backdated, true, 3}, {current, true, 5}, {future, true, 4}} {
		result, err := svc.EntityAttributes(ctx, entity.ID, point.at)
		require.NoError(t, err)
		require.Equal(t, point.known, result.Known)
		if point.known {
			require.EqualValues(t, point.value, result.Attributes["capacity"])
		} else {
			require.Empty(t, result.Attributes)
		}
	}
	detail, err := svc.EntityDetail(ctx, entity.ID)
	require.NoError(t, err)
	require.EqualValues(t, 5, detail.Entity.Attributes["capacity"])
	require.NoError(t, hydrateManagedEntityAttributes(ctx, db, []*types.Entity{entity}, future))
	require.EqualValues(t, 4, entity.Attributes["capacity"])
	history, err := svc.EntityAttributeHistory(ctx, entity.ID, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 5, history.Total)
	_, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: 1, Attributes: &map[string]any{"capacity": 9}})
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&types.EntityAttributeVersion{}).Where("entity_id=?", entity.ID).Count(&count).Error)
	require.EqualValues(t, 5, count)
	// A currently empty field may still be used by a scheduled snapshot.
	empty := map[string]any{}
	_, err = svc.UpdateManagedEntity(ctx, entity.ID, types.ManagedEntityPatch{Revision: entity.Revision, Attributes: &empty})
	require.NoError(t, err)
	var before types.Library
	require.NoError(t, db.First(&before, "id=?", "entity-library").Error)
	after := before
	after.EntitySchema.Kinds = append(append([]types.EntityKind{}, before.EntitySchema.Kinds...), types.EntityKind{Key: "other", Name: "Other"})
	after.EntitySchema.Fields = append([]types.EntityFieldDefinition{}, before.EntitySchema.Fields...)
	after.EntitySchema.Fields[0].Kinds = []string{"other"}
	require.ErrorContains(t, validateManagedCatalogChange(db, &before, &after), "attribute history")
}

// Spec: managed catalog evolution. Reorganizations retain source timelines,
// preserve moved evidence/relationship identities and roll back conflicts.
func TestManagedEntityReorganizationPreservesProvenance(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	create := func(name string) *types.Entity {
		e, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: name, Code: "CODE-" + name, Kind: "product", Attributes: map[string]any{"capacity": 1}})
		require.NoError(t, err)
		return e
	}
	target, source, peer := create("Target product"), create("Source product"), create("Peer product")
	require.NoError(t, db.Create(&types.LibraryRelationType{LibraryID: "entity-library", Name: "related", Enabled: true, Fields: []types.EntityFieldDefinition{{Key: "strength", Name: "Strength", Type: "number"}}}).Error)
	edge, err := svc.SaveManagedRelation(ctx, "entity-library", "", types.ManagedRelationInput{SubjectID: source.ID, ObjectID: peer.ID, RelationType: "related", Attributes: map[string]any{"strength": 7}})
	require.NoError(t, err)
	entry := entityArticle(t, db, "reorganization-entry")
	require.NoError(t, db.Create(&types.EntryEntity{SpaceID: 1, EntryID: entry.ID, EntityID: source.ID, Origin: "manual", RelationType: "references", Evidence: []types.SourceLocation{{Start: 1, End: 4}}, BodyHash: "original-body"}).Error)
	future := time.Now().Add(24 * time.Hour)
	attrs := map[string]any{"capacity": 8}
	source, err = svc.UpdateManagedEntity(ctx, source.ID, types.ManagedEntityPatch{Revision: source.Revision, Attributes: &attrs, EffectiveAt: &future})
	require.NoError(t, err)
	target, err = svc.MergeManagedEntities(ctx, target.ID, types.ManagedEntityMergeInput{Revision: target.Revision, SourceID: source.ID, SourceRevision: source.Revision, Attributes: map[string]any{"capacity": 2}})
	require.NoError(t, err)
	require.Contains(t, target.Aliases, "CODE-Source product")
	oldTimeline, err := svc.EntityAttributes(ctx, source.ID, future)
	require.NoError(t, err)
	require.EqualValues(t, 8, oldTimeline.Attributes["capacity"])
	futureTarget, err := svc.EntityAttributes(ctx, target.ID, future)
	require.NoError(t, err)
	require.EqualValues(t, 2, futureTarget.Attributes["capacity"])
	var moved types.Relation
	require.NoError(t, db.First(&moved, "id=?", edge.ID).Error)
	require.Equal(t, target.ID, moved.SubjectID)
	require.EqualValues(t, 7, moved.Attributes["strength"])
	var link types.EntryEntity
	require.NoError(t, db.First(&link, "entry_id=? AND entity_id=?", entry.ID, target.ID).Error)
	require.Equal(t, "manual", link.Origin)
	require.Equal(t, "original-body", link.BodyHash)
	require.Len(t, link.Evidence, 1)
	split, err := svc.SplitManagedEntity(ctx, target.ID, types.ManagedEntitySplitInput{Revision: target.Revision, NewEntity: types.ManagedEntityInput{Name: "Split product", Kind: "product", Attributes: map[string]any{"capacity": 3}}, EntryIDs: []string{entry.ID}, RelationIDs: []string{edge.ID}})
	require.NoError(t, err)
	require.NoError(t, db.First(&moved, "id=?", edge.ID).Error)
	require.Equal(t, split.ID, moved.SubjectID)
	require.EqualValues(t, 7, moved.Attributes["strength"])
	require.NoError(t, db.First(&link, "entry_id=? AND entity_id=?", entry.ID, split.ID).Error)
	require.Len(t, link.Evidence, 1)
	records, err := svc.EntityReorganizations(ctx, target.ID)
	require.NoError(t, err)
	require.Len(t, records, 2)
	targetDetail, err := svc.EntityDetail(ctx, target.ID)
	require.NoError(t, err)
	require.Nil(t, targetDetail.Entity.ArchivedAt)
	conflict := create("Conflicting product")
	require.NoError(t, db.Create(&types.EntryEntity{SpaceID: 1, EntryID: entry.ID, EntityID: conflict.ID, Origin: "manual", Excluded: true, RelationType: "references"}).Error)
	_, err = svc.MergeManagedEntities(ctx, split.ID, types.ManagedEntityMergeInput{Revision: split.Revision, SourceID: conflict.ID, SourceRevision: conflict.Revision, Attributes: map[string]any{"capacity": 4}})
	require.Error(t, err)
	var unchanged types.Entity
	require.NoError(t, db.First(&unchanged, "id=?", conflict.ID).Error)
	require.Empty(t, unchanged.MergedIntoID)
	require.Nil(t, unchanged.ArchivedAt)
	var historyCount int64
	require.NoError(t, db.Model(&types.EntityAttributeVersion{}).Where("entity_id=?", split.ID).Count(&historyCount).Error)
	require.EqualValues(t, 1, historyCount)
	// A hierarchy link between the two merge participants cannot disappear silently.
	require.NoError(t, db.Create(&types.LibraryRelationType{LibraryID: "entity-library", Name: "belongs", Enabled: true, Hierarchical: true}).Error)
	_, err = svc.SaveManagedRelation(ctx, "entity-library", "", types.ManagedRelationInput{SubjectID: conflict.ID, ObjectID: peer.ID, RelationType: "belongs"})
	require.NoError(t, err)
	_, err = svc.MergeManagedEntities(ctx, peer.ID, types.ManagedEntityMergeInput{Revision: peer.Revision, SourceID: conflict.ID, SourceRevision: conflict.Revision, Attributes: map[string]any{}})
	require.Error(t, err)
}

type mergeDropFixture struct {
	db                                                                 *gorm.DB
	ctx                                                                context.Context
	svc                                                                *EntityService
	target, source                                                     *types.Entity
	sourceParent, targetParent, sourceDuplicate, targetDuplicate, keep *types.Relation
}

func newMergeDropFixture(t *testing.T) mergeDropFixture {
	t.Helper()
	db, ctx, svc := managedHistoryFixture(t)
	create := func(name string) *types.Entity {
		row, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: name, Kind: "product", Attributes: map[string]any{}})
		require.NoError(t, err)
		return row
	}
	target, source, parentA, parentB, peer, other := create("Target"), create("Source"), create("Parent A"), create("Parent B"), create("Peer"), create("Other")
	require.NoError(t, db.Create(&[]types.LibraryRelationType{{LibraryID: "entity-library", Name: "belongs", Enabled: true, Hierarchical: true}, {LibraryID: "entity-library", Name: "related", Enabled: true}}).Error)
	relate := func(from, to *types.Entity, kind string) *types.Relation {
		row, err := svc.SaveManagedRelation(ctx, "entity-library", "", types.ManagedRelationInput{SubjectID: from.ID, ObjectID: to.ID, RelationType: kind})
		require.NoError(t, err)
		return row
	}
	return mergeDropFixture{db: db, ctx: ctx, svc: svc, target: target, source: source, sourceParent: relate(source, parentA, "belongs"), targetParent: relate(target, parentB, "belongs"), sourceDuplicate: relate(source, peer, "related"), targetDuplicate: relate(target, peer, "related"), keep: relate(source, other, "related")}
}

// Requirement 4: explicit source-edge drops resolve parent/duplicate conflicts
// without discarding the target's relationships or unselected source edges.
func TestManagedEntityMergeDropsChosenSourceRelations(t *testing.T) {
	f := newMergeDropFixture(t)
	var err error
	f.source, err = f.svc.UpdateManagedEntity(f.ctx, f.source.ID, types.ManagedEntityPatch{Revision: f.source.Revision, AutoMatch: &map[string]bool{"Source": false}})
	require.NoError(t, err)
	f.target, err = f.svc.UpdateManagedEntity(f.ctx, f.target.ID, types.ManagedEntityPatch{Revision: f.target.Revision, AutoMatch: &map[string]bool{"Target": true}})
	require.NoError(t, err)
	input := types.ManagedEntityMergeInput{Revision: f.target.Revision, SourceID: f.source.ID, SourceRevision: f.source.Revision, Attributes: map[string]any{}}
	_, err = f.svc.MergeManagedEntities(f.ctx, f.target.ID, input)
	require.Error(t, err, "existing merge constraints still apply when no drops are supplied")
	input.DropRelationIDs = []string{f.sourceParent.ID, f.sourceDuplicate.ID}
	result, err := f.svc.MergeManagedEntities(f.ctx, f.target.ID, input)
	require.NoError(t, err)
	require.Equal(t, f.target.ID, result.ID)
	require.Equal(t, map[string]bool{"source": false, "target": true}, result.AutoMatch)
	var rows []types.Relation
	require.NoError(t, f.db.Order("id").Find(&rows).Error)
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.NotEqual(t, f.source.ID, row.SubjectID)
		require.NotEqual(t, f.source.ID, row.ObjectID)
	}
	var kept types.Relation
	require.NoError(t, f.db.First(&kept, "id=?", f.keep.ID).Error)
	require.Equal(t, f.target.ID, kept.SubjectID)
	for _, dropped := range []*types.Relation{f.sourceParent, f.sourceDuplicate} {
		var log types.OperationLog
		require.NoError(t, f.db.First(&log, "object_id=? AND operation_type='delete_relation'", dropped.ID).Error)
		var before types.Relation
		require.NoError(t, json.Unmarshal(log.Before, &before))
		require.Equal(t, dropped.ID, before.ID)
		require.Equal(t, dropped.SubjectID, before.SubjectID)
		require.Equal(t, dropped.ObjectID, before.ObjectID)
		require.Equal(t, dropped.Version, before.Version)
		require.Equal(t, dropped.Attributes, before.Attributes)
		require.True(t, dropped.CreatedAt.Equal(before.CreatedAt))
		var pending []types.GraphPendingSync
		require.NoError(t, f.db.Where("relation_id=? AND deleted=true", dropped.ID).Find(&pending).Error)
		require.Len(t, pending, 2)
		require.ElementsMatch(t, []string{dropped.SubjectID, dropped.ObjectID}, []string{pending[0].EntityID, pending[1].EntityID})
		require.Equal(t, dropped.Version+1, pending[0].RelationVersion)
	}
}

// Requirement 4: invalid drop selections and later merge failures leave
// relations, audit rows and graph pending records unchanged.
func TestManagedEntityMergeDropRollback(t *testing.T) {
	f := newMergeDropFixture(t)
	input := types.ManagedEntityMergeInput{Revision: f.target.Revision, SourceID: f.source.ID, SourceRevision: f.source.Revision, Attributes: map[string]any{}}
	for _, ids := range [][]string{{f.sourceParent.ID, "unknown-relation"}, {f.sourceParent.ID, f.targetParent.ID}, {f.sourceParent.ID, f.sourceParent.ID}, {f.sourceParent.ID}} {
		input.DropRelationIDs = ids
		_, err := f.svc.MergeManagedEntities(f.ctx, f.target.ID, input)
		require.Error(t, err)
		var count int64
		require.NoError(t, f.db.Model(&types.Relation{}).Where("id=?", f.sourceParent.ID).Count(&count).Error)
		require.EqualValues(t, 1, count)
		require.NoError(t, f.db.Model(&types.OperationLog{}).Where("operation_type='delete_relation'").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, f.db.Model(&types.GraphPendingSync{}).Where("deleted=true").Count(&count).Error)
		require.Zero(t, count)
		var source types.Entity
		require.NoError(t, f.db.First(&source, "id=?", f.source.ID).Error)
		require.Empty(t, source.MergedIntoID)
		require.Equal(t, f.source.Revision, source.Revision)
	}
}
