package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVaultConfigurationRequiresRootAndPositiveWaits(t *testing.T) {
	t.Setenv("VAULT_ROOT", "")
	require.ErrorContains(t, applyVaultEnvOverrides(&Config{}), "VAULT_ROOT")
	t.Setenv("VAULT_ROOT", t.TempDir())
	for _, name := range []string{"VAULT_HEADER_DEBOUNCE", "VAULT_STABLE_WAIT", "VAULT_SCAN_INTERVAL"} {
		t.Setenv(name, "2s")
	}
	cfg := &Config{}
	require.NoError(t, applyVaultEnvOverrides(cfg))
	require.Equal(t, 2*time.Second, cfg.Vault.HeaderDebounce)
	require.Equal(t, 2*time.Second, cfg.Vault.StableWait)
	require.Equal(t, 2*time.Second, cfg.Vault.ScanInterval)
	t.Setenv("VAULT_STABLE_WAIT", "0s")
	require.ErrorContains(t, applyVaultEnvOverrides(cfg), "VAULT_STABLE_WAIT")
}
