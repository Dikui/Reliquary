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

// Medium spec, task 03 / ADR 0023: drafts, saved decisions and provenance are
// portable data. Old replicas receive only empty migration defaults.
func TestShelfChangesetMigrationAndReplicaPreserveDrafts(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(42))
	seed(t, db)
	ctx := context.Background()
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	oldRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(oldRoot, "", before))
	oldManifest, err := BuildManifest(ctx, db, oldRoot, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(43))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	currentRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(currentRoot, "", after))
	currentManifest, err := BuildManifest(ctx, db, currentRoot, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(oldManifest, currentManifest))
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, oldRoot))
	require.NoError(t, WriteSnapshot(oldRoot, "", after), "upgrade must retain append-only historical logs")
	verified, err := VerifyReplica(ctx, db, oldRoot, "", false)
	require.NoError(t, err)
	require.True(t, verified.OK, verified.Differences)

	require.NoError(t, migration.Migrate(42))
	require.NoError(t, migration.Migrate(43))
	require.NoError(t, db.Exec(`INSERT INTO shelf_changesets(id,space_id,library_id,revision,status,source,items,created_at,updated_at)
 VALUES('draft',1,'library',2,'pending','{"type":"agent","agent_name":"Synthetic helper"}',
 '[{"id":"edit","operation":{"id":"edit","type":"add_alias","entity_id":"entity-a","revision":1,"alias":"Synthetic alias"},"before":null,"after":null,"conflicts":[],"decision":{"item_id":"edit","action":"defer"}}]',now(),now());
 UPDATE operation_logs SET source='{"type":"import","import_batch_id":"synthetic-batch","file_date":"2026-11-01T00:00:00Z"}',changeset_id='draft' WHERE id='op-1'`).Error)
	saved, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", saved))
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, root))
	actual, err := Capture(ctx, restored)
	require.NoError(t, err)
	require.Equal(t, saved.Tables["shelf_changesets"], actual.Tables["shelf_changesets"])
	require.Equal(t, saved.Tables["operation_logs"], actual.Tables["operation_logs"])
	require.ErrorContains(t, migration.Migrate(42), "shelf changesets or operation sources exist")
}
