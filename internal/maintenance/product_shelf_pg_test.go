package maintenance

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/testutil/pgtest"
)

// Medium spec, task 02 / ADR 0023: old replicas gain empty shelf defaults;
// configured shelves survive export/import and block a lossy downgrade.
func TestProductShelfMigrationAndReplicaPreserveConfiguration(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Down())
	require.NoError(t, migration.Up())
	require.NoError(t, migration.Migrate(41))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Shelf fixtures','personal');
 INSERT INTO libraries(id,space_id,name,directory,embedding_model_id,summary_model_id,entity_mode)
 VALUES('shelf',1,'Synthetic shelf','synthetic-shelf','','','managed')`).Error)
	ctx, root := context.Background(), t.TempDir()
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", before))
	manifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(42))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	upgraded := t.TempDir()
	require.NoError(t, WriteSnapshot(upgraded, "", after))
	current, err := BuildManifest(ctx, db, upgraded, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(manifest, current))
	oldRestore := pgtest.Open(t)
	_, err = Import(ctx, oldRestore, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, oldRestore, root))
	require.NoError(t, migration.Migrate(41), "ordinary libraries do not block downgrade")
	require.NoError(t, migration.Migrate(42))
	require.NoError(t, db.Exec(`UPDATE libraries SET shelf_enabled=true,shelf_config='{"module_types":[{"key":"module_a","name":" Module A "}],"regions":[{"key":"region_a","name":"Region A"}],"stages":[{"key":"stage_b","name":"Stage B"},{"key":"stage_a","name":"Stage A"}]}' WHERE id='shelf'`).Error)
	saved, err := Capture(ctx, db)
	require.NoError(t, err)
	root = t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", saved))
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	var restored struct {
		Enabled bool
		Config  string
	}
	require.NoError(t, fresh.Raw("SELECT shelf_enabled AS enabled,shelf_config::text AS config FROM libraries WHERE id='shelf'").Scan(&restored).Error)
	require.True(t, restored.Enabled)
	require.JSONEq(t, `{"module_types":[{"key":"module_a","name":" Module A "}],"regions":[{"key":"region_a","name":"Region A"}],"stages":[{"key":"stage_b","name":"Stage B"},{"key":"stage_a","name":"Stage A"}]}`, restored.Config)
	require.ErrorContains(t, migration.Migrate(41), "product shelf data exists")
}
