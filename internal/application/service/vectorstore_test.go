package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type storeViewRepo struct {
	interfaces.VectorStoreRepository
	rows map[string]*types.VectorStore
}

func (r *storeViewRepo) GetByID(_ context.Context, tenantID uint64, id string) (*types.VectorStore, error) {
	store := r.rows[id]
	if store != nil && store.TenantID == tenantID {
		return store, nil
	}
	return nil, nil
}
func (r *storeViewRepo) List(_ context.Context, tenantID uint64) ([]*types.VectorStore, error) {
	var stores []*types.VectorStore
	for _, store := range r.rows {
		if store.TenantID == tenantID {
			stores = append(stores, store)
		}
	}
	return stores, nil
}
func newStoreViewTestService() (*vectorStoreService, *storeViewRepo) {
	repo := &storeViewRepo{rows: map[string]*types.VectorStore{}}
	return &vectorStoreService{repo: repo}, repo
}
func insertGuardStore(t *testing.T, repo *storeViewRepo, id string, tenantID uint64) {
	t.Helper()
	repo.rows[id] = &types.VectorStore{ID: id, Name: id, TenantID: tenantID, EngineType: types.PostgresRetrieverEngineType}
}

func TestResolveStoreView(t *testing.T) {
	ctx := context.Background()
	svc, db := newStoreViewTestService()
	insertGuardStore(t, db, "store-A", 1)
	svc.envStores = []types.VectorStore{
		{ID: "__env_postgres__", Name: "PostgreSQL", EngineType: types.PostgresRetrieverEngineType},
	}

	t.Run("empty store id returns DefaultStoreDisplay", func(t *testing.T) {
		v, err := svc.ResolveStoreView(ctx, 1, "")
		require.NoError(t, err)
		assert.Equal(t, "System default", v.Name)
		assert.Equal(t, types.StoreSourceEnv, v.Source)
	})
	t.Run("DB hit", func(t *testing.T) {
		v, err := svc.ResolveStoreView(ctx, 1, "store-A")
		require.NoError(t, err)
		assert.Equal(t, "store-A", v.Name)
		assert.Equal(t, types.StoreSourceUser, v.Source)
		assert.Equal(t, string(types.PostgresRetrieverEngineType), v.EngineType)
	})
	t.Run("env hit", func(t *testing.T) {
		v, err := svc.ResolveStoreView(ctx, 1, "__env_postgres__")
		require.NoError(t, err)
		assert.Equal(t, "PostgreSQL", v.Name)
		assert.Equal(t, types.StoreSourceEnv, v.Source)
	})
	t.Run("miss returns unavailable", func(t *testing.T) {
		v, err := svc.ResolveStoreView(ctx, 1, "store-zzz")
		require.NoError(t, err)
		assert.Equal(t, types.StoreSourceUnavailable, v.Source)
	})
	t.Run("cross-tenant store is treated as unavailable", func(t *testing.T) {
		insertGuardStore(t, db, "store-B", 2) // owned by tenant 2
		v, err := svc.ResolveStoreView(ctx, 1, "store-B")
		require.NoError(t, err)
		assert.Equal(t, types.StoreSourceUnavailable, v.Source)
	})
}

func TestBatchResolveStoreView(t *testing.T) {
	ctx := context.Background()
	svc, db := newStoreViewTestService()
	insertGuardStore(t, db, "store-A", 1)
	insertGuardStore(t, db, "store-B", 1)
	svc.envStores = []types.VectorStore{
		{ID: "__env_postgres__", Name: "PostgreSQL", EngineType: types.PostgresRetrieverEngineType},
	}

	got, err := svc.BatchResolveStoreView(ctx, 1, []string{
		"store-A", "store-zzz", "__env_postgres__", "",
	})
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, types.StoreSourceUser, got["store-A"].Source)
	assert.Equal(t, "store-A", got["store-A"].Name)
	assert.Equal(t, types.StoreSourceUnavailable, got["store-zzz"].Source)
	assert.Equal(t, types.StoreSourceEnv, got["__env_postgres__"].Source)
	assert.Equal(t, "PostgreSQL", got["__env_postgres__"].Name)
	assert.Equal(t, types.StoreSourceEnv, got[""].Source)
	assert.Equal(t, "System default", got[""].Name)
}

func TestBatchResolveStoreView_Empty(t *testing.T) {
	ctx := context.Background()
	svc, _ := newStoreViewTestService()
	got, err := svc.BatchResolveStoreView(ctx, 1, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}
