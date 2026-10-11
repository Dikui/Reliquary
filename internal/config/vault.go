package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// VaultConfig controls the server-local Markdown library.
type VaultConfig struct {
	Root           string        `yaml:"root" json:"root"`
	HeaderDebounce time.Duration `yaml:"header_debounce" json:"header_debounce"`
	StableWait     time.Duration `yaml:"stable_wait" json:"stable_wait"`
	ScanInterval   time.Duration `yaml:"scan_interval" json:"scan_interval"`
}

func applyVaultEnvOverrides(cfg *Config) error {
	if cfg.Vault == nil {
		cfg.Vault = &VaultConfig{}
	}
	v := cfg.Vault
	if root, ok := os.LookupEnv("VAULT_ROOT"); ok {
		v.Root = strings.TrimSpace(root)
	}
	if strings.TrimSpace(v.Root) == "" {
		return fmt.Errorf("VAULT_ROOT is required")
	}
	root, err := filepath.Abs(v.Root)
	if err != nil {
		return fmt.Errorf("VAULT_ROOT: %w", err)
	}
	v.Root = root
	for _, item := range []struct {
		name     string
		dst      *time.Duration
		fallback time.Duration
	}{
		{"VAULT_HEADER_DEBOUNCE", &v.HeaderDebounce, 5 * time.Second},
		{"VAULT_STABLE_WAIT", &v.StableWait, 10 * time.Minute},
		{"VAULT_SCAN_INTERVAL", &v.ScanInterval, time.Minute},
	} {
		if raw, ok := os.LookupEnv(item.name); ok {
			d, err := time.ParseDuration(raw)
			if err != nil || d <= 0 {
				return fmt.Errorf("%s must be a positive duration", item.name)
			}
			*item.dst = d
		}
		if *item.dst < 0 {
			return fmt.Errorf("%s must be a positive duration", item.name)
		}
		if *item.dst == 0 {
			*item.dst = item.fallback
		}
	}
	return nil
}
