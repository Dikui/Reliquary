package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/config"
	"reliquary/internal/testutil/pgtest"
)

func TestVaultFileRebuildShutdownCancelsBlockedWorker(t *testing.T) {
	db := pgtest.Open(t)
	vault, err := NewVaultService(db, &config.Config{Vault: &config.VaultConfig{Root: t.TempDir()}})
	require.NoError(t, err)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, tx.Exec("LOCK TABLE entries IN ACCESS EXCLUSIVE MODE").Error)
	defer tx.Rollback()
	state, err := vault.StartFileRebuild(context.Background())
	require.NoError(t, err)
	require.Equal(t, "running", state.Status)
	stopped := make(chan struct{})
	go func() { vault.closeFileRebuild(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel blocked rebuild")
	}
	state = vault.FileRebuildStatus()
	require.Equal(t, "failed", state.Status)
	require.Contains(t, state.Error, "context canceled")
	_, err = vault.StartFileRebuild(context.Background())
	require.ErrorContains(t, err, "closed")
	require.NoError(t, tx.Rollback().Error)
	require.NoError(t, vault.Close())
}
