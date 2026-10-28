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

func TestIKB09ReplicaUpgradeImportAndTamperDetection(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	root, restoreRoot := t.TempDir(), t.TempDir()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	defer m.Close()
	require.NoError(t, m.Migrate(14))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Owner','personal');
 INSERT INTO libraries(id,space_id,name,directory,template,embedding_model_id,summary_model_id) VALUES('one',1,'One','one','own','',''),('two',1,'Two','two','own','','');
 INSERT INTO topics(id,space_id,name,description) VALUES('topic',1,'Scope','Keep');
 INSERT INTO follow_feed_states(space_id,last_viewed_at) VALUES(1,'2026-10-01T01:02:03.456789Z');`).Error)
	old, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", old))
	require.NoError(t, WriteSnapshot(restoreRoot, "", old))
	before, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, m.Up())
	current, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", current))
	after, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	require.NoError(t, db.Exec(`UPDATE topics SET description='Tampered'`).Error)
	tampered, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(before, tampered), "table topics")
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, restoreRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, restoreRoot))
	require.NoError(t, ValidateClassification(ctx, restored))
	var n int64
	require.NoError(t, restored.Table("follow_feed_library_states").Count(&n).Error)
	require.EqualValues(t, 2, n)
	require.NoError(t, restored.Exec(`UPDATE follow_feed_library_states SET last_viewed_at=last_viewed_at+interval '1 second' WHERE library_id='one'`).Error)
	require.ErrorContains(t, VerifyImportedReplica(ctx, restored, restoreRoot), "follow_feed_library_states")
}
