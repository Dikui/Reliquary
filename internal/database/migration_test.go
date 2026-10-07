package database

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/golang-migrate/migrate/v4"
	migratedb "github.com/golang-migrate/migrate/v4/database/stub"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/require"
)

func migrationTestSource(t *testing.T) source.Driver {
	t.Helper()
	driver, err := iofs.New(fstest.MapFS{
		"000000_init.up.sql":   {Data: []byte("SELECT 1;")},
		"000000_init.down.sql": {Data: []byte("SELECT 1;")},
		"000005_next.up.sql":   {Data: []byte("SELECT 2;")},
		"000005_next.down.sql": {Data: []byte("SELECT 2;")},
	}, ".")
	require.NoError(t, err)
	return driver
}

func TestRecoverDirtyMigrationUsesAvailableHistory(t *testing.T) {
	for _, tc := range []struct {
		name          string
		dirtyVersion  int
		forcedVersion int
	}{
		{name: "initial baseline", dirtyVersion: 0, forcedVersion: -1},
		{name: "later migration with version gap", dirtyVersion: 5, forcedVersion: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrationSource := migrationTestSource(t)
			db := &migratedb.Stub{CurrentVersion: tc.dirtyVersion, IsDirty: true}
			m, err := migrate.NewWithInstance("iofs", migrationSource, "stub", db)
			require.NoError(t, err)
			t.Cleanup(func() { m.Close() })
			require.NoError(t, recoverFromDirtyState(context.Background(), m, migrationSource, uint(tc.dirtyVersion)))
			require.Equal(t, tc.forcedVersion, db.CurrentVersion)
			require.False(t, db.IsDirty)
			require.NoError(t, m.Steps(1), "recovery must retry the failed migration")
			require.Equal(t, tc.dirtyVersion, db.CurrentVersion)
		})
	}
}

func TestRecoverDirtyMigrationRejectsRemovedHistory(t *testing.T) {
	migrationSource := migrationTestSource(t)
	db := &migratedb.Stub{CurrentVersion: 98, IsDirty: true}
	m, err := migrate.NewWithInstance("iofs", migrationSource, "stub", db)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	err = recoverFromDirtyState(context.Background(), m, migrationSource, 98)
	require.ErrorContains(t, err, "must be recreated")
	require.Equal(t, 98, db.CurrentVersion, "old databases must keep their migration history")
	require.True(t, db.IsDirty)
	require.Empty(t, db.MigrationSequence)
}

func TestDirtyInitialMigrationInstructionsRetryBaseline(t *testing.T) {
	migrationSource := migrationTestSource(t)
	t.Cleanup(func() { migrationSource.Close() })
	err := dirtyMigrationError(migrationSource, 0)
	require.ErrorContains(t, err, "./scripts/migrate.sh force -1")
	require.ErrorContains(t, err, "force does not undo SQL changes")
	require.NotContains(t, err.Error(), "force 0")
}
