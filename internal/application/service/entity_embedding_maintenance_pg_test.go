package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/embedding"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/testutil/redistest"
	"reliquary/internal/types"
)

func TestManagedEntityWritesSkipNameEmbeddingJobs(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	configureNameEmbedding(t, db, "test-vector", 3)
	queue := &nameRebuildQueue{}
	svc.SetNameEmbeddingRebuilder(NewEntityNameEmbeddingRebuilder(db, &nameRebuildModels{}, queue))

	// Unrelated pending extracted names must not cause a job on any managed edit.
	require.NoError(t, db.Create(&types.Library{ID: "extracted", SpaceID: 1, Name: "Extracted", Directory: "extracted"}).Error)
	extracted := entityRecord("extracted-name", "Extracted name", "company")
	extracted.LibraryID = "extracted"
	extracted.EmbeddingNeedsRebuild = true
	require.NoError(t, db.Create(&extracted).Error)
	create := func(name string) *types.Entity {
		t.Helper()
		entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: name, Kind: "product"})
		require.NoError(t, err)
		require.False(t, entity.EmbeddingNeedsRebuild)
		require.Zero(t, queue.count())
		return entity
	}
	target, source := create("Product"), create("Another product")
	description := "Updated description"
	target, err := svc.UpdateManagedEntity(ctx, target.ID, types.ManagedEntityPatch{Revision: target.Revision, Description: &description})
	require.NoError(t, err)
	require.Zero(t, queue.count())
	name := "Renamed product"
	target, err = svc.UpdateManagedEntity(ctx, target.ID, types.ManagedEntityPatch{Revision: target.Revision, Name: &name})
	require.NoError(t, err)
	require.Equal(t, name, target.CanonicalName)
	require.Zero(t, queue.count())
	target, err = svc.MergeManagedEntities(ctx, target.ID, types.ManagedEntityMergeInput{Revision: target.Revision, SourceID: source.ID, SourceRevision: source.Revision, Attributes: map[string]any{}})
	require.NoError(t, err)
	require.Zero(t, queue.count())
	split, err := svc.SplitManagedEntity(ctx, target.ID, types.ManagedEntitySplitInput{Revision: target.Revision, NewEntity: types.ManagedEntityInput{Name: "Split product", Kind: "product", Attributes: map[string]any{}}})
	require.NoError(t, err)
	require.False(t, split.EmbeddingNeedsRebuild)
	require.Zero(t, queue.count())

	_, err = svc.RenameEntity(ctx, extracted.ID, "Renamed extracted name")
	require.NoError(t, err)
	require.Equal(t, types.TypeEntityNameEmbeddingRebuild, queue.last(t).Type())
	require.Equal(t, 1, queue.count())
}

func TestLegacyManagedNameEmbeddingJobCompletesWithoutModel(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	configureNameEmbedding(t, db, "test-vector", 3)
	entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Old product", Kind: "product"})
	require.NoError(t, err)
	require.NoError(t, db.Model(entity).Update("embedding_needs_rebuild", true).Error)
	queue := &nameRebuildQueue{}
	models := &nameRebuildModels{err: errors.New("the embedding model is unavailable")}
	rebuilder := NewEntityNameEmbeddingRebuilder(db, models, queue)
	require.NoError(t, rebuilder.Startup(ctx))
	require.NoError(t, rebuilder.MarkAndEnqueue(ctx, 1, "library_embedding_model_changed"))
	require.Zero(t, queue.count())
	status, err := rebuilder.Status(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, status.PendingCount)

	require.NoError(t, db.Create(&types.Library{ID: "extracted", SpaceID: 1, Name: "Extracted", Directory: "extracted"}).Error)
	extracted := entityRecord("pending-extracted", "Extracted name", "company")
	extracted.LibraryID = "extracted"
	extracted.EmbeddingNeedsRebuild = true
	require.NoError(t, db.Create(&extracted).Error)

	run := types.EntityNameEmbeddingRebuildRun{ID: "legacy-managed", SpaceID: 1, Reason: "managed_entity_created", Status: "pending", TotalCount: 1}
	require.NoError(t, db.Create(&run).Error)
	payload, err := json.Marshal(types.EntityNameEmbeddingRebuildPayload{SpaceID: 1, RunID: run.ID})
	require.NoError(t, err)
	require.NoError(t, rebuilder.Handle(ctx, taskqueue.NewTask(types.TypeEntityNameEmbeddingRebuild, payload)))
	status, err = rebuilder.Status(ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, status.PendingCount, "an unrelated extracted name remains pending")
	require.Equal(t, "completed", status.LatestRun.Status)
	require.Zero(t, status.LatestRun.TotalCount)
	require.Zero(t, status.LatestRun.ModelCalls)
	require.Empty(t, readNameEntity(t, db, entity.ID).NameEmbedding)
}

