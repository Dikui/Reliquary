package maintenance

import (
	"context"
	"path/filepath"
	"reliquary/internal/testutil/pgtest"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestManualGroupingMigrationPreservesReplica(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(41))
	seed(t, db)
	require.NoError(t, db.Exec(`UPDATE entries SET source_id='precision-test',source_revision=9007199254740993,
 custom_metadata='{"large_integer":9007199254740993}'::jsonb WHERE id='article'`).Error)
	require.NoError(t, db.Exec(`INSERT INTO stories(id,space_id,title,first_fact_at,last_fact_at)
 VALUES('story',1,'Preserved',now(),now());
 INSERT INTO facts(id,space_id,story_id,title,subject,action,created_at)
 VALUES('fact',1,'story','Preserved','Institute','Announced',now());`).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	oldManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, m.Migrate(42))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", after))
	newManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(oldManifest, newManifest))
	require.NoError(t, m.Migrate(41))
	reverted, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, before.Tables, reverted.Tables)
	require.NoError(t, m.Migrate(42))
	again, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, after.Tables, again.Tables)
	oldRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(oldRoot, "", before))
	target := pgtest.Open(t)
	_, err = Import(ctx, target, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, target, oldRoot))
	require.NoError(t, target.Exec("UPDATE facts SET manual_corrected=true WHERE id='fact'").Error)
	require.Error(
		t,
		VerifyImportedReplica(ctx, target, oldRoot),
		"nondefault correction must not pass as a migration default",
	)
}
