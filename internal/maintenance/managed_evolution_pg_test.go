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

func TestManagedEvolutionMigrationPreservesSnapshots(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	defer migration.Close()
	require.NoError(t, migration.Migrate(37))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Migration fixture','personal');
 INSERT INTO libraries(id,space_id,name,directory,embedding_model_id,summary_model_id,entity_mode,entity_schema) VALUES('products',1,'Products','products','','','managed','{"kinds":[{"key":"model","name":"Model"}],"fields":[{"key":"status","name":"Status","type":"text"}]}');
 INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,kind,attributes,revision,created_at,updated_at) VALUES('product',1,'products','Example','example','product','model','{"status":"available"}',3,'2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	previous, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Up())
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Len(t, after.Tables["entity_attribute_versions"], 1)
	require.Equal(t, after.Tables["entities"][0]["attributes"], after.Tables["entity_attribute_versions"][0]["attributes"])
	upgraded := t.TempDir()
	require.NoError(t, WriteSnapshot(upgraded, "", after))
	current, err := BuildManifest(ctx, db, upgraded, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(previous, current))
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, root))
	var count int64
	require.NoError(t, restored.Table("entity_attribute_versions").Count(&count).Error)
	require.EqualValues(t, 1, count)
}
