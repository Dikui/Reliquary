package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
	"reliquary/internal/config"
)

// VaultService persists articles and refreshes their database-owned headers.
type VaultService struct {
	db       *gorm.DB
	root     string
	cfg      config.VaultConfig
	articles vaultArticleState
}

func NewVaultService(db *gorm.DB, cfg *config.Config) (*VaultService, error) {
	if cfg == nil || cfg.Vault == nil || strings.TrimSpace(cfg.Vault.Root) == "" {
		return nil, fmt.Errorf("VAULT_ROOT is required")
	}
	root, err := filepath.Abs(cfg.Vault.Root)
	if err != nil {
		return nil, err
	}
	for _, dir := range []string{"articles", "mine", "reports", "attachments"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			return nil, fmt.Errorf("create vault directory %s: %w", dir, err)
		}
	}
	return &VaultService{db: db, root: root, cfg: *cfg.Vault}, nil
}
