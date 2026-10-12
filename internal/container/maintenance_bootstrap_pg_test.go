package container

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestSnapshotPausePreservesBuiltinModelsAtStartup(t *testing.T) {
	db := pgtest.Open(t)
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "builtin_models.yaml"), []byte(`builtin_models:
  - id: waiting-builtin
    name: changed-from-yaml
    type: KnowledgeQA
    parameters:
      provider: openai
`), 0o644))
	require.NoError(t, db.Create(&types.Model{ID: "waiting-builtin", SpaceID: types.SingleSpaceID, Name: "original model", Type: types.ModelTypeGeneration,
		Source: types.ModelSourceRemote, Status: types.ModelStatusNeedsConfiguration, IsBuiltin: true, ManagedBy: types.BuiltinModelManagedBy,
		Parameters: types.ModelParameters{Provider: "openai", ExtraConfig: map[string]string{"maintenance_required_fields": `["api_key"]`}},
	}).Error)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	var originalModel, retainedModel string
	require.NoError(t, db.Raw("SELECT to_jsonb(m)::text FROM models m WHERE id='waiting-builtin'").Scan(&originalModel).Error)
	require.NoError(t, initializeRuntimeData(context.Background(), db, directory))
	require.NoError(t, db.Raw("SELECT to_jsonb(m)::text FROM models m WHERE id='waiting-builtin'").Scan(&retainedModel).Error)
	require.JSONEq(t, originalModel, retainedModel)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	require.NoError(t, initializeRuntimeData(context.Background(), db, directory))
	var name string
	require.NoError(t, db.Raw("SELECT name FROM models WHERE id='waiting-builtin'").Scan(&name).Error)
	require.Equal(t, "changed-from-yaml", name)
}
