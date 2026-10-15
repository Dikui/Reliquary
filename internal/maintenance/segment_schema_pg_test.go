package maintenance

import (
	"context"
	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"path/filepath"
	"reliquary/internal/testutil/pgtest"
	"testing"
)

func TestSegmentCheckpointMigrationAndReplica(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	require.NoError(t, m.Migrate(14))
	seed(t, db)
	before, err := BuildManifest(ctx, db, "", "")
	require.NoError(t, err)
	require.NoError(t, m.Migrate(16))
	after, err := BuildManifest(ctx, db, "", "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	require.NoError(t, db.Exec(`INSERT INTO entry_segment_runs(id,space_id,entry_id,phase,plan_hash,body_hash,plan,status) VALUES('checkpoint',1,'article','processing','plan','body','[{"number":1,"start":0,"end":5,"hash":"one","status":"completed","result":{"topics":[]}}]','running')`).Error)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	var count int64
	require.NoError(t, fresh.Table("entry_segment_runs").Where("id='checkpoint'").Count(&count).Error)
	require.EqualValues(t, 1, count)
	current, err := BuildManifest(ctx, fresh, "", "")
	require.NoError(t, err)
	require.NoError(t, fresh.Exec(`UPDATE entry_segment_runs SET plan='[]'`).Error)
	changed, err := BuildManifest(ctx, fresh, "", "")
	require.NoError(t, err)
	require.NotEmpty(t, CompareManifest(current, changed))
}

func TestSegmentSnapshotProjectionOnlyAddsTheOldEmptyDefault(t *testing.T) {
	original := &Snapshot{schemaVersion: 14, Tables: map[string][]map[string]any{}}
	current := &Snapshot{schemaVersion: 16, Tables: map[string][]map[string]any{"entry_segment_runs": {{"id": "unexpected"}}}}
	projectSegmentSnapshot(original, current)
	require.Empty(t, original.Tables["entry_segment_runs"])
	require.NotEqual(t, original.Tables["entry_segment_runs"], current.Tables["entry_segment_runs"])
	modern := &Snapshot{schemaVersion: 16, Tables: map[string][]map[string]any{}}
	projectSegmentSnapshot(modern, current)
	require.NotContains(t, modern.Tables, "entry_segment_runs")
}
