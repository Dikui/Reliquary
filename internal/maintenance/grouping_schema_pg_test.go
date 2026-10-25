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

func TestGroupingMigrationPreservesReplica(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(40))
	seed(t, db)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	oldManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, m.Migrate(41))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", after))
	newManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(oldManifest, newManifest))
	require.NoError(t, m.Migrate(40))
	reverted, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, before.Tables, reverted.Tables)
	require.NoError(t, m.Migrate(41))
	again, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, after.Tables, again.Tables)
	oldRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(oldRoot, "", before))
	target := pgtest.Open(t)
	_, err = Import(ctx, target, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, target, oldRoot))
}
