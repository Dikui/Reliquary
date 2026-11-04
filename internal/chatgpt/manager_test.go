package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type authorization struct{ nonce, challenge, callback, clientID string }

type fakeOpenAI struct {
	server               *httptest.Server
	key                  *rsa.PrivateKey
	now                  time.Time
	mu                   sync.Mutex
	codes                map[string]authorization
	scope                string
	identityFailure      string
	refreshFailure       string
	refreshStatus        int
	revokeStatus         int
	tokenCalls           int
	refreshCalls         int
	revokeCalls          int
	lastRefresh          url.Values
	lastRevocation       url.Values
	earliest             json.RawMessage
	dynamicRegistrations int
	codeFailures         int
}

func newFakeOpenAI(t *testing.T) *fakeOpenAI {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &fakeOpenAI{key: key, now: time.Now().UTC().Truncate(time.Second), codes: map[string]authorization{}, scope: RequestedScopes, revokeStatus: http.StatusOK}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeOpenAI) endpoints() Endpoints {
	return Endpoints{Issuer: f.server.URL, Authorization: f.server.URL + "/authorize", Token: f.server.URL + "/token", Discovery: f.server.URL + "/.well-known/openid-configuration", Models: f.server.URL + "/v1/models"}
}

func (f *fakeOpenAI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": f.server.URL, "jwks_uri": f.server.URL + "/jwks", "revocation_endpoint": f.server.URL + "/revoke"})
	case "/jwks":
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "test-key", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes())}}})
	case "/authorize":
		query := r.URL.Query()
		if query.Get("code_challenge_method") != "S256" || query.Get("nonce") == "" || query.Get("state") == "" || query.Get("ext_agent_host_id") == "" || query.Get("scope") != RequestedScopes || query.Get("resource") != Resource {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		clientID := query.Get("client_id")
		if clientID == DynamicClientID {
			f.dynamicRegistrations++
			clientID = "oaiapp_test"
			if f.dynamicRegistrations > 1 {
				clientID = fmt.Sprintf("oaiapp_test_%d", f.dynamicRegistrations)
			}
		}
		code := uuid.NewString()
		f.codes[code] = authorization{nonce: query.Get("nonce"), challenge: query.Get("code_challenge"), callback: query.Get("redirect_uri"), clientID: clientID}
		callback, _ := url.Parse(query.Get("redirect_uri"))
		callback.RawQuery = url.Values{"state": {query.Get("state")}, "code": {code}, "client_id": {clientID}}.Encode()
		http.Redirect(w, r, callback.String(), http.StatusSeeOther)
	case "/token":
		f.tokenCalls++
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			f.refreshCalls++
			f.lastRefresh = r.Form
			if f.refreshFailure != "" {
				w.WriteHeader(f.refreshStatus)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": f.refreshFailure})
				return
			}
			_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: fmt.Sprintf("access-refreshed-%d", f.refreshCalls), RefreshToken: fmt.Sprintf("refresh-refreshed-%d", f.refreshCalls), TokenType: "Bearer", ExpiresIn: 3600, Scope: f.scope})
			return
		}
		auth, ok := f.codes[r.Form.Get("code")]
		digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || auth.clientID != r.Form.Get("client_id") || auth.callback != r.Form.Get("redirect_uri") || auth.challenge != base64.RawURLEncoding.EncodeToString(digest[:]) || r.Form.Get("resource") != Resource {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		delete(f.codes, r.Form.Get("code"))
		if f.codeFailures > 0 {
			f.codeFailures--
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		claims := jwt.MapClaims{"iss": f.server.URL, "aud": auth.clientID, "sub": "subscriber-test", "email": "person@example.test", "nonce": auth.nonce, "exp": f.now.Add(time.Hour).Unix(), "iat": f.now.Unix()}
		switch f.identityFailure {
		case "issuer":
			claims["iss"] = "https://invalid.test"
		case "audience":
			claims["aud"] = "other-client"
		case "expired":
			claims["exp"] = f.now.Add(-time.Second).Unix()
		case "nonce":
			claims["nonce"] = "wrong-nonce"
		case "subject":
			claims["sub"] = "another-person"
		case "missing_subject":
			delete(claims, "sub")
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "test-key"
		idToken, _ := token.SignedString(f.key)
		if f.identityFailure == "signature" {
			idToken += "tampered"
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "access-initial-secret", RefreshToken: "refresh-initial-secret", IDToken: idToken, TokenType: "Bearer", ExpiresIn: 3600, Scope: f.scope, EarliestRefreshAt: f.earliest})
	case "/v1/models":
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer access-") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []Model{{Slug: "first", DisplayName: "First", Visibility: "list"}, {Slug: "hidden", Visibility: "hidden"}, {Slug: "second", DisplayName: "Second", Visibility: "list"}}})
	case "/revoke":
		f.revokeCalls++
		_ = r.ParseForm()
		f.lastRevocation = r.Form
		w.WriteHeader(f.revokeStatus)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func setupManager(t *testing.T) (*Manager, *fakeOpenAI, *gorm.DB) {
	t.Helper()
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	db := pgtest.Open(t)
	f := newFakeOpenAI(t)
	m := NewManager(db, WithHTTPClient(f.server.Client()), WithEndpoints(f.endpoints()), WithClock(func() time.Time { return f.now }))
	return m, f, db
}

func authorize(t *testing.T, f *fakeOpenAI, start *LoginStart) url.Values {
	t.Helper()
	client := *f.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(start.AuthorizationURL)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	callback, err := url.Parse(response.Header.Get("Location"))
	require.NoError(t, err)
	return callback.Query()
}

func login(t *testing.T, m *Manager, f *fakeOpenAI, accountID string) *Account {
	t.Helper()
	start, err := m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", accountID)
	require.NoError(t, err)
	callback := authorize(t, f, start)
	account, err := m.CompleteLogin(context.Background(), callback.Get("state"), callback.Get("code"), callback.Get("client_id"))
	require.NoError(t, err)
	return account
}

func assertNoAccounts(t *testing.T, db *gorm.DB) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&accountRecord{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestLoginPKCEStateEncryptionAndPersistentHost(t *testing.T) {
	m, f, db := setupManager(t)
	start, err := m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", "")
	require.NoError(t, err)
	u, err := url.Parse(start.AuthorizationURL)
	require.NoError(t, err)
	query := u.Query()
	require.Equal(t, DynamicClientID, query.Get("client_id"))
	require.Equal(t, "Reliquary", query.Get("agent_name_hint"))
	require.Equal(t, "S256", query.Get("code_challenge_method"))
	require.Len(t, query.Get("state"), 43)
	require.Len(t, query.Get("nonce"), 43)
	var pending loginAttempt
	require.NoError(t, db.First(&pending, "state = ?", query.Get("state")).Error)
	require.True(t, strings.HasPrefix(pending.Verifier, utils.EncPrefix))
	callback := authorize(t, f, start)
	_, err = m.CompleteLogin(context.Background(), "attacker-state", callback.Get("code"), callback.Get("client_id"))
	require.ErrorIs(t, err, ErrInvalidState)
	require.Zero(t, f.tokenCalls)
	account, err := m.CompleteLogin(context.Background(), callback.Get("state"), callback.Get("code"), callback.Get("client_id"))
	require.NoError(t, err)
	require.Equal(t, StatusConnected, account.Status)
	require.Equal(t, "oaiapp_test", account.ClientID)
	var stored accountRecord
	require.NoError(t, db.First(&stored, "id = ?", account.ID).Error)
	require.True(t, strings.HasPrefix(stored.Credentials, utils.EncPrefix))
	require.NotContains(t, stored.Credentials, "access-initial-secret")
	require.NotContains(t, stored.Credentials, "refresh-initial-secret")
	var saved credentials
	require.NoError(t, decryptJSON(stored.Credentials, &saved))
	require.Equal(t, "access-initial-secret", saved.AccessToken)
	require.Equal(t, "refresh-initial-secret", saved.RefreshToken)
	serialized, err := json.Marshal(account)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), saved.AccessToken)
	require.NotContains(t, string(serialized), saved.IDToken)
	hostBefore, err := m.HostID(context.Background())
	require.NoError(t, err)
	other := NewManager(db)
	hostAfter, err := other.HostID(context.Background())
	require.NoError(t, err)
	require.Equal(t, hostBefore, hostAfter)
	require.Equal(t, query.Get("ext_agent_host_id"), hostBefore)
	require.True(t, strings.HasPrefix(hostBefore, "urn:uuid:"))
	_, err = m.CompleteLogin(context.Background(), callback.Get("state"), callback.Get("code"), callback.Get("client_id"))
	require.ErrorIs(t, err, ErrInvalidState)
}

func TestLoginRejectsInvalidIdentityAndInsufficientScopes(t *testing.T) {
	for _, failure := range []string{"signature", "issuer", "audience", "expired", "nonce", "missing_subject", "scope"} {
		t.Run(failure, func(t *testing.T) {
			m, f, db := setupManager(t)
			if failure == "scope" {
				f.scope = "openid profile email offline_access resource.invoke"
			} else {
				f.identityFailure = failure
			}
			start, err := m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", "")
			require.NoError(t, err)
			callback := authorize(t, f, start)
			_, err = m.CompleteLogin(context.Background(), callback.Get("state"), callback.Get("code"), callback.Get("client_id"))
			if failure == "scope" {
				require.ErrorIs(t, err, ErrInsufficientScopes)
			} else {
				require.ErrorIs(t, err, ErrInvalidIdentity)
			}
			assertNoAccounts(t, db)
		})
	}
}

func TestLoginRejectsInvalidPKCEAndClientID(t *testing.T) {
	m, f, db := setupManager(t)
	start, err := m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", "")
	require.NoError(t, err)
	callback := authorize(t, f, start)
	wrong, err := encryptJSON("wrong-verifier")
	require.NoError(t, err)
	require.NoError(t, db.Model(&loginAttempt{}).Where("state = ?", callback.Get("state")).Update("verifier", wrong).Error)
	_, err = m.CompleteLogin(context.Background(), callback.Get("state"), callback.Get("code"), callback.Get("client_id"))
	var endpoint *EndpointError
	require.True(t, errors.As(err, &endpoint))
	require.Equal(t, "invalid_grant", endpoint.Code)
	assertNoAccounts(t, db)
	for _, clientID := range []string{"", DynamicClientID} {
		start, err = m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", "")
		require.NoError(t, err)
		callback = authorize(t, f, start)
		_, err = m.CompleteLogin(context.Background(), callback.Get("state"), callback.Get("code"), clientID)
		require.ErrorIs(t, err, ErrInvalidClient)
	}
}

func TestLoginRequiresEncryptionAndLoopbackCallback(t *testing.T) {
	m, _, db := setupManager(t)
	for _, callback := range []string{"http://localhost:8080/auth/callback", "https://127.0.0.1:8080/auth/callback", "http://127.0.0.1/auth/callback", "http://127.0.0.1:8080/callback", "http://127.0.0.1:8080/auth/callback?x=1", "http://user@127.0.0.1:8080/auth/callback"} {
		_, err := m.BeginLogin(context.Background(), callback, "")
		require.ErrorIs(t, err, ErrInvalidCallback)
	}
	for _, key := range []string{"", "short"} {
		t.Setenv("SYSTEM_AES_KEY", key)
		_, err := m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", "")
		require.ErrorIs(t, err, ErrEncryptionRequired)
	}
	assertNoAccounts(t, db)
}

func TestReturningAccountIdentityAndRegistrationRemainSeparate(t *testing.T) {
	m, f, db := setupManager(t)
	account := login(t, m, f, "")
	start, err := m.BeginLogin(context.Background(), "http://127.0.0.1:28080/auth/callback", account.ID)
	require.NoError(t, err)
	u, _ := url.Parse(start.AuthorizationURL)
	require.Equal(t, account.ClientID, u.Query().Get("client_id"))
	require.Empty(t, u.Query().Get("agent_name_hint"))
	require.NotEmpty(t, u.Query().Get("id_token_hint"))
	require.Equal(t, account.Email, u.Query().Get("login_hint"))
	callback := authorize(t, f, start)
	_, err = m.CompleteLogin(context.Background(), callback.Get("state"), callback.Get("code"), "changed-client")
	require.ErrorIs(t, err, ErrInvalidClient)
	f.identityFailure = "subject"
	start, err = m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", account.ID)
	require.NoError(t, err)
	callback = authorize(t, f, start)
	_, err = m.CompleteLogin(context.Background(), callback.Get("state"), callback.Get("code"), "")
	require.ErrorIs(t, err, ErrInvalidIdentity)
	var stored accountRecord
	require.NoError(t, db.First(&stored, "id = ?", account.ID).Error)
	require.Equal(t, account.Subject, stored.Subject)
	require.Equal(t, account.ClientID, stored.ClientID)
	f.identityFailure = ""
	require.Equal(t, account.ID, login(t, m, f, account.ID).ID)
}

func TestRefreshNearExpiryRotatesAtomicallyAndSerializesManagers(t *testing.T) {
	m, f, db := setupManager(t)
	account := login(t, m, f, "")
	token, err := m.AccessToken(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, "access-initial-secret", token)
	require.Zero(t, f.refreshCalls)
	f.now = f.now.Add(59 * time.Minute)
	other := NewManager(db, WithHTTPClient(f.server.Client()), WithEndpoints(f.endpoints()), WithClock(func() time.Time { return f.now }))
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			manager := m
			if index%2 == 0 {
				manager = other
			}
			token, err := manager.AccessToken(context.Background(), account.ID)
			if err == nil && token != "access-refreshed-1" {
				err = fmt.Errorf("incorrect token")
			}
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Equal(t, 1, f.refreshCalls)
	require.Equal(t, account.ClientID, f.lastRefresh.Get("client_id"))
	require.Equal(t, "refresh-initial-secret", f.lastRefresh.Get("refresh_token"))
	require.Equal(t, Resource, f.lastRefresh.Get("resource"))
	require.NotContains(t, f.lastRefresh, "scope")
	var row accountRecord
	require.NoError(t, db.First(&row, "id = ?", account.ID).Error)
	var saved credentials
	require.NoError(t, decryptJSON(row.Credentials, &saved))
	require.Equal(t, "refresh-refreshed-1", saved.RefreshToken)
	require.Equal(t, "access-refreshed-1", saved.AccessToken)
	require.NotEmpty(t, saved.IDToken)
}

func TestRefreshFailureNeedsLoginPreservesTemporaryCredentials(t *testing.T) {
	for _, tc := range []struct {
		code    string
		status  int
		cleared bool
	}{{"invalid_grant", 400, true}, {"invalid_refresh_token", 400, true}, {"refresh_token_reused", 400, true}, {"temporarily_unavailable", 503, false}, {"invalid_client", 401, false}} {
		t.Run(tc.code, func(t *testing.T) {
			m, f, db := setupManager(t)
			account := login(t, m, f, "")
			f.now = f.now.Add(59 * time.Minute)
			f.refreshFailure, f.refreshStatus = tc.code, tc.status
			_, err := m.AccessToken(context.Background(), account.ID)
			require.Error(t, err)
			status, err := m.Status(context.Background(), account.ID)
			require.NoError(t, err)
			require.Equal(t, StatusNeedsLogin, status.Status)
			require.Equal(t, tc.code, status.LastError)
			var row accountRecord
			require.NoError(t, db.First(&row, "id = ?", account.ID).Error)
			if tc.cleared {
				require.Empty(t, row.Credentials)
			} else {
				require.NotEmpty(t, row.Credentials)
				f.refreshFailure = ""
				_, err = m.AccessToken(context.Background(), account.ID)
				require.NoError(t, err)
				status, err = m.Status(context.Background(), account.ID)
				require.NoError(t, err)
				require.Equal(t, StatusConnected, status.Status)
			}
		})
	}
}

func TestEarliestRefreshModelsWelcomeAndLogout(t *testing.T) {
	m, f, db := setupManager(t)
	f.earliest = json.RawMessage(fmt.Sprintf("%d", f.now.Add(59*time.Minute+30*time.Second).Unix()))
	account := login(t, m, f, "")
	f.now = f.now.Add(59 * time.Minute)
	_, err := m.AccessToken(context.Background(), account.ID)
	require.NoError(t, err)
	require.Zero(t, f.refreshCalls)
	f.now = f.now.Add(40 * time.Second)
	models, err := m.Models(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, []Model{{Slug: "first", DisplayName: "First", Visibility: "list"}, {Slug: "second", DisplayName: "Second", Visibility: "list"}}, models)
	require.Equal(t, 1, f.refreshCalls)
	require.False(t, account.WelcomeAcknowledged)
	require.NoError(t, m.AcknowledgeWelcome(context.Background(), account.ID))
	status, err := m.Status(context.Background(), account.ID)
	require.NoError(t, err)
	require.True(t, status.WelcomeAcknowledged)
	host, err := m.HostID(context.Background())
	require.NoError(t, err)
	result, err := m.Logout(context.Background(), account.ID)
	require.NoError(t, err)
	require.True(t, result.RemoteRevocationConfirmed)
	require.Equal(t, account.ClientID, f.lastRevocation.Get("client_id"))
	require.Equal(t, "refresh_token", f.lastRevocation.Get("token_type_hint"))
	require.Equal(t, "refresh-refreshed-1", f.lastRevocation.Get("token"))
	var row accountRecord
	require.NoError(t, db.First(&row, "id = ?", account.ID).Error)
	require.Equal(t, StatusSignedOut, row.Status)
	require.Empty(t, row.Credentials)
	require.Equal(t, account.Subject, row.Subject)
	require.Equal(t, account.ClientID, row.ClientID)
	_, err = m.AccessToken(context.Background(), account.ID)
	require.ErrorIs(t, err, ErrNeedsLogin)
	after, err := m.HostID(context.Background())
	require.NoError(t, err)
	require.Equal(t, host, after)
	start, err := m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", account.ID)
	require.NoError(t, err)
	u, _ := url.Parse(start.AuthorizationURL)
	require.Equal(t, account.ClientID, u.Query().Get("client_id"))
	require.Empty(t, u.Query().Get("id_token_hint"))
	accounts, err := m.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
}

func TestLogoutReportsUnconfirmedRevocationAndStillClearsLocalTokens(t *testing.T) {
	m, f, db := setupManager(t)
	account := login(t, m, f, "")
	f.revokeStatus = http.StatusServiceUnavailable
	result, err := m.Logout(context.Background(), account.ID)
	require.NoError(t, err)
	require.False(t, result.RemoteRevocationConfirmed)
	require.Equal(t, 3, f.revokeCalls)
	var row accountRecord
	require.NoError(t, db.First(&row, "id = ?", account.ID).Error)
	require.Empty(t, row.Credentials)
	require.Equal(t, "revocation_unconfirmed", row.LastError)
}

func TestExpiredAndDeclinedAuthorizationConsumeNoCredentials(t *testing.T) {
	m, _, db := setupManager(t)
	start, err := m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", "")
	require.NoError(t, err)
	require.NoError(t, m.CancelLogin(context.Background(), start.State, "access_denied"))
	require.ErrorIs(t, m.CancelLogin(context.Background(), start.State, "access_denied"), ErrInvalidState)
	start, err = m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", "")
	require.NoError(t, err)
	require.NoError(t, db.Model(&loginAttempt{}).Where("state = ?", start.State).Update("expires_at", time.Now().Add(-time.Hour)).Error)
	_, err = m.CompleteLogin(context.Background(), start.State, "unused", "oaiapp_test")
	require.ErrorIs(t, err, ErrInvalidState)
	assertNoAccounts(t, db)
}

func TestTemporaryRefreshNetworkFailureKeepsCredentials(t *testing.T) {
	m, f, db := setupManager(t)
	account := login(t, m, f, "")
	f.now = f.now.Add(59 * time.Minute)
	f.server.Close()
	_, err := m.AccessToken(context.Background(), account.ID)
	require.Error(t, err)
	var connectionError *types.ChatGPTError
	require.ErrorAs(t, err, &connectionError)
	require.NotNil(t, connectionError.Cause)
	var row accountRecord
	require.NoError(t, db.First(&row, "id = ?", account.ID).Error)
	require.Equal(t, StatusNeedsLogin, row.Status)
	require.Equal(t, "refresh_unavailable", row.LastError)
	require.NotEmpty(t, row.Credentials)
}

func TestSameEmailRegistrationsAndFailedReauthorizationStaySeparate(t *testing.T) {
	m, f, db := setupManager(t)
	first := login(t, m, f, "")
	second := login(t, m, f, "")
	require.Equal(t, first.Email, second.Email)
	require.Equal(t, first.Subject, second.Subject)
	require.NotEqual(t, first.ID, second.ID)
	require.NotEqual(t, first.ClientID, second.ClientID)
	var original accountRecord
	require.NoError(t, db.First(&original, "id = ?", first.ID).Error)
	f.scope = "openid profile email offline_access resource.invoke"
	start, err := m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", first.ID)
	require.NoError(t, err)
	callback := authorize(t, f, start)
	_, err = m.CompleteLogin(context.Background(), callback.Get("state"), callback.Get("code"), "")
	require.ErrorIs(t, err, ErrInsufficientScopes)
	var after accountRecord
	require.NoError(t, db.First(&after, "id = ?", first.ID).Error)
	require.Equal(t, original.Credentials, after.Credentials)
	require.Equal(t, original.Status, after.Status)
	accounts, err := m.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 2)
}

