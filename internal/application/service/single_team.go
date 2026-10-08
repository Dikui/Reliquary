package service

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"reliquary/internal/application/repository"
	"reliquary/internal/types"
)

// EnsureSingleTeamSpace runs before any request or background task starts.
func EnsureSingleTeamSpace(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(72651000)).Error; err != nil {
			return err
		}
		repo := repository.NewTenantRepository(tx)
		tenants, err := repo.ListTenants(ctx)
		if err != nil {
			return err
		}
		if len(tenants) == 1 && tenants[0].ID == types.SingleTenantID {
			return nil
		}
		if len(tenants) != 0 {
			return fmt.Errorf("expected only personal workspace %d, found %d workspaces", types.SingleTenantID, len(tenants))
		}
		svc := NewTenantService(repo, repository.NewStorageBackendRepository(tx))
		_, err = svc.CreateTenant(ctx, &types.Tenant{
			ID: types.SingleTenantID, Name: "Personal workspace", Description: "Personal knowledge", Status: "active",
		})
		return err
	})
}
