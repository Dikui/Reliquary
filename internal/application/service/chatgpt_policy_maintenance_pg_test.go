package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestSnapshotChatGPTPolicyGetIsReadOnlyAcrossDailyReset(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 12, 4, 0, 0, 0, time.UTC)
	old := now.Add(-26 * time.Hour)
	limit := 3
	original := &types.ChatGPTPolicy{ID: 1, DailyLimit: &limit, DailyUsed: 3, Day: old.Format(time.DateOnly), Paused: true, Reason: "daily_limit", RetryAt: &old, UpdatedAt: old}
	require.NoError(t, db.Create(original).Error)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	svc := NewChatGPTPolicyService(db)
	svc.now = func() time.Time { return now }
	var before string
	require.NoError(t, db.Raw(`SELECT to_jsonb(p)::text FROM chatgpt_policy p WHERE id=1`).Scan(&before).Error)
	for range 3 {
		policy, err := svc.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, original.Day, policy.Day)
		require.Equal(t, original.DailyUsed, policy.DailyUsed)
		require.True(t, policy.Paused)
	}
	var after string
	require.NoError(t, db.Raw(`SELECT to_jsonb(p)::text FROM chatgpt_policy p WHERE id=1`).Scan(&after).Error)
	require.Equal(t, before, after, "reading the settings page must not change any saved policy field")
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	policy, err := svc.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, now.Format(time.DateOnly), policy.Day)
	require.Zero(t, policy.DailyUsed)
	require.False(t, policy.Paused)
	require.Empty(t, policy.Reason)
	require.True(t, policy.UpdatedAt.After(old), "normal reads retain their established daily reset and timestamp behavior")
}

func TestSnapshotChatGPTPolicyGetDoesNotCreateMissingPolicy(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	svc := NewChatGPTPolicyService(db)
	policy, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, policy.ID)
	require.Zero(t, policy.DailyUsed)
	var count int64
	require.NoError(t, db.Model(&types.ChatGPTPolicy{}).Count(&count).Error)
	require.Zero(t, count, "temporary defaults must not add a row during reconfiguration")
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	_, err = svc.Get(context.Background())
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.ChatGPTPolicy{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "normal reads still create the default policy")
}
