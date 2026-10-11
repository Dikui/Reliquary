package container

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/types/interfaces"
)

func newVaultDocumentService(db *gorm.DB, cfg *config.Config, models interfaces.ModelService, retrieval interfaces.RetrievalService) (*service.VaultDocumentService, error) {
	return service.NewVaultDocumentService(db, models, retrieval, cfg.Vault.Root, cfg.Vault.StableWait, cfg.Vault.ScanInterval)
}

func registerVaultIntegration(v *service.VaultService, documents *service.VaultDocumentService, entry interfaces.EntryService, entities *service.EntityService, h *handler.VaultHandler, cleaner interfaces.ResourceCleaner) error {
	setter, ok := entry.(interface{ SetVault(*service.VaultService) })
	if !ok {
		return fmt.Errorf("vault integration unavailable for %T", entry)
	}
	setter.SetVault(v)
	entities.SetHeaderRefresher(v)
	h.SetArticles(v)
	cleaner.RegisterWithName("Vault article headers", v.Close)
	cleaner.RegisterWithName("Vault directory watcher", documents.Close)
	if err := v.StartReplica(); err != nil {
		return err
	}
	return documents.Start(context.Background())
}
