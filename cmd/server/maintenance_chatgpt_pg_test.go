package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"reliquary/internal/chatgpt"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

type offlineAuthorization struct{ nonce, challenge, callback, clientID string }

type offlineOpenAI struct {
	server       *httptest.Server
	key          *rsa.PrivateKey
	mu           sync.Mutex
	codes        map[string]offlineAuthorization
	invalidNonce bool
	tokenCalls   int
}

func newOfflineOpenAI(t *testing.T) *offlineOpenAI {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &offlineOpenAI{key: key, codes: map[string]offlineAuthorization{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/authorize":
			query := r.URL.Query()
			require.Equal(t, "S256", query.Get("code_challenge_method"))
			require.Equal(t, chatgpt.RequestedScopes, query.Get("scope"))
			require.Equal(t, chatgpt.Resource, query.Get("resource"))
			require.NotEmpty(t, query.Get("ext_agent_host_id"))
			code := uuid.NewString()
			f.codes[code] = offlineAuthorization{nonce: query.Get("nonce"), challenge: query.Get("code_challenge"), callback: query.Get("redirect_uri"), clientID: query.Get("client_id")}
			callback, err := url.Parse(query.Get("redirect_uri"))
			require.NoError(t, err)
			callback.RawQuery = url.Values{"state": {query.Get("state")}, "code": {code}, "client_id": {query.Get("client_id")}}.Encode()
			http.Redirect(w, r, callback.String(), http.StatusSeeOther)
		case "/token":
			f.tokenCalls++
			require.NoError(t, r.ParseForm())
			auth, exists := f.codes[r.Form.Get("code")]
			digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if !exists || r.Form.Get("grant_type") != "authorization_code" || auth.clientID != r.Form.Get("client_id") || auth.callback != r.Form.Get("redirect_uri") || auth.challenge != base64.RawURLEncoding.EncodeToString(digest[:]) || r.Form.Get("resource") != chatgpt.Resource {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			delete(f.codes, r.Form.Get("code"))
			nonce := auth.nonce
			if f.invalidNonce {
				nonce = "wrong-nonce"
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": f.server.URL, "sub": "offline-subject", "aud": auth.clientID, "email": "owner@example.test", "nonce": nonce, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()})
			token.Header["kid"] = "offline-test"
			idToken, err := token.SignedString(f.key)
			require.NoError(t, err)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "offline-access-secret", "refresh_token": "offline-refresh-secret", "id_token": idToken, "token_type": "Bearer", "expires_in": 3600, "scope": chatgpt.RequestedScopes})
		case "/discovery":
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": f.server.URL, "jwks_uri": f.server.URL + "/jwks"})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "offline-test", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes())}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *offlineOpenAI) authorize(t *testing.T, endpoint string) string {
	t.Helper()
	client := *f.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(endpoint)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	return response.Header.Get("Location")
}

func offlineChatGPTFixture(t *testing.T, legacy bool) (*gorm.DB, *chatgpt.Manager, *offlineOpenAI, string) {
	t.Helper()
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	db := pgtest.Open(t)
	if legacy {
		directory, err := filepath.Abs("../../migrations/versioned")
		require.NoError(t, err)
		migration, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
		require.NoError(t, err)
		require.NoError(t, db.Exec("DROP TABLE maintenance_state").Error)
		t.Cleanup(func() {
			sourceErr, databaseErr := migration.Close()
			require.NoError(t, sourceErr)
			require.NoError(t, databaseErr)
		})
	}
	f := newOfflineOpenAI(t)
	manager := chatgpt.NewManager(db, chatgpt.WithHTTPClient(f.server.Client()), chatgpt.WithEndpoints(chatgpt.Endpoints{Issuer: f.server.URL, Authorization: f.server.URL + "/authorize", Token: f.server.URL + "/token", Discovery: f.server.URL + "/discovery"}))
	id := uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,status,business) VALUES(1,'Offline owner','active','personal')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO chatgpt_accounts(id,client_id,issuer,subject,email,status,created_at,updated_at,welcome_acknowledged)
	 VALUES(?, 'offline-client', ?, 'offline-subject','owner@example.test','needs_login','2026-01-01T01:00:00Z','2026-10-01T01:00:00Z',true)`, id, f.server.URL).Error)
	for _, original := range []string{"active", "download_failed"} {
		model := &types.Model{ID: "offline-chatgpt-" + original, SpaceID: 1, Name: "offline-model", Type: types.ModelTypeGeneration, Source: types.ModelSourceChatGPT, Status: types.ModelStatusNeedsConfiguration,
			Parameters: types.ModelParameters{Provider: "chatgpt", InterfaceType: "responses", ExtraConfig: map[string]string{"chatgpt_account_id": id, "maintenance_required_fields": `["chatgpt_login"]`, "maintenance_original_status": original, "maintenance_original_updated_at": "2026-10-01T01:00:00+00:00"}}}
		require.NoError(t, db.Create(model).Error)
	}
	return db, manager, f, id
}

func invokeOfflineChatGPT(t *testing.T, db *gorm.DB, manager *chatgpt.Manager, args []string, payload string) (map[string]any, string, error) {
	t.Helper()
	var output bytes.Buffer
	err := runMaintenanceChatGPT(context.Background(), db, args, strings.NewReader(payload), &output, manager)
	for _, secret := range []string{"offline-access-secret", "offline-refresh-secret", "wrong-nonce"} {
		require.NotContains(t, output.String(), secret)
		if err != nil {
			require.NotContains(t, err.Error(), secret)
		}
	}
	var result map[string]any
	if output.Len() != 0 {
		require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	}
	return result, output.String(), err
}

func TestOfflineChatGPTSignInUsesRealOAuthAndPreservesArchivedHistoryWithoutMaintenanceState(t *testing.T) {
	db, manager, fake, id := offlineChatGPTFixture(t, true)
	ctx := context.Background()
	for _, suffix := range []string{"expired", "future"} {
		expires := time.Now().Add(-24 * time.Hour)
		if suffix == "future" {
			expires = time.Now().Add(24 * time.Hour)
		}
		require.NoError(t, db.Exec(`INSERT INTO chatgpt_login_attempts(state,nonce,verifier,callback_url,client_id,account_id,expires_at) VALUES(?, '', '', 'http://127.0.0.1:18080/auth/callback','offline-client',?,?)`, types.MaintenanceExcludedCredentialPrefix+suffix, id, expires).Error)
	}
	var archivedBefore, accountBefore string
	require.NoError(t, db.Raw("SELECT jsonb_agg(to_jsonb(a) ORDER BY state)::text FROM chatgpt_login_attempts a").Scan(&archivedBefore).Error)
	require.NoError(t, db.Raw("SELECT to_jsonb(a)::text FROM chatgpt_accounts a WHERE id=?", id).Scan(&accountBefore).Error)
	start, _, err := invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-login", "--id", id, "--callback", "http://127.0.0.1:18080/auth/callback"}, "")
	require.NoError(t, err)
	authURL, err := url.Parse(start["authorization_url"].(string))
	require.NoError(t, err)
	require.Empty(t, authURL.Query().Get("id_token_hint"))
	var storedVerifier string
	require.NoError(t, db.Table("chatgpt_login_attempts").Select("verifier").Where("state=?", authURL.Query().Get("state")).Scan(&storedVerifier).Error)
	require.True(t, strings.HasPrefix(storedVerifier, utils.EncPrefix))
	callback := fake.authorize(t, authURL.String())
	encoded, err := json.Marshal(map[string]string{"callback_url": callback})
	require.NoError(t, err)
	result, completeOutput, err := invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-complete"}, string(encoded))
	require.NoError(t, err)
	require.Equal(t, id, result["account_id"])
	require.Equal(t, "connected", result["status"])
	require.Len(t, result["restored_model_ids"], 2)
	require.NotContains(t, completeOutput, "code")
	require.NotContains(t, completeOutput, authURL.Query().Get("state"))
	var after struct {
		ClientID, Issuer, Subject, Credentials, Status string
		CreatedAt                                      time.Time
		WelcomeAcknowledged                            bool
	}
	require.NoError(t, db.Table("chatgpt_accounts").Where("id=?", id).Take(&after).Error)
	require.Equal(t, "offline-client", after.ClientID)
	require.Equal(t, fake.server.URL, after.Issuer)
	require.Equal(t, "offline-subject", after.Subject)
	require.Equal(t, time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), after.CreatedAt)
	require.True(t, after.WelcomeAcknowledged)
	require.True(t, strings.HasPrefix(after.Credentials, utils.EncPrefix))
	plain, err := utils.DecryptStoredSecret(after.Credentials)
	require.NoError(t, err)
	require.Contains(t, plain, "offline-access-secret")
	require.Contains(t, plain, "offline-refresh-secret")
	for _, original := range []string{"active", "download_failed"} {
		var model types.Model
		require.NoError(t, db.Where("id=?", "offline-chatgpt-"+original).Take(&model).Error)
		require.Equal(t, original, string(model.Status))
		require.NotContains(t, model.Parameters.ExtraConfig, "maintenance_required_fields")
		require.Equal(t, original, model.Parameters.ExtraConfig["maintenance_original_status"])
	}
	var archivedAfter string
	require.NoError(t, db.Raw("SELECT jsonb_agg(to_jsonb(a) ORDER BY state)::text FROM chatgpt_login_attempts a WHERE state LIKE ?", types.MaintenanceExcludedCredentialPrefix+"%").Scan(&archivedAfter).Error)
	require.JSONEq(t, archivedBefore, archivedAfter)
	_, _, err = invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-complete"}, string(encoded))
	require.ErrorContains(t, err, "does not match")
	_, output, err := invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-login", "--id", id}, "")
	require.ErrorContains(t, err, "needs_login")
	require.Empty(t, output)
	var version int
	require.NoError(t, db.WithContext(ctx).Table("schema_migrations").Select("version").Scan(&version).Error)
	require.Equal(t, 7, version)
	require.False(t, db.Migrator().HasTable("maintenance_state"))
}

func TestOfflineChatGPTCallbackRejectsTamperingAndValidatesProviderIdentity(t *testing.T) {
	db, manager, fake, id := offlineChatGPTFixture(t, false)
	start, _, err := invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-login", "--id", id, "--callback", "http://127.0.0.1:18080/auth/callback"}, "")
	require.NoError(t, err)
	callback := fake.authorize(t, start["authorization_url"].(string))
	parsed, err := url.Parse(callback)
	require.NoError(t, err)
	for _, altered := range []string{
		callback + "&code=private-code", callback + "&state=private-state", callback + "#private-fragment",
		strings.Replace(callback, "127.0.0.1:18080", "127.0.0.1:18081", 1),
		strings.Replace(callback, "127.0.0.1", "attacker.example", 1),
		strings.Replace(callback, "127.0.0.1", "private-user:private-password@127.0.0.1", 1),
	} {
		payload, err := json.Marshal(map[string]string{"callback_url": altered})
		require.NoError(t, err)
		_, output, err := invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-complete"}, string(payload))
		require.Error(t, err)
		require.Empty(t, output)
		require.NotContains(t, err.Error(), "private-")
	}
	var attempts int64
	require.NoError(t, db.Table("chatgpt_login_attempts").Where("state=?", parsed.Query().Get("state")).Count(&attempts).Error)
	require.EqualValues(t, 1, attempts, "rejected callback URLs must not consume the real attempt")
	_, _, err = invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-complete"}, `{"callback_url":"x","access_token":"private-token"}`)
	require.ErrorContains(t, err, "invalid ChatGPT callback JSON")
	fake.mu.Lock()
	fake.invalidNonce = true
	fake.mu.Unlock()
	encoded, err := json.Marshal(map[string]string{"callback_url": callback})
	require.NoError(t, err)
	_, _, err = invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-complete"}, string(encoded))
	require.ErrorContains(t, err, "failed validation")
	var credentials, status string
	require.NoError(t, db.Table("chatgpt_accounts").Select("credentials").Where("id=?", id).Scan(&credentials).Error)
	require.NoError(t, db.Table("chatgpt_accounts").Select("status").Where("id=?", id).Scan(&status).Error)
	require.Empty(t, credentials)
	require.Equal(t, "needs_login", status)
}

func TestOfflineChatGPTCallbackInputIsPrivateAndBounded(t *testing.T) {
	db, manager, _, _ := offlineChatGPTFixture(t, false)
	input := filepath.Join(t.TempDir(), "callback.json")
	require.NoError(t, os.WriteFile(input, []byte(`{"callback_url":"private-callback"}`), 0644))
	_, output, err := invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-complete", "--input", input}, "")
	require.ErrorContains(t, err, "owner-only")
	require.Empty(t, output)
	_, _, err = invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-complete"}, `{"callback_url":"private-callback"} {}`)
	require.ErrorContains(t, err, "exactly one")
	_, _, err = invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-complete"}, strings.Repeat(" ", 1<<20)+"{}")
	require.ErrorContains(t, err, "1 MiB")
	t.Setenv("SYSTEM_AES_KEY", "")
	_, _, err = invokeOfflineChatGPT(t, db, manager, []string{"chatgpt-login", "--id", "fixture"}, "")
	require.ErrorContains(t, err, "32-byte")
}

func TestOfflineChatGPTAuthorizationOutputNeverContainsIDTokenHints(t *testing.T) {
	endpoint, err := offlineChatGPTAuthorizationURL("https://auth.openai.com/authorize?client_id=fixture&state=fresh&nonce=fresh&id_token_hint=private-token")
	require.NoError(t, err)
	require.NotContains(t, endpoint, "private-token")
	parsed, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.Empty(t, parsed.Query().Get("id_token_hint"))
	require.Equal(t, "fresh", parsed.Query().Get("state"))
	require.Equal(t, "fresh", parsed.Query().Get("nonce"))
}
