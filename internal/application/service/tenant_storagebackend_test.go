package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestCreateTenantCreatesConcreteDefaultStorageBackend(t *testing.T) {
	t.Setenv("STORAGE_TYPE", "local")
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.StorageBackend{}))
	tenantRepo := repository.NewTenantRepository(db)
	storageRepo := repository.NewStorageBackendRepository(db)
	tenantSvc := service.NewTenantService(tenantRepo, storageRepo)

	tenant, err := tenantSvc.CreateTenant(context.Background(), &types.Tenant{Name: "workspace"})
	require.NoError(t, err)
	require.NotNil(t, tenant.DefaultStorageBackendID)

	backend, err := storageRepo.GetByID(context.Background(), tenant.ID, *tenant.DefaultStorageBackendID)
	require.NoError(t, err)
	require.NotNil(t, backend)
	assert.Equal(t, "local", backend.Provider)
	assert.Equal(t, types.StorageBackendSourceEnv, backend.Source)
	assert.True(t, backend.LegacyAlias)
}
