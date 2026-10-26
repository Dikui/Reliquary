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

func TestEntityCandidatesMigrationPreservesFactsAndReplica(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(39))
	seed(t, db)
	require.NoError(t, db.Exec(`INSERT INTO stories(id,space_id,title,first_fact_at,last_fact_at)
 VALUES('story',1,'Retained',now(),now());
 INSERT INTO facts(id,space_id,story_id,title,subject,action,created_at)
 VALUES('fact',1,'story','Retained','New Name','Announced',now());`).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	oldManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, m.Migrate(40))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", after))
	newManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(oldManifest, newManifest))
	require.NoError(t, m.Migrate(39))
	reverted, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, before.Tables, reverted.Tables)
	require.NoError(t, m.Migrate(40))
	again, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, after.Tables, again.Tables)
	// The old replica also imports into a current schema with only empty defaults.
	oldRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(oldRoot, "", before))
	target := pgtest.Open(t)
	_, err = Import(ctx, target, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, target, oldRoot))
	// A nondefault endpoint must not be accepted as a migration default.
	require.NoError(t, db.Exec("UPDATE facts SET subject_entity_id='entity-a' WHERE id='fact'").Error)
	changed, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(oldManifest, changed), "table facts")
	// Adding material sources preserves every existing business row.
	require.NoError(t, m.Migrate(45))
	beforeEntries, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(46))
	afterEntries, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Empty(t, afterEntries.Tables["entity_candidate_entries"])
	delete(afterEntries.Tables, "entity_candidate_entries")
	require.Equal(t, beforeEntries.Tables, afterEntries.Tables)
}
