package chatgpt

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestRestoredLoginAttemptHistorySurvivesLoginCleanupAndLogout(t *testing.T) {
	m, f, db := setupManager(t)
	account := login(t, m, f, "")
	old := f.now.Add(-time.Hour)
	for _, attempt := range []loginAttempt{
		{State: types.MaintenanceExcludedCredentialPrefix + "expired", ExpiresAt: old},
		{State: types.MaintenanceExcludedCredentialPrefix + "future", ExpiresAt: f.now.Add(time.Hour)},
		{State: "normal-expired-state", Nonce: "normal-nonce", Verifier: "normal-verifier", ExpiresAt: old},
		{State: "normal-future-state", Nonce: "normal-nonce", Verifier: "normal-verifier", ExpiresAt: f.now.Add(time.Hour)},
		{State: "real-" + types.MaintenanceExcludedCredentialPrefix + "state", ExpiresAt: old},
	} {
		attempt.AccountID, attempt.ClientID, attempt.CallbackURL = &account.ID, account.ClientID, "http://127.0.0.1:18080/auth/callback"
		require.NoError(t, db.Create(&attempt).Error)
	}
	archived := func() string {
		var rows string
		require.NoError(t, db.Raw(`SELECT jsonb_agg(to_jsonb(t) ORDER BY state)::text FROM chatgpt_login_attempts t WHERE state LIKE ?`, types.MaintenanceExcludedCredentialPrefix+"%").Scan(&rows).Error)
		return rows
	}
	before := archived()
	start, err := m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", account.ID)
	require.NoError(t, err)
	require.Equal(t, before, archived(), "login expiry cleanup must preserve the restored historical metadata")
	var count int64
	require.NoError(t, db.Model(&loginAttempt{}).Where("state IN ?", []string{"normal-expired-state", "real-" + types.MaintenanceExcludedCredentialPrefix + "state"}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&loginAttempt{}).Where("state=?", "normal-future-state").Count(&count).Error)
	require.EqualValues(t, 1, count)
	_, err = m.Logout(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, before, archived(), "logout cleanup must preserve the restored historical metadata")
	require.NoError(t, db.Model(&loginAttempt{}).Where("state IN ?", []string{"normal-future-state", start.State}).Count(&count).Error)
	require.Zero(t, count, "logout still cancels every ordinary pending login for the account")
}