func TestSnapshotModelAndLibraryEditsPreserveEntityAndQueueHistory(t *testing.T) {
	db, ctx := nameEmbeddingTriggerDB(t)
	queue := redistest.NewClient(t, db)
	models := &nameRebuildModels{models: map[string]embedding.Embedder{
		"team-vector":     &nameRebuildEmbedder{id: "team-vector", dimension: 4},
		"override-vector": &nameRebuildEmbedder{id: "override-vector", dimension: 3},
	}}
	rebuilder := NewEntityNameEmbeddingRebuilder(db, models, queue)
	originalAt := time.Date(2025, 1, 2, 3, 4, 5, 123456000, time.UTC)
	require.NoError(t, db.Create(&types.Entity{ID: "historical-entity", SpaceID: 1, LibraryID: "trigger-library", CanonicalName: "Historical Entity", NormalizedName: "historical entity", EntityType: "company",
		Description: "Original profile", NameEmbedding: types.EntityNameEmbedding{1, 0, 0}, EmbeddingModelID: "team-vector", EmbeddingDimension: 3, CreatedAt: originalAt, UpdatedAt: originalAt}).Error)
	require.NoError(t, db.Create(&types.EntityNameEmbeddingRebuildRun{ID: "historical-run", SpaceID: 1, Status: "completed", Reason: "original", ModelID: "team-vector", Dimension: 3,
		ProfileHash: "historical-profile", TotalCount: 1, ProcessedCount: 1, CreatedAt: originalAt, UpdatedAt: originalAt}).Error)
	// Freeze delivery before recording the snapshot that model edits must preserve.
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	_, err := queue.Enqueue(taskqueue.NewTask(types.TypeEntityNameEmbeddingRebuild, []byte(`{"tenant_id":1,"run_id":"historical-run"}`)), taskqueue.Retention(time.Hour))
	require.NoError(t, err)
	tables := []string{"entities", "task_pending_ops", "entity_embedding_rebuild_runs"}
	original := map[string]string{}
	for _, table := range tables {
		var value string
		require.NoError(t, db.Raw("SELECT jsonb_agg(to_jsonb(row) ORDER BY id)::text FROM "+table+" row").Scan(&value).Error)
		original[table] = value
	}
	service := NewModelServiceWithRoutingDB(repository.NewModelRepository(db), repository.NewLibraryRepository(db), nil, nil, nil, db).(*modelService)
	service.SetNameEmbeddingRebuilder(rebuilder)
	var model types.Model
	require.NoError(t, db.First(&model, "id='team-vector'").Error)
	model.Parameters.EmbeddingParameters.Dimension = 4
	require.NoError(t, service.UpdateModel(ctx, &model))
	model = types.Model{}
	require.NoError(t, db.First(&model, "id='team-vector'").Error)
	require.Equal(t, 4, model.Parameters.EmbeddingParameters.Dimension, "model configuration must remain editable during snapshot pause")
	libraries := &libraryService{db: db, repo: repository.NewLibraryRepository(db)}
	libraries.SetNameEmbeddingRebuilder(rebuilder)
	_, err = libraries.UpdateLibrary(ctx, "trigger-library", "Library", "", &types.LibraryConfig{ModelTiers: &types.ModelTierConfig{Embedding: "override-vector"}})
	require.NoError(t, err)
	for _, table := range tables {
		var value string
		require.NoError(t, db.Raw("SELECT jsonb_agg(to_jsonb(row) ORDER BY id)::text FROM "+table+" row").Scan(&value).Error)
		require.JSONEq(t, original[table], value, table+" must retain every historical column while configuring models")
	}
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	require.NoError(t, rebuilder.MarkAndEnqueue(ctx, 1, "library_embedding_model_changed"))
	entity := readNameEntity(t, db, "historical-entity")
	require.True(t, entity.EmbeddingNeedsRebuild)
	require.True(t, entity.UpdatedAt.After(originalAt))
	var jobs, runs int64
	require.NoError(t, db.Table("task_pending_ops").Count(&jobs).Error)
	require.NoError(t, db.Table("entity_embedding_rebuild_runs").Count(&runs).Error)
	require.EqualValues(t, 2, jobs)
	require.EqualValues(t, 2, runs)
}
