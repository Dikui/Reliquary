package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"reliquary/internal/chatgpt"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func chatGPTTestHTTPApp(t *testing.T, codeFailures ...int) *httptest.Server {
	return chatGPTTestHTTPAppWithLogs(t, nil, codeFailures...)
}

func chatGPTTestHTTPAppWithLogs(t *testing.T, logOutput io.Writer, codeFailures ...int) *httptest.Server {
	t.Helper()
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	db := pgtest.Open(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var mu sync.Mutex
	var nonce, challenge, redirectURI string
	remainingCodeFailures := 0
	if len(codeFailures) > 0 {
		remainingCodeFailures = codeFailures[0]
	}
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": upstream.URL, "jwks_uri": upstream.URL + "/jwks", "revocation_endpoint": upstream.URL + "/revoke"})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "handler-key", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/authorize":
			q := r.URL.Query()
			nonce, challenge, redirectURI = q.Get("nonce"), q.Get("code_challenge"), q.Get("redirect_uri")
			callback, _ := url.Parse(redirectURI)
			callback.RawQuery = url.Values{"state": {q.Get("state")}, "code": {"handler-code"}, "client_id": {"oaiapp_handler"}}.Encode()
			http.Redirect(w, r, callback.String(), http.StatusSeeOther)
		case "/token":
			_ = r.ParseForm()
			digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("code") != "handler-code" || r.Form.Get("redirect_uri") != redirectURI || challenge != base64.RawURLEncoding.EncodeToString(digest[:]) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if remainingCodeFailures > 0 {
				remainingCodeFailures--
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			idToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": upstream.URL, "aud": "oaiapp_handler", "sub": "handler-person", "email": "handler@example.test", "nonce": nonce, "exp": time.Now().Add(time.Hour).Unix()})
			idToken.Header["kid"] = "handler-key"
			signed, _ := idToken.SignedString(key)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "handler-access-secret", "refresh_token": "handler-refresh-secret", "id_token": signed, "token_type": "Bearer", "expires_in": 3600, "scope": chatgpt.RequestedScopes})
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer handler-access-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]string{"slug": "model-visible", "display_name": "Visible model", "visibility": "list"}, map[string]string{"slug": "model-hidden", "visibility": "hidden"}}})
		case "/revoke":
			_ = r.ParseForm()
			if r.Form.Get("token") != "handler-refresh-secret" || r.Form.Get("client_id") != "oaiapp_handler" {
				w.WriteHeader(http.StatusBadRequest)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)
	manager := chatgpt.NewManager(db, chatgpt.WithHTTPClient(upstream.Client()), chatgpt.WithEndpoints(chatgpt.Endpoints{Issuer: upstream.URL, Authorization: upstream.URL + "/authorize", Token: upstream.URL + "/token", Discovery: upstream.URL + "/.well-known/openid-configuration", Models: upstream.URL + "/v1/models"}))
	h := NewChatGPTHandler(manager)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.ContextWithFallback = true
	if logOutput != nil {
		requestLogger := logrus.New()
		requestLogger.SetOutput(logOutput)
		requestLogger.SetFormatter(&logrus.JSONFormatter{})
		r.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.LoggerContextKey, logrus.NewEntry(requestLogger)))
			c.Next()
		})
		r.Use(middleware.Logger())
	}
	r.POST("/api/v1/chatgpt/login", h.StartLogin)
	r.GET("/api/v1/chatgpt/accounts", h.Accounts)
	r.GET("/api/v1/chatgpt/accounts/:account_id/models", h.Models)
	r.POST("/api/v1/chatgpt/accounts/:account_id/logout", h.Logout)
	r.POST("/api/v1/chatgpt/welcome/ack", h.AckWelcome)
	r.GET("/auth/callback", h.Callback)
	r.GET("/auth/chatgpt/continue", h.ContinueLogin)
	app := httptest.NewServer(r)
	t.Cleanup(app.Close)
	t.Setenv("CHATGPT_CALLBACK_URL", app.URL+"/auth/callback")
	t.Setenv("CHATGPT_SETTINGS_URL", app.URL+"/platform/settings?section=models")
	return app
}

func chatGPTTestRequest(t *testing.T, app *httptest.Server, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, app.URL+path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := app.Client().Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, payload
}

