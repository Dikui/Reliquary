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

func TestStoryFollowsMigrationRetiresViewingTimesAndPreservesReplica(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(44))
	seed(t, db)
	require.NoError(t, db.Exec(`INSERT INTO stories(id,space_id,title,first_fact_at,last_fact_at)
 VALUES('followed-story',1,'Preserved story',now(),now());
 INSERT INTO follow_feed_library_states(space_id,library_id,last_viewed_at) VALUES(1,'library',now())`).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	oldRoot, root := t.TempDir(), t.TempDir()
	require.NoError(t, WriteSnapshot(oldRoot, "", before))
	require.NoError(t, WriteSnapshot(root, "", before))
	oldManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, m.Migrate(45))
	require.NoError(t, ValidateClassification(ctx, db))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NotContains(t, after.Tables, "follow_feed_states")
	require.NotContains(t, after.Tables, "follow_feed_library_states")
	require.Equal(t, before.Tables["follows"], after.Tables["follows"])
	require.NoError(t, WriteSnapshot(root, "", after))
	manifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(oldManifest, manifest))
	target := pgtest.Open(t)
	_, err = Import(ctx, target, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, target, oldRoot))
	require.NoError(
		t,
		db.Exec(`INSERT INTO follows(space_id,target_type,target_id) VALUES(1,'story','followed-story')`).Error,
	)
	current, err := Capture(ctx, db)
	require.NoError(t, err)
	currentRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(currentRoot, "", current))
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, currentRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, currentRoot))
	require.NoError(t, restored.Exec("DELETE FROM follows WHERE target_type='story'").Error)
	require.ErrorContains(t, VerifyImportedReplica(ctx, restored, currentRoot), "follows")
	require.Error(t, db.Exec(`INSERT INTO follows(space_id,target_type,target_id) VALUES(1,'story','missing')`).Error)
	require.NoError(t, m.Migrate(44))
	require.True(t, db.Migrator().HasTable("follow_feed_library_states"))
	var count int64
	require.NoError(t, db.Table("follow_feed_library_states").Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Table("follows").Where("target_type='story'").Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, m.Migrate(45))
	require.NoError(t, ValidateClassification(ctx, db))
}
