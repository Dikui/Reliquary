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

// Medium spec, task 11 / ADR 0023: plans and import provenance survive replicas;
// schema upgrade preserves all prior rows and business data prevents downgrade.
func TestShelfImportMigrationAndReplicaPreservePlans(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Down())
	require.NoError(t, migration.Up())
	require.NoError(t, migration.Migrate(48))
	seed(t, db)
	ctx := context.Background()
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	oldRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(oldRoot, "", before))
	oldManifest, err := BuildManifest(ctx, db, oldRoot, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(49))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	for table, rows := range before.Tables {
		if table != "schema_migrations" {
			require.Equal(t, rows, after.Tables[table], table)
		}
	}
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", after))
	manifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(oldManifest, manifest))
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, oldRoot))
	require.NoError(t, migration.Migrate(48))
	require.NoError(t, migration.Migrate(49))
	require.NoError(t, db.Exec(`INSERT INTO shelf_import_plans(space_id,library_id,key,name,file_kind,revision,definition,created_at,updated_at)
 VALUES(1,'library','map','Synthetic map','product_map',2,'{"sheets":[{"worksheet":"Map","header_rows":2,"product_line_id":"entity-a","columns":[{"column":"A","field":"model"}]}],"model_separators":[";"],"fill_meanings":{"FFFFFF00":"uncertain"}}',now(),now());
 INSERT INTO shelf_changesets(id,space_id,library_id,revision,status,source,items,created_at,updated_at)
 VALUES('import-batch',1,'library',1,'pending','{"type":"import","import_plan_key":"map","plan_revision":2,"file_name":"synthetic.xlsx","file_sha256":"synthetic-digest","import_batch_id":"synthetic-batch","file_date":"2026-10-28T00:00:00Z"}',
 '[{"id":"row","operation":{"id":"row","type":"create_entity","entity":{"kind":"model"},"import_location":{"worksheet":"Map","cells":["A3"]},"issues":[{"type":"required_missing"}]},"before":null,"after":null,"conflicts":[{"type":"required_missing"}]}]',now(),now())`).Error)
	saved, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", saved))
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, root))
	actual, err := Capture(ctx, restored)
	require.NoError(t, err)
	require.Equal(t, saved.Tables["shelf_import_plans"], actual.Tables["shelf_import_plans"])
	require.Equal(t, saved.Tables["shelf_changesets"], actual.Tables["shelf_changesets"])
	require.ErrorContains(t, migration.Migrate(48), "shelf import plans or batches exist")
}
