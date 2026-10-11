package container

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestSnapshotPausePreservesBuiltinModelsAndSyncLogsAtStartup(t *testing.T) {
	db := pgtest.Open(t)
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "builtin_models.yaml"), []byte(`builtin_models:
  - id: waiting-builtin
    name: changed-from-yaml
    type: KnowledgeQA
    parameters:
      provider: openai
`), 0o644))
	require.NoError(t, db.Create(&types.Model{ID: "waiting-builtin", SpaceID: types.SingleSpaceID, Name: "original model", Type: types.ModelTypeKnowledgeQA,
		Source: types.ModelSourceRemote, Status: types.ModelStatusNeedsConfiguration, IsBuiltin: true, ManagedBy: types.BuiltinModelManagedBy,
		Parameters: types.ModelParameters{Provider: "openai", ExtraConfig: map[string]string{"maintenance_required_fields": `["api_key"]`}},
	}).Error)
	require.NoError(t, db.Create(&types.DataSource{ID: "original-source", SpaceID: types.SingleSpaceID, KnowledgeBaseID: "original-kb", Name: "Original source", Type: "rss", Status: types.DataSourceStatusPaused}).Error)
	require.NoError(t, db.Exec("INSERT INTO sync_logs (id,data_source_id,space_id,status,started_at) VALUES ('waiting-sync','original-source',?,'running',?)", types.SingleSpaceID, time.Now().Add(-4*time.Hour)).Error)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	var originalModel, originalSync, retainedModel, retainedSync string
	require.NoError(t, db.Raw("SELECT to_jsonb(m)::text FROM models m WHERE id='waiting-builtin'").Scan(&originalModel).Error)
	require.NoError(t, db.Raw("SELECT to_jsonb(s)::text FROM sync_logs s WHERE id='waiting-sync'").Scan(&originalSync).Error)
	require.NoError(t, initializeRuntimeData(context.Background(), db, directory))
	resetPendingTasks(db)
	require.NoError(t, db.Raw("SELECT to_jsonb(m)::text FROM models m WHERE id='waiting-builtin'").Scan(&retainedModel).Error)
	require.NoError(t, db.Raw("SELECT to_jsonb(s)::text FROM sync_logs s WHERE id='waiting-sync'").Scan(&retainedSync).Error)
	require.JSONEq(t, originalModel, retainedModel)
	require.JSONEq(t, originalSync, retainedSync)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	require.NoError(t, initializeRuntimeData(context.Background(), db, directory))
	var name, status string
	require.NoError(t, db.Raw("SELECT name FROM models WHERE id='waiting-builtin'").Scan(&name).Error)
	require.NoError(t, db.Raw("SELECT status FROM sync_logs WHERE id='waiting-sync'").Scan(&status).Error)
	require.Equal(t, "changed-from-yaml", name)
	require.Equal(t, types.SyncLogStatusFailed, status)
}
