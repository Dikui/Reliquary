package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type IngestDiagnosticsConfig struct {
	Root          string `yaml:"root" json:"root"`
	RetentionDays int    `yaml:"retention_days" json:"retention_days"`
	MaxBytes      int64  `yaml:"max_bytes" json:"max_bytes"`
}

func ResolveIngestDiagnostics(cfg *Config) (IngestDiagnosticsConfig, error) {
	c := IngestDiagnosticsConfig{RetentionDays: 7, MaxBytes: 512 * 1024 * 1024}
	if cfg != nil && cfg.IngestDiagnostics != nil {
		v := cfg.IngestDiagnostics
		c.Root = v.Root
		if v.RetentionDays != 0 {
			c.RetentionDays = v.RetentionDays
		}
		if v.MaxBytes != 0 {
			c.MaxBytes = v.MaxBytes
		}
	}
	if raw := os.Getenv("RELIQUARY_DIAGNOSTICS_ROOT"); raw != "" {
		c.Root = raw
	}
	if raw := os.Getenv("RELIQUARY_DIAGNOSTICS_RETENTION_DAYS"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil {
			return c, fmt.Errorf("invalid diagnostics retention")
		}
		c.RetentionDays = n
	}
	if raw := os.Getenv("RELIQUARY_DIAGNOSTICS_MAX_BYTES"); raw != "" {
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil {
			return c, fmt.Errorf("invalid diagnostics capacity")
		}
		c.MaxBytes = n
	}
	if c.Root == "" {
		cache, e := os.UserCacheDir()
		if e != nil {
			return c, e
		}
		c.Root = filepath.Join(cache, "reliquary", "diagnostics")
	}
	if !filepath.IsAbs(c.Root) || c.RetentionDays < 1 || c.RetentionDays > 365 || c.MaxBytes < 1024*1024 {
		return c, fmt.Errorf("diagnostics requires an absolute root, retention of 1..365 days and capacity of at least 1 MiB")
	}
	return c, nil
}
