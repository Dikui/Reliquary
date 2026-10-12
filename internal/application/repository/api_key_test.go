package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestAPIKeyRepositoryPersistsUTCExpiry(t *testing.T) {
	t.Setenv("TZ", "Asia/Shanghai")

	db := pgtest.OpenFixture(t)
	require.NoError(t, db.AutoMigrate(&types.APIKey{}))

	repo := NewAPIKeyRepository(db)
	ctx := context.Background()

	expiresAt := time.Unix(time.Now().UTC().Add(5*time.Second).Unix(), 0).UTC()
	spaceID := uint64(42)
	key := &types.APIKey{
		SpaceID: &spaceID,
		Name:    "integration",
		KeyHash: "hash-expiry",
		APIKey:  "sk-test",

		ExpiresAt: &expiresAt,
	}
	require.NoError(t, repo.CreateAPIKey(ctx, key))

	loaded, err := repo.GetAPIKeyByHash(ctx, key.KeyHash)
	require.NoError(t, err)
	require.NotNil(t, loaded.ExpiresAt)
	require.Equal(t, time.UTC, loaded.ExpiresAt.Location())
	require.True(t, loaded.ExpiresAt.Equal(expiresAt))
}

// TestAPIKeyRepositoryUpdateIsSpaceScoped 验证通用更新不会越过租户边界。
// 输入同租户和其他租户的 Key；前者更新全部可配置字段，后者必须返回未找到。
func TestAPIKeyRepositoryUpdateIsSpaceScoped(t *testing.T) {
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.AutoMigrate(&types.APIKey{}))
	repo := NewAPIKeyRepository(db)
	ctx := context.Background()
	space42, space43 := uint64(42), uint64(43)
	keys := []*types.APIKey{
		{SpaceID: &space42, Name: "scoped", KeyHash: "hash-scoped", APIKey: "sk-scoped"},
		{SpaceID: &space43, Name: "other", KeyHash: "hash-other", APIKey: "sk-other"},
		{SpaceID: &space42, Name: "full", KeyHash: "hash-full", APIKey: "sk-full"},
	}
	for _, key := range keys {
		require.NoError(t, repo.CreateAPIKey(ctx, key))
	}

	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	updated, err := repo.UpdateAPIKey(ctx, space42, keys[0].ID, &types.APIKey{
		Name: "updated",

		ExpiresAt: &expiresAt,
	})
	require.NoError(t, err)
	require.Equal(t, "updated", updated.Name)
	require.NotNil(t, updated.ExpiresAt)
	require.True(t, updated.ExpiresAt.Equal(expiresAt))

	_, err = repo.UpdateAPIKey(ctx, space42, keys[1].ID, &types.APIKey{Name: "blocked"})
	require.ErrorIs(t, err, ErrAPIKeyNotFound)

	full, err := repo.UpdateAPIKey(ctx, space42, keys[2].ID, &types.APIKey{
		Name: "full updated",
	})
	require.NoError(t, err)
	require.Equal(t, "full updated", full.Name)
}
