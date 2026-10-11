package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/models/limiter"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func TestSettingsWriteRefreshesProcessCacheImmediately(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
	t.Setenv("SSRF_WHITELIST", "")
	t.Setenv("SSRF_WHITELIST_EXTRA", "")
	t.Setenv("RELIQUARY_MODEL_MAX_CONCURRENCY", "32")
	t.Cleanup(func() { utils.SetSSRFWhitelistFromRaw(""); limiter.SetGlobalLimit(32) })
	svc := NewSystemSettingService(repository.NewSystemSettingRepository(db), &config.Config{})
	_, err := svc.Update(ctx, "ssrf.whitelist", []string{"203.0.113.29"})
	require.NoError(t, err)
	require.Equal(t, []string{"203.0.113.29"}, svc.GetStringList(ctx, "ssrf.whitelist", "", nil))
	require.True(t, utils.IsSSRFWhitelisted("203.0.113.29"))
	_, err = svc.Update(ctx, "model.max_concurrency", 7)
	require.NoError(t, err)
	require.Equal(t, int64(7), svc.GetInt(ctx, "model.max_concurrency", "", 32))
	require.NoError(t, svc.Reset(ctx, "ssrf.whitelist"))
	require.Empty(t, svc.GetStringList(ctx, "ssrf.whitelist", "", nil))
	require.False(t, utils.IsSSRFWhitelisted("203.0.113.29"))
	var count int64
	require.NoError(t, db.Model(&types.SystemSetting{}).Where("key = ?", "model.max_concurrency").Count(&count).Error)
	require.Equal(t, int64(1), count)
}