func TestInvalidAuthorizationCodeOffersFreshAttemptWithIssuedClient(t *testing.T) {
	m, f, db := setupManager(t)
	start, err := m.BeginLogin(context.Background(), "http://127.0.0.1:18080/auth/callback", "")
	require.NoError(t, err)
	originalURL, err := url.Parse(start.AuthorizationURL)
	require.NoError(t, err)
	originalQuery := originalURL.Query()
	callback := authorize(t, f, start)
	f.codeFailures = 1
	_, err = m.CompleteLogin(context.Background(), callback.Get("state"), callback.Get("code"), callback.Get("client_id"))
	var retry *AuthorizationRetryError
	require.True(t, errors.As(err, &retry))
	var upstream *EndpointError
	require.True(t, errors.As(err, &upstream))
	require.Equal(t, "invalid_grant", upstream.Code)
	assertNoAccounts(t, db)
	_, err = m.CompleteLogin(context.Background(), callback.Get("state"), callback.Get("code"), callback.Get("client_id"))
	require.ErrorIs(t, err, ErrInvalidState)
	require.Equal(t, 1, f.tokenCalls)
	var pending loginAttempt
	require.NoError(t, db.First(&pending, "state = ?", retry.State).Error)
	require.Nil(t, pending.AccountID)
	require.Equal(t, callback.Get("client_id"), pending.ClientID)
	require.Equal(t, "http://127.0.0.1:18080/auth/callback", retry.CallbackURL)
	retryURL, err := m.LoginAuthorizationURL(context.Background(), retry.State)
	require.NoError(t, err)
	parsed, err := url.Parse(retryURL)
	require.NoError(t, err)
	query := parsed.Query()
	require.Equal(t, "oaiapp_test", query.Get("client_id"))
	require.Empty(t, query.Get("agent_name_hint"))
	require.Equal(t, originalQuery.Get("redirect_uri"), query.Get("redirect_uri"))
	require.Equal(t, originalQuery.Get("ext_agent_host_id"), query.Get("ext_agent_host_id"))
	require.Equal(t, RequestedScopes, query.Get("scope"))
	require.NotEqual(t, originalQuery.Get("state"), query.Get("state"))
	require.NotEqual(t, originalQuery.Get("nonce"), query.Get("nonce"))
	require.NotEqual(t, originalQuery.Get("code_challenge"), query.Get("code_challenge"))
	newCallback := authorize(t, f, &LoginStart{AuthorizationURL: retryURL})
	account, err := m.CompleteLogin(context.Background(), newCallback.Get("state"), newCallback.Get("code"), "")
	require.NoError(t, err)
	require.Equal(t, "oaiapp_test", account.ClientID)
	require.Equal(t, StatusConnected, account.Status)
	require.Equal(t, 1, f.dynamicRegistrations)
}
