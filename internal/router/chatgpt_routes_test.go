package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/chatgpt"
	"reliquary/internal/handler"
)

func TestChatGPTSettingsRequireLocalUserAndCallbackChecksState(t *testing.T) {
	f := newPersonalAuthFixture(t)
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("CHATGPT_CALLBACK_URL", "http://127.0.0.1:18117/auth/callback")
	t.Setenv("LOCAL_STORAGE_BASE_DIR", t.TempDir())
	f.params.ChatGPTHandler = handler.NewChatGPTHandler(chatgpt.NewManager(f.db))
	f.params.ChatGPTPolicyHandler = handler.NewChatGPTPolicyHandler(service.NewChatGPTPolicyService(f.db))
	f.router = NewRouter(f.params)
	for _, path := range []string{"/api/v1/chatgpt/accounts", "/api/v1/chatgpt/policy"} {
		unauth := personalRequest(t, f.router, http.MethodGet, path, "", "invalid-key", true)
		require.Equal(t, http.StatusUnauthorized, unauth.Code)
		apiKey := personalRequest(t, f.router, http.MethodGet, path, "", f.key, true)
		require.Equal(t, http.StatusForbidden, apiKey.Code)
		user := personalRequest(t, f.router, http.MethodGet, path, "", f.token, false)
		require.Equal(t, http.StatusOK, user.Code, user.Body.String())
	}
	login := personalRequest(t, f.router, http.MethodPost, "/api/v1/chatgpt/login", "{}", f.token, false)
	require.Equal(t, http.StatusOK, login.Code, login.Body.String())
	require.Contains(t, login.Body.String(), "127.0.0.1:18117/auth/chatgpt/continue")
	require.NotContains(t, login.Body.String(), "id_token_hint")
	require.NotContains(t, login.Body.String(), "code_verifier")
	require.NotContains(t, login.Body.String(), "nonce")
	badCallback := personalRequest(t, f.router, http.MethodGet, "/auth/callback?state=invalid&code=secret-code&client_id=oaiapp_fixture", "", "", false)
	require.Equal(t, http.StatusBadRequest, badCallback.Code)
	require.NotContains(t, badCallback.Body.String(), "secret-code")
	badContinue := personalRequest(t, f.router, http.MethodGet, "/auth/chatgpt/continue?authorization_attempt=invalid", "", "", false)
	require.Equal(t, http.StatusBadRequest, badContinue.Code)

	updated := personalRequest(t, f.router, http.MethodPut, "/api/v1/chatgpt/policy", `{"daily_limit":2,"fallback_model_id":""}`, f.token, false)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	var policy struct {
		Success bool `json:"success"`
		Data    struct {
			DailyLimit int `json:"daily_limit"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &policy))
	require.True(t, policy.Success)
	require.Equal(t, 2, policy.Data.DailyLimit)
	badLimit := personalRequest(t, f.router, http.MethodPut, "/api/v1/chatgpt/policy", `{"daily_limit":0}`, f.token, false)
	require.Equal(t, http.StatusBadRequest, badLimit.Code)
	resume := personalRequest(t, f.router, http.MethodPost, "/api/v1/chatgpt/resume", "{}", f.token, false)
	require.Equal(t, http.StatusOK, resume.Code, resume.Body.String())
}
