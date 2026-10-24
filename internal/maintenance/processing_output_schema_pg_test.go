package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/testutil/pgtest"
)

func TestIKB11ProcessingOutputMigrationReplicaAndTamperDetection(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	require.NoError(t, m.Migrate(16))
	seed(t, db)
	before, err := BuildManifest(ctx, db, "", "")
	require.NoError(t, err)
	require.NoError(t, m.Migrate(17))
	after, err := BuildManifest(ctx, db, "", "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	require.NoError(t, db.Exec(`INSERT INTO entry_processing_outputs(entry_id,space_id,library_id,body_hash,content_type,relations,images) SELECT id,space_id,library_id,'hash','自定义类型','[{"subject_id":"subject","type":"合作","object_id":"object","evidence":[]}]','[{"url":"../attachments/chart.png","description":"收入增长42%","ordinal":1,"locations":[]}]' FROM entries WHERE id='article'`).Error)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	require.NoError(t, fresh.Exec(`UPDATE entry_processing_outputs SET images='[]'`).Error)
	require.Error(t, VerifyImportedReplica(ctx, fresh, root))
	require.NoError(t, db.Exec(`UPDATE entries SET content_type='自定义类型' WHERE id='article'`).Error)
	// Development downgrade refuses data the old global check cannot represent.
	raw, err := filepath.Abs("../../migrations/versioned/000017_processing_outputs.down.sql")
	require.NoError(t, err)
	data, err := os.ReadFile(raw)
	require.NoError(t, err)
	require.Error(t, db.Exec(string(data)).Error)
	var present bool
	require.NoError(t, db.Raw(`SELECT to_regclass('public.entry_processing_outputs') IS NOT NULL`).Scan(&present).Error)
	require.True(t, present)
}