func TestChatGPTSettingsHTTPLoginCatalogWelcomeLogout(t *testing.T) {
	app := chatGPTTestHTTPApp(t)
	status, payload := chatGPTTestRequest(t, app, http.MethodGet, "/api/v1/chatgpt/accounts", nil)
	require.Equal(t, http.StatusOK, status)
	var initial struct {
		Data struct {
			Accounts        []chatgpt.Account `json:"accounts"`
			EncryptionReady bool              `json:"encryption_ready"`
			CallbackURL     string            `json:"callback_url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(payload, &initial))
	require.Empty(t, initial.Data.Accounts)
	require.True(t, initial.Data.EncryptionReady)
	require.Equal(t, app.URL+"/auth/callback", initial.Data.CallbackURL)
	status, payload = chatGPTTestRequest(t, app, http.MethodPost, "/api/v1/chatgpt/login", map[string]string{})
	require.Equal(t, http.StatusOK, status)
	var started struct {
		Data chatgpt.LoginStart `json:"data"`
	}
	require.NoError(t, json.Unmarshal(payload, &started))
	require.True(t, strings.HasPrefix(started.Data.AuthorizationURL, app.URL+"/auth/chatgpt/continue?"))
	require.NotContains(t, string(payload), "id_token_hint")
	response, err := app.Client().Get(started.Data.AuthorizationURL)
	require.NoError(t, err)
	callbackPage, err := io.ReadAll(response.Body)
	response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, string(callbackPage), "已连接 ChatGPT")
	require.Contains(t, string(callbackPage), "chatgpt_login=success")
	require.NotContains(t, string(callbackPage), "handler-access-secret")
	require.NotContains(t, string(callbackPage), "handler-refresh-secret")
	require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
	status, payload = chatGPTTestRequest(t, app, http.MethodGet, "/api/v1/chatgpt/accounts", nil)
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal(payload, &initial))
	require.Len(t, initial.Data.Accounts, 1)
	account := initial.Data.Accounts[0]
	require.Equal(t, chatgpt.StatusConnected, account.Status)
	require.False(t, account.WelcomeAcknowledged)
	require.NotContains(t, string(payload), "handler-access-secret")
	require.NotContains(t, string(payload), "handler-refresh-secret")
	status, payload = chatGPTTestRequest(t, app, http.MethodGet, "/api/v1/chatgpt/accounts/"+account.ID+"/models", nil)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, string(payload), "model-visible")
	require.NotContains(t, string(payload), "model-hidden")
	status, _ = chatGPTTestRequest(t, app, http.MethodPost, "/api/v1/chatgpt/welcome/ack", map[string]string{"account_id": account.ID})
	require.Equal(t, http.StatusOK, status)
	status, payload = chatGPTTestRequest(t, app, http.MethodGet, "/api/v1/chatgpt/accounts", nil)
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal(payload, &initial))
	require.True(t, initial.Data.Accounts[0].WelcomeAcknowledged)
	// A later login returns only a local link, even with a retained ID-token hint.
	status, payload = chatGPTTestRequest(t, app, http.MethodPost, "/api/v1/chatgpt/login", map[string]string{"account_id": account.ID})
	require.Equal(t, http.StatusOK, status)
	require.NotContains(t, string(payload), "id_token_hint")
	require.NotContains(t, string(payload), "eyJ")
	status, payload = chatGPTTestRequest(t, app, http.MethodPost, "/api/v1/chatgpt/accounts/"+account.ID+"/logout", nil)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, string(payload), `"remote_revocation_confirmed":true`)
	status, payload = chatGPTTestRequest(t, app, http.MethodGet, "/api/v1/chatgpt/accounts", nil)
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal(payload, &initial))
	require.Equal(t, chatgpt.StatusSignedOut, initial.Data.Accounts[0].Status)
	require.Equal(t, account.ClientID, initial.Data.Accounts[0].ClientID)
}

func TestChatGPTCallbackRejectsUnknownOrAmbiguousState(t *testing.T) {
	app := chatGPTTestHTTPApp(t)
	for _, query := range []string{"state=wrong&code=unused&client_id=oaiapp_handler", "state=first&state=second&code=unused", "state=wrong&error=access_denied"} {
		status, payload := chatGPTTestRequest(t, app, http.MethodGet, "/auth/callback?"+query, nil)
		require.Equal(t, http.StatusBadRequest, status)
		require.NotContains(t, string(payload), "handler-access-secret")
	}
	_, payload := chatGPTTestRequest(t, app, http.MethodGet, "/api/v1/chatgpt/accounts", nil)
	require.Contains(t, string(payload), `"accounts":[]`)
}

func TestChatGPTCallbackOffersExplicitSafeAuthorizationRetry(t *testing.T) {
	app := chatGPTTestHTTPApp(t, 1)
	status, payload := chatGPTTestRequest(t, app, http.MethodPost, "/api/v1/chatgpt/login", map[string]string{})
	require.Equal(t, http.StatusOK, status)
	var started struct {
		Data chatgpt.LoginStart `json:"data"`
	}
	require.NoError(t, json.Unmarshal(payload, &started))
	response, err := app.Client().Get(started.Data.AuthorizationURL)
	require.NoError(t, err)
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Contains(t, string(page), "Continue with ChatGPT")
	require.NotContains(t, string(page), "handler-access-secret")
	require.NotContains(t, string(page), "handler-refresh-secret")
	// The first link is the explicit local retry action; no redirect loop occurs.
	marker := `<a href="`
	index := strings.Index(string(page), marker)
	require.GreaterOrEqual(t, index, 0)
	remaining := string(page)[index+len(marker):]
	end := strings.Index(remaining, `"`)
	require.GreaterOrEqual(t, end, 0)
	retryURL := html.UnescapeString(remaining[:end])
	require.True(t, strings.HasPrefix(retryURL, app.URL+"/auth/chatgpt/continue?"))
	require.NotEqual(t, started.Data.AuthorizationURL, retryURL)
	_, payload = chatGPTTestRequest(t, app, http.MethodGet, "/api/v1/chatgpt/accounts", nil)
	require.Contains(t, string(payload), `"accounts":[]`)
	response, err = app.Client().Get(retryURL)
	require.NoError(t, err)
	page, err = io.ReadAll(response.Body)
	response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, string(page), "已连接 ChatGPT")
	_, payload = chatGPTTestRequest(t, app, http.MethodGet, "/api/v1/chatgpt/accounts", nil)
	require.Contains(t, string(payload), `"client_id":"oaiapp_handler"`)
}

func TestChatGPTReturningRedirectHasEmptyBodyAndLoggerOmitsIDTokenHint(t *testing.T) {
	var logs bytes.Buffer
	app := chatGPTTestHTTPAppWithLogs(t, &logs)
	_, payload := chatGPTTestRequest(t, app, http.MethodPost, "/api/v1/chatgpt/login", map[string]string{})
	var started struct {
		Data chatgpt.LoginStart `json:"data"`
	}
	require.NoError(t, json.Unmarshal(payload, &started))
	response, err := app.Client().Get(started.Data.AuthorizationURL)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	_, payload = chatGPTTestRequest(t, app, http.MethodGet, "/api/v1/chatgpt/accounts", nil)
	var accounts struct {
		Data struct {
			Accounts []chatgpt.Account `json:"accounts"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(payload, &accounts))
	require.Len(t, accounts.Data.Accounts, 1)
	_, payload = chatGPTTestRequest(t, app, http.MethodPost, "/api/v1/chatgpt/login", map[string]string{"account_id": accounts.Data.Accounts[0].ID})
	require.NoError(t, json.Unmarshal(payload, &started))
	launch, err := url.Parse(started.Data.AuthorizationURL)
	require.NoError(t, err)
	state := launch.Query().Get("authorization_attempt")
	require.NotEmpty(t, state)
	client := *app.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err = client.Get(started.Data.AuthorizationURL)
	require.NoError(t, err)
	redirectBody, err := io.ReadAll(response.Body)
	response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Empty(t, redirectBody)
	location, err := url.Parse(response.Header.Get("Location"))
	require.NoError(t, err)
	idToken := location.Query().Get("id_token_hint")
	require.NotEmpty(t, idToken)
	require.Equal(t, "oaiapp_handler", location.Query().Get("client_id"))
	require.NotContains(t, logs.String(), idToken)
	require.NotContains(t, logs.String(), state)
	require.NotContains(t, logs.String(), "handler-access-secret")
	require.NotContains(t, logs.String(), "handler-refresh-secret")
	require.Contains(t, logs.String(), "[OAuth response omitted]")
}

func TestChatGPTAuthorizationRetryHTMLKeepsFreshStateOutOfLogger(t *testing.T) {
	var logs bytes.Buffer
	app := chatGPTTestHTTPAppWithLogs(t, &logs, 1)
	_, payload := chatGPTTestRequest(t, app, http.MethodPost, "/api/v1/chatgpt/login", map[string]string{})
	var started struct {
		Data chatgpt.LoginStart `json:"data"`
	}
	require.NoError(t, json.Unmarshal(payload, &started))
	response, err := app.Client().Get(started.Data.AuthorizationURL)
	require.NoError(t, err)
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	marker := `<a href="`
	index := strings.Index(string(page), marker)
	require.GreaterOrEqual(t, index, 0)
	remaining := string(page)[index+len(marker):]
	end := strings.Index(remaining, `"`)
	require.GreaterOrEqual(t, end, 0)
	retryURL, err := url.Parse(html.UnescapeString(remaining[:end]))
	require.NoError(t, err)
	state := retryURL.Query().Get("authorization_attempt")
	require.NotEmpty(t, state)
	require.Contains(t, string(page), state)
	require.NotContains(t, logs.String(), state)
	require.NotContains(t, logs.String(), "handler-code")
	require.Contains(t, logs.String(), "[OAuth response omitted]")
	// Other response bodies still use the existing logger path.
	_, _ = chatGPTTestRequest(t, app, http.MethodGet, "/api/v1/chatgpt/accounts", nil)
	require.Contains(t, logs.String(), "callback_url")
	_, _ = chatGPTTestRequest(t, app, http.MethodGet, "/auth/callback?state=unknown&code=code-secret&nonce=nonce-secret&id_token_hint=id-secret&code_verifier=verifier-secret", nil)
	for _, secret := range []string{"code-secret", "nonce-secret", "id-secret", "verifier-secret"} {
		require.NotContains(t, logs.String(), secret)
	}
}
