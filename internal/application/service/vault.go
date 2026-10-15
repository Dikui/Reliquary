package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gorm.io/gorm"
	"reliquary/internal/config"
	"reliquary/internal/maintenance"
)

// VaultService persists articles and refreshes their database-owned headers.
type VaultService struct {
	db            *gorm.DB
	root          string
	cfg           config.VaultConfig
	articles      vaultArticleState
	replica       *maintenance.Writer
	rebuildMu     sync.Mutex
	rebuild       VaultFileRebuildStatus
	rebuildCancel context.CancelFunc
	rebuildDone   chan struct{}
	rebuildClosed bool
}

func NewVaultService(db *gorm.DB, cfg *config.Config) (*VaultService, error) {
	if cfg == nil || cfg.Vault == nil || strings.TrimSpace(cfg.Vault.Root) == "" {
		return nil, fmt.Errorf("VAULT_ROOT is required")
	}
	root, err := filepath.Abs(cfg.Vault.Root)
	if err != nil {
		return nil, err
	}
	if err := ensureVaultDirectories(db, root); err != nil {
		return nil, err
	}
	return &VaultService{db: db, root: root, cfg: *cfg.Vault, replica: maintenance.NewWriter(db, root, os.Getenv("LOCAL_STORAGE_BASE_DIR"), cfg.Vault.HeaderDebounce)}, nil
}
