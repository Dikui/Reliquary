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

func TestLibraryUpgradePreservationProjection(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	root := t.TempDir()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	defer migration.Close()
	require.NoError(t, migration.Migrate(9))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Owner','personal'); INSERT INTO libraries(id,space_id,name,description,embedding_model_id,summary_model_id) VALUES('old',1,'Library','','embed',''); INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind) VALUES('mine',1,'old','manual','Retained','','mine'),('report',1,'old','manual','Report','','report'); INSERT INTO task_pending_ops(id,task_type,queue,payload,state) VALUES('legacy-library-task','manual:process','default','{"knowledge_id":"mine","library_id":"old"}','pending')`).Error)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	restoreRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(restoreRoot, "", snapshot))
	before, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(10))
	current, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", current))
	after, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	// A changed new default or lost old value must remain visible to verification.
	require.NoError(t, db.Exec(`UPDATE libraries SET review_criteria='unauthorized' WHERE system_role='mine'`).Error)
	tampered, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(before, tampered), "table libraries")
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, restoreRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, restoreRoot))
}
