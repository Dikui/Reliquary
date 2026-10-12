package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func archivedAuthRows(t *testing.T, db *gorm.DB, table string) string {
	t.Helper()
	var rows string
	column := "token"
	if table == "resource_access_grants" {
		column = "token_hash"
	}
	require.NoError(t, db.Raw(`SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb)::text FROM `+table+` t WHERE `+column+` LIKE ?`, types.MaintenanceExcludedCredentialPrefix+"%").Scan(&rows).Error)
	return rows
}

func TestAuthTokenMaintenanceArchivesSurviveExpiryAndBulkRevocation(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Archive space','personal')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO users(id,username,email,password_hash) VALUES('archive-user','Archive user','archive@example.invalid','password-hash')`).Error)
	old := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, token := range []types.AuthToken{
		{ID: "archived-expired", Token: types.MaintenanceExcludedCredentialPrefix + "expired", ExpiresAt: old, IsRevoked: false},
		{ID: "archived-future", Token: types.MaintenanceExcludedCredentialPrefix + "future", ExpiresAt: time.Now().Add(time.Hour), IsRevoked: true},
		{ID: "normal-expired", Token: "expired-JWT", ExpiresAt: old},
		{ID: "normal-future", Token: "future-JWT", ExpiresAt: time.Now().Add(time.Hour)},
		{ID: "normal-containing-marker", Token: "real-" + types.MaintenanceExcludedCredentialPrefix + "value", ExpiresAt: old},
	} {
		token.UserID, token.TokenType, token.CreatedAt, token.UpdatedAt = "archive-user", "access_token", old, old
		require.NoError(t, db.Create(&token).Error)
	}
	before := archivedAuthRows(t, db, "auth_tokens")
	repo := NewAuthTokenRepository(db)
	require.NoError(t, repo.RevokeTokensByUserID(context.Background(), "archive-user"))
	require.Equal(t, before, archivedAuthRows(t, db, "auth_tokens"), "all archived timestamps and revocation flags must remain unchanged")
	var live types.AuthToken
	require.NoError(t, db.First(&live, "id=?", "normal-future").Error)
	require.True(t, live.IsRevoked, "ordinary tokens still follow logout and password rotation")
	require.NoError(t, repo.DeleteExpiredTokens(context.Background()))
	require.Equal(t, before, archivedAuthRows(t, db, "auth_tokens"))
	var count int64
	require.NoError(t, db.Model(&types.AuthToken{}).Where("id IN ?", []string{"normal-expired", "normal-containing-marker"}).Count(&count).Error)
	require.Zero(t, count, "expiry still cleans ordinary records, including nonprefix lookalikes")
	require.NoError(t, db.Exec(`DELETE FROM users WHERE id='archive-user'`).Error)
	require.NoError(t, db.Model(&types.AuthToken{}).Count(&count).Error)
	require.Zero(t, count, "explicit account deletion retains its foreign-key cascade")
}

func TestResourceGrantMaintenanceArchivesSurviveOrdinaryExpiryCleanup(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Exec(`INSERT INTO resources(id,handle,space_id,provider,physical_path,location_hash,kind,mime_type,original_name) VALUES('archive-resource','archive-handle',1,'local','archive/file','archive-location','file','text/plain','archive.txt')`).Error)
	old := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, grant := range []types.ResourceAccessGrant{
		{ID: "archived-expired", TokenHash: types.MaintenanceExcludedCredentialPrefix + "expired", ExpiresAt: old, RevokedAt: &old},
		{ID: "archived-future", TokenHash: types.MaintenanceExcludedCredentialPrefix + "future", ExpiresAt: time.Now().Add(time.Hour)},
		{ID: "normal-expired", TokenHash: "ordinary-expired-hash", ExpiresAt: old},
		{ID: "normal-future", TokenHash: "ordinary-future-hash", ExpiresAt: time.Now().Add(time.Hour)},
		{ID: "normal-containing-marker", TokenHash: "real-" + types.MaintenanceExcludedCredentialPrefix + "value", ExpiresAt: old},
	} {
		grant.ResourceID, grant.AccessScope, grant.CreatedAt = "archive-resource", "read", old
		require.NoError(t, db.Create(&grant).Error)
	}
	before := archivedAuthRows(t, db, "resource_access_grants")
	repo := NewResourceRepository(db)
	require.NoError(t, repo.DeleteExpiredGrants(context.Background(), time.Now()))
	require.Equal(t, before, archivedAuthRows(t, db, "resource_access_grants"), "all archived timestamps and revocation history must remain unchanged")
	var grants []types.ResourceAccessGrant
	require.NoError(t, db.Where("token_hash NOT LIKE ?", types.MaintenanceExcludedCredentialPrefix+"%").Find(&grants).Error)
	require.Len(t, grants, 1)
	require.Equal(t, "normal-future", grants[0].ID)
	require.NoError(t, db.Exec(`DELETE FROM resources WHERE id='archive-resource'`).Error)
	var count int64
	require.NoError(t, db.Model(&types.ResourceAccessGrant{}).Count(&count).Error)
	require.Zero(t, count, "explicit resource deletion retains its foreign-key cascade")
}
