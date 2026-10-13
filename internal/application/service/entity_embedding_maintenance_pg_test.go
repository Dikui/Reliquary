package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/embedding"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

func TestSnapshotModelAndLibraryEditsPreserveEntityAndQueueHistory(t *testing.T) {
	db, ctx := nameEmbeddingTriggerDB(t)
	queue := taskqueue.NewClient(db)
	models := &nameRebuildModels{models: map[string]embedding.Embedder{
		"team-vector":     &nameRebuildEmbedder{id: "team-vector", dimension: 4},
		"override-vector": &nameRebuildEmbedder{id: "override-vector", dimension: 3},
	}}
	rebuilder := NewEntityNameEmbeddingRebuilder(db, models, queue)
	originalAt := time.Date(2025, 1, 2, 3, 4, 5, 123456000, time.UTC)
	require.NoError(t, db.Create(&types.Entity{ID: "historical-entity", SpaceID: 1, CanonicalName: "Historical Entity", NormalizedName: "historical entity", EntityType: "company",
		Description: "Original profile", NameEmbedding: types.EntityNameEmbedding{1, 0, 0}, EmbeddingModelID: "team-vector", EmbeddingDimension: 3, CreatedAt: originalAt, UpdatedAt: originalAt}).Error)
	require.NoError(t, db.Create(&types.EntityNameEmbeddingRebuildRun{ID: "historical-run", SpaceID: 1, Status: "completed", Reason: "original", ModelID: "team-vector", Dimension: 3,
		ProfileHash: "historical-profile", TotalCount: 1, ProcessedCount: 1, CreatedAt: originalAt, UpdatedAt: originalAt}).Error)
	_, err := queue.Enqueue(taskqueue.NewTask(types.TypeEntityNameEmbeddingRebuild, []byte(`{"tenant_id":1,"run_id":"historical-run"}`)), taskqueue.Retention(time.Hour))
	require.NoError(t, err)
	tables := []string{"entities", "task_jobs", "entity_embedding_rebuild_runs"}
	original := map[string]string{}
	for _, table := range tables {
		var value string
		require.NoError(t, db.Raw("SELECT jsonb_agg(to_jsonb(row) ORDER BY id)::text FROM "+table+" row").Scan(&value).Error)
		original[table] = value
	}
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
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
	require.NoError(t, db.Table("task_jobs").Count(&jobs).Error)
	require.NoError(t, db.Table("entity_embedding_rebuild_runs").Count(&runs).Error)
	require.EqualValues(t, 2, jobs)
	require.EqualValues(t, 2, runs)
}
