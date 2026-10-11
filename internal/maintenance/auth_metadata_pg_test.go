package maintenance

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/chatgpt"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/utils"
)

func seedAuthenticationMetadata(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO auth_tokens(id,user_id,token,token_type,expires_at,is_revoked,created_at,updated_at) VALUES
 ('auth-live','owner','old-access.secret.signature','access_token','2030-10-11T12:00:00Z',false,'2026-10-01T12:00:00Z','2026-10-02T12:00:00Z'),
 ('auth-revoked','owner','old-refresh.secret.signature','refresh_token','2030-10-12T12:00:00Z',true,'2026-10-01T13:00:00Z','2026-10-02T13:00:00Z')`,
		`INSERT INTO resource_access_grants(id,token_hash,resource_id,access_scope,expires_at,revoked_at,created_at) VALUES
 ('grant-live',repeat('a',64),'resource','read','2030-10-11T12:00:00Z',NULL,'2026-10-01T12:00:00Z'),
 ('grant-revoked',repeat('b',64),'resource','read','2030-10-12T12:00:00Z','2026-10-02T12:00:00Z','2026-10-01T13:00:00Z')`,
		`INSERT INTO chatgpt_login_attempts(state,nonce,verifier,callback_url,client_id,account_id,expires_at) VALUES
 ('old-oauth-state-one','old-oauth-nonce-one','old-oauth-verifier-one','http://localhost:18080/callback','public-client-id','00000000-0000-0000-0000-000000000001','2030-10-11T12:00:00Z'),
 ('old-oauth-state-two','old-oauth-nonce-two','old-oauth-verifier-two','http://localhost:18080/callback','public-client-id','00000000-0000-0000-0000-000000000001','2030-10-11T12:00:00Z'),
 ('old-oauth-state-three','old-oauth-nonce-three','old-oauth-verifier-three','http://localhost:18081/callback','public-other-client',NULL,'2030-10-12T12:00:00Z')`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
}

func assertAuthenticationDisabled(t *testing.T, db *gorm.DB) {
	t.Helper()
	t.Setenv("SYSTEM_AES_KEY", "12345678901234567890123456789012")
	var matches int64
	for _, query := range []string{
		`SELECT count(*) FROM auth_tokens WHERE token IN ('old-access.secret.signature','old-refresh.secret.signature')`,
		`SELECT count(*) FROM resource_access_grants WHERE token_hash IN (repeat('a',64),repeat('b',64))`,
		`SELECT count(*) FROM chatgpt_login_attempts WHERE state IN ('old-oauth-state-one','old-oauth-state-two','old-oauth-state-three')`,
	} {
		require.NoError(t, db.Raw(query).Scan(&matches).Error)
		require.Zero(t, matches, query)
	}
	var tokens []string
	require.NoError(t, db.Table("auth_tokens").Pluck("token", &tokens).Error)
	for _, token := range tokens {
		require.True(t, strings.HasPrefix(token, excludedCredentialPrefix))
		require.NotEqual(t, 2, strings.Count(token, "."), "a sentinel cannot have JWT compact serialization")
	}
	var hashes []string
	require.NoError(t, db.Table("resource_access_grants").Pluck("token_hash", &hashes).Error)
	for _, hash := range hashes {
		require.True(t, strings.HasPrefix(hash, excludedCredentialPrefix))
		_, err := hex.DecodeString(hash)
		require.Error(t, err, "a sentinel cannot match a SHA256 capability hash")
	}
	var attempts []struct{ State, Nonce, Verifier string }
	require.NoError(t, db.Table("chatgpt_login_attempts").Select("state,nonce,verifier").Find(&attempts).Error)
	require.Len(t, attempts, 3)
	states := map[string]bool{}
	for _, attempt := range attempts {
		require.True(t, strings.HasPrefix(attempt.State, excludedCredentialPrefix))
		require.False(t, states[attempt.State], "equal public metadata still needs distinct inert identities")
		states[attempt.State] = true
		require.Empty(t, attempt.Nonce)
		require.Empty(t, attempt.Verifier)
		_, err := chatgpt.NewManager(db).LoginAuthorizationURL(context.Background(), attempt.State)
		require.ErrorContains(t, err, "unencrypted credential storage rejected", "the real OAuth flow cannot obtain a PKCE verifier from the inert attempt")
	}
}

func TestAuthenticationMetadataRoundtripKeepsHistoryWithoutRestoringCredentials(t *testing.T) {
	ctx := context.Background()
	source := pgtest.Open(t)
	seed(t, source)
	seedAuthenticationMetadata(t, source)
	before, err := Capture(ctx, source)
	require.NoError(t, err)
	for _, table := range []string{"auth_tokens", "resource_access_grants", "chatgpt_login_attempts"} {
		require.NotEmpty(t, before.Tables[table])
		for _, row := range before.Tables[table] {
			keys := []string{}
			for column := range row {
				keys = append(keys, column)
			}
			require.ElementsMatch(t, strings.Fields(Rules[table].Sidecar), keys, table)
		}
	}
	encoded, err := json.Marshal(before)
	require.NoError(t, err)
	for _, credential := range []string{"old-access.secret.signature", "old-refresh.secret.signature", strings.Repeat("a", 64), strings.Repeat("b", 64), "old-oauth-state", "old-oauth-nonce", "old-oauth-verifier"} {
		require.NotContains(t, string(encoded), credential)
	}
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	expected, err := BuildManifest(ctx, source, root, "")
	require.NoError(t, err)
	target := pgtest.Open(t)
	_, err = Import(ctx, target, root, "", nil)
	require.NoError(t, err)
	assertAuthenticationDisabled(t, target)
	for _, database := range []*gorm.DB{target, source} {
		if database == source {
			_, err := Scrub(ctx, database)
			require.NoError(t, err)
			assertAuthenticationDisabled(t, database)
		}
		after, err := Capture(ctx, database)
		require.NoError(t, err)
		for _, table := range []string{"auth_tokens", "resource_access_grants", "chatgpt_login_attempts"} {
			require.Equal(t, before.Tables[table], after.Tables[table], table)
		}
		actual, err := BuildManifest(ctx, database, root, "")
		require.NoError(t, err)
		for _, table := range []string{"auth_tokens", "resource_access_grants", "chatgpt_login_attempts"} {
			require.False(t, expected.Tables[table].Regenerated, table)
			require.Equal(t, expected.Tables[table].SHA256, actual.Tables[table].SHA256, table)
			require.Equal(t, expected.Tables[table].Rows, actual.Tables[table].Rows, table)
		}
		require.Empty(t, CompareManifest(expected, actual))
	}
	// A second scrub keeps every public row and the same synthetic identities.
	var firstStates, secondStates []string
	require.NoError(t, source.Table("chatgpt_login_attempts").Order("state").Pluck("state", &firstStates).Error)
	_, err = Scrub(ctx, source)
	require.NoError(t, err)
	require.NoError(t, source.Table("chatgpt_login_attempts").Order("state").Pluck("state", &secondStates).Error)
	require.Equal(t, firstStates, secondStates)
}

func TestReconfigurationAppendPreservesOldAuthenticationMetadataAndDuplicateAttempts(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	seedAuthenticationMetadata(t, db)
	root := t.TempDir()
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	expected, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	data, err := json.Marshal(expected)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, expected))
	for _, statement := range []string{
		`INSERT INTO auth_tokens(id,user_id,token,token_type,expires_at) VALUES('new-auth','owner','new-access.secret.signature','access_token','2030-10-11T12:00:00Z')`,
		`INSERT INTO resource_access_grants(id,token_hash,resource_id,access_scope,expires_at) VALUES('new-grant',repeat('c',64),'resource','read','2030-10-11T12:00:00Z')`,
		`INSERT INTO chatgpt_login_attempts(state,nonce,verifier,callback_url,client_id,account_id,expires_at) VALUES('new-oauth-state','new-oauth-nonce','new-oauth-verifier','http://localhost:18080/callback','public-client-id','00000000-0000-0000-0000-000000000001','2030-10-11T12:00:00Z')`,
		`INSERT INTO operation_logs(id,space_id,actor,operation_type,object_id,before_value,after_value) VALUES('new-login-log',1,'owner','login','owner','null'::jsonb,'{}')`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	refresh := func() *Manifest {
		t.Helper()
		snapshot, err := Capture(ctx, db)
		require.NoError(t, err)
		require.NoError(t, WriteSnapshot(root, "", snapshot))
		actual, err := BuildManifest(ctx, db, root, "")
		require.NoError(t, err)
		return actual
	}
	actual := refresh()
	for _, table := range []string{"auth_tokens", "resource_access_grants", "chatgpt_login_attempts", "operation_logs"} {
		require.Contains(t, CompareManifest(expected, actual), "table "+table)
	}
	require.Contains(t, CompareManifestWithOptions(expected, actual, false, true), "table auth_tokens", "the old log allowance remains limited to logs")
	options := CompareOptions{AllowReconfigurationAppend: true}
	require.Empty(t, CompareManifestWithSettings(expected, actual, options))
	for _, mutation := range []struct{ Table, Change, Undo string }{
		{"auth_tokens", `UPDATE auth_tokens SET is_revoked=true WHERE id='auth-live'`, `UPDATE auth_tokens SET is_revoked=false WHERE id='auth-live'`},
		{"resource_access_grants", `UPDATE resource_access_grants SET expires_at='2031-10-11T12:00:00Z' WHERE id='grant-live'`, `UPDATE resource_access_grants SET expires_at='2030-10-11T12:00:00Z' WHERE id='grant-live'`},
		{"chatgpt_login_attempts", `UPDATE chatgpt_login_attempts SET client_id='changed-public-client' WHERE state='old-oauth-state-three'`, `UPDATE chatgpt_login_attempts SET client_id='public-other-client' WHERE state='old-oauth-state-three'`},
	} {
		require.NoError(t, db.Exec(mutation.Change).Error)
		require.Contains(t, CompareManifestWithSettings(expected, refresh(), options), "table "+mutation.Table)
		require.NoError(t, db.Exec(mutation.Undo).Error)
	}
	// Three equal public attempt rows contain two old occurrences plus one new.
	// Removing only one old occurrence may still leave two, but removing two
	// must fail: a set comparison would incorrectly accept the single survivor.
	require.NoError(t, db.Exec(`DELETE FROM chatgpt_login_attempts WHERE state IN ('old-oauth-state-one','old-oauth-state-two')`).Error)
	require.Contains(t, CompareManifestWithSettings(expected, refresh(), options), "table chatgpt_login_attempts")
	for table, secretColumns := range SecretColumns {
		if !inactiveCredentialTable(table) {
			continue
		}
		for _, column := range strings.Fields(secretColumns) {
			for _, row := range actual.Tables[table].Data {
				_, exists := row[column]
				require.False(t, exists, fmt.Sprintf("%s.%s", table, column))
			}
		}
	}
}

func TestSubscriptionCredentialRequirementsNameEveryEncryptedLeaf(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	key := []byte("12345678901234567890123456789012")
	t.Setenv("SYSTEM_AES_KEY", string(key))
	credential := func(value string) string {
		t.Helper()
		ciphertext, err := utils.EncryptAESGCM(value, key)
		require.NoError(t, err)
		return ciphertext
	}
	config := map[string]any{"url": "https://source.example.test/feed", "credentials": map[string]any{
		"token": credential("subscription-private-token-123456"), "app_secret": credential("subscription-private-app-secret-123456"),
		"auth_headers": credential(`{"Authorization":"Bearer subscription-private-header-123456","X-Custom-Key":"subscription-private-second-header-123456"}`),
		"unused":       "",
	}}
	encoded, err := json.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`UPDATE data_sources SET config=?::jsonb WHERE id='rss'`, string(encoded)).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	expected := []string{"config.credentials.token", "config.credentials.app_secret", "config.credentials.auth_headers.Authorization", "config.credentials.auth_headers.X-Custom-Key"}
	for _, requirement := range before.Required {
		if requirement.Kind == "data_sources" && requirement.ID == "rss" {
			require.ElementsMatch(t, expected, requirement.Fields)
		}
	}
	_, err = Scrub(ctx, db)
	require.NoError(t, err)
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	for _, requirement := range after.Required {
		if requirement.Kind == "data_sources" && requirement.ID == "rss" {
			require.ElementsMatch(t, expected, requirement.Fields)
		}
	}
	serialized, err := json.Marshal(after)
	require.NoError(t, err)
	for _, secret := range []string{"subscription-private-token-123456", "subscription-private-app-secret-123456", "subscription-private-header-123456", "subscription-private-second-header-123456"} {
		require.NotContains(t, string(serialized), secret)
	}
}

func TestRestoredCredentialSentinelsAllowAnotherBackupWithoutWeakeningSecretScan(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	seedAuthenticationMetadata(t, db)
	for _, secret := range []string{"old-access.secret.signature", strings.Repeat("a", 64), "old-oauth-state-one", "API_HASH_SECRET"} {
		require.ErrorContains(t, ScanCredentialReader(ctx, db, strings.NewReader(secret)), "operational credential detected")
	}
	for round := 0; round < 2; round++ {
		_, err := Scrub(ctx, db)
		require.NoError(t, err)
		// Decoded pg_restore COPY output contains the database's non-null
		// placeholders. Inspect the same raw fields without emitting a dump.
		for _, table := range []string{"auth_tokens", "resource_access_grants", "chatgpt_login_attempts", "api_keys"} {
			var rows []string
			require.NoError(t, db.Raw("SELECT to_jsonb(t)::text FROM "+quote(table)+" t").Scan(&rows).Error)
			require.NoError(t, ScanCredentialReader(ctx, db, strings.NewReader(strings.Join(rows, "\n"))), table)
		}
	}
	var firstState, differentState string
	require.NoError(t, db.Raw(`SELECT state FROM chatgpt_login_attempts WHERE client_id='public-client-id' ORDER BY state LIMIT 1`).Scan(&firstState).Error)
	require.NoError(t, db.Raw(`SELECT state FROM chatgpt_login_attempts WHERE client_id='public-other-client'`).Scan(&differentState).Error)
	swap := func(left, right string) {
		t.Helper()
		require.NoError(t, db.Exec(`UPDATE chatgpt_login_attempts SET state='temporary-private-state-for-swap' WHERE state=?`, left).Error)
		require.NoError(t, db.Exec(`UPDATE chatgpt_login_attempts SET state=? WHERE state=?`, left, right).Error)
		require.NoError(t, db.Exec(`UPDATE chatgpt_login_attempts SET state=? WHERE state='temporary-private-state-for-swap'`, right).Error)
	}
	swap(firstState, differentState)
	for _, mismatchedState := range []string{firstState, differentState} {
		require.ErrorContains(t, ScanCredentialReader(ctx, db, strings.NewReader(mismatchedState)), "operational credential detected", "a generated state from a different public identity is not inert for this row")
	}
	swap(firstState, differentState)
	// Similar prefixes cannot exempt an ordinary credential belonging to a
	// different identity, and an OAuth attempt with a verifier is not inert.
	lookalike := excludedCredentialPrefix + "another-auth-record"
	require.NoError(t, db.Exec(`UPDATE auth_tokens SET token=? WHERE id='auth-live'`, lookalike).Error)
	require.ErrorContains(t, ScanCredentialReader(ctx, db, strings.NewReader(lookalike)), "operational credential detected")
	require.NoError(t, db.Exec(`UPDATE auth_tokens SET token=? WHERE id='auth-live'`, excludedCredentialPrefix+"auth-live").Error)
	var state string
	require.NoError(t, db.Raw(`SELECT state FROM chatgpt_login_attempts ORDER BY state LIMIT 1`).Scan(&state).Error)
	require.NoError(t, db.Exec(`UPDATE chatgpt_login_attempts SET verifier='still-private-verifier' WHERE state=?`, state).Error)
	require.ErrorContains(t, ScanCredentialReader(ctx, db, strings.NewReader(state)), "operational credential detected")
	require.NoError(t, db.Exec(`UPDATE chatgpt_login_attempts SET verifier='' WHERE state=?`, state).Error)
	malformed := state[:strings.LastIndex(state, "-")+1] + "999999"
	require.NoError(t, db.Exec(`UPDATE chatgpt_login_attempts SET state=? WHERE state=?`, malformed, state).Error)
	require.ErrorContains(t, ScanCredentialReader(ctx, db, strings.NewReader(malformed)), "operational credential detected")
	require.NoError(t, db.Exec(`UPDATE api_keys SET api_key='still-live-space-api-key' WHERE id=1`).Error)
	require.ErrorContains(t, ScanCredentialReader(ctx, db, strings.NewReader("disabled-1")), "operational credential detected")
}
