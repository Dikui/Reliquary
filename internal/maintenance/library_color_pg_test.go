package maintenance

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reliquary/internal/testutil/pgtest"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestLibraryColorUpgradeAndReplicaRestore(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(34))
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Color preservation','personal');
INSERT INTO libraries(id,space_id,name,directory,created_at,embedding_model_id,summary_model_id) VALUES
('later',1,'Later','later','2026-02-01','',''),
('tie-b',1,'Tie B','tie-b','2026-01-01','',''),
('tie-a',1,'Tie A','tie-a','2026-01-01','','');
`).Error)
	root, restoreRoot := t.TempDir(), t.TempDir()
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	require.NoError(t, WriteSnapshot(restoreRoot, "", snapshot))
	before, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, m.Migrate(35))
	current, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", current))
	after, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, restoreRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, restoreRoot))
	var colors []int
	require.NoError(t, restored.Raw(`SELECT color FROM libraries ORDER BY created_at,id`).Scan(&colors).Error)
	require.Equal(t, []int{1, 2, 3}, colors)

	// Current replicas must retain a user's selection instead of recomputing it.
	require.NoError(t, db.Exec(`UPDATE libraries SET color=8 WHERE id='tie-a'`).Error)
	current, err = Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", current))
	changed, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NotEmpty(t, CompareManifest(before, changed), "upgrade projection must detect an incorrect color")
	currentRestored := pgtest.Open(t)
	_, err = Import(ctx, currentRestored, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, currentRestored, root))
	var color int
	require.NoError(t, currentRestored.Raw(`SELECT color FROM libraries WHERE id='tie-a'`).Scan(&color).Error)
	require.Equal(t, 8, color)
	for _, row := range snapshot.Tables["libraries"] {
		require.NotContains(t, row, "color", "projection must not mutate original evidence")
	}
	for _, row := range before.Tables["libraries"].Data {
		require.NotContains(t, row, "color")
	}
	for _, row := range current.Tables["libraries"] {
		if row["id"] == "tie-a" {
			require.Equal(t, json.Number("8"), row["color"])
		}
	}
}
