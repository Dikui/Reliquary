package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVaultConfigurationRequiresRootAndPositiveHeaderDebounce(t *testing.T) {
	t.Setenv("VAULT_ROOT", "")
	require.ErrorContains(t, applyVaultEnvOverrides(&Config{}), "VAULT_ROOT")
	t.Setenv("VAULT_ROOT", t.TempDir())
	t.Setenv("VAULT_HEADER_DEBOUNCE", "2s")
	cfg := &Config{}
	require.NoError(t, applyVaultEnvOverrides(cfg))
	require.Equal(t, 2*time.Second, cfg.Vault.HeaderDebounce)
	t.Setenv("VAULT_HEADER_DEBOUNCE", "0s")
	require.ErrorContains(t, applyVaultEnvOverrides(cfg), "VAULT_HEADER_DEBOUNCE")
}
