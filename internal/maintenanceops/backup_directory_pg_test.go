package maintenanceops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
)

func TestBackupDirectoryDefaultUsesDeploymentHostWithoutChangingStoredSettings(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	t.Setenv("BACKUP_DIR", filepath.Join(t.TempDir(), "host-backups"))
	state, err := Status(ctx, db)
	require.NoError(t, err)
	require.Equal(t, os.Getenv("BACKUP_DIR"), state.BackupDirectory)
	due, err := Due(ctx, db, time.Now(), false)
	require.NoError(t, err)
	require.Equal(t, state.BackupDirectory, due.Directory)
	var stored string
	require.NoError(t, db.Raw("SELECT backup_directory FROM maintenance_state WHERE id=true").Scan(&stored).Error)
	require.Equal(t, "../reliquary-backups", stored)

	require.NoError(t, db.Exec("UPDATE maintenance_state SET backup_directory='/external/user-selected' WHERE id=true").Error)
	state, err = Status(ctx, db)
	require.NoError(t, err)
	require.Equal(t, "/external/user-selected", state.BackupDirectory)
	due, err = Due(ctx, db, time.Now(), false)
	require.NoError(t, err)
	require.Equal(t, state.BackupDirectory, due.Directory)
}

func TestBackupDirectoryDefaultWithoutEnvironmentUsesHostHome(t *testing.T) {
	t.Setenv("BACKUP_DIR", "")
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "Backups", "Reliquary"), effectiveBackupDirectory("../reliquary-backups"))
}
