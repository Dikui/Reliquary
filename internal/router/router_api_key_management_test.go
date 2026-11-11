package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersonalAPIKeyCanManageAPIKeys(t *testing.T) {
	f := newPersonalAuthFixture(t)
	create := personalRequest(t, f.router, http.MethodPost, "/api/v1/space/api-keys", `{"name":"managed through key"}`, f.key, true)
	require.Equal(t, http.StatusCreated, create.Code, create.Body.String())
	var response struct {
		Data struct {
			ID    uint64 `json:"id"`
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(create.Body.Bytes(), &response))
	require.NotEmpty(t, response.Data.Token)
	path := fmt.Sprintf("/api/v1/space/api-keys/%d", response.Data.ID)
	updated := personalRequest(t, f.router, http.MethodPut, path, `{"name":"renamed"}`, f.key, true)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.Contains(t, updated.Body.String(), "renamed")
	removed := personalRequest(t, f.router, http.MethodDelete, path, "", f.key, true)
	require.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	revoked := personalRequest(t, f.router, http.MethodGet, "/api/v1/entry-domains", "", response.Data.Token, true)
	require.Equal(t, http.StatusUnauthorized, revoked.Code, revoked.Body.String())
}

// Medium regression, task 33: only the create response reveals a usable token;
// neither creation, listing nor editing reveals the stored credential.
func TestAPIKeyResponsesOnlyRevealTokenAtCreation(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprint(encrypted), func(t *testing.T) {
			if encrypted {
				t.Setenv("SYSTEM_AES_KEY", strings.Repeat("s", 32))
			} else {
				t.Setenv("SYSTEM_AES_KEY", "")
			}
			f := newPersonalAuthFixture(t)
			created := personalRequest(t, f.router, http.MethodPost, "/api/v1/space/api-keys", `{"name":"Synthetic integration"}`, f.key, true)
			require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
			var response struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(created.Body.Bytes(), &response))
			token, ok := response.Data["token"].(string)
			require.True(t, ok)
			require.NotEmpty(t, token)
			require.NotContains(t, response.Data, "api_key")
			id := response.Data["id"]
			for _, request := range []struct{ method, path, body string }{
				{http.MethodGet, "/api/v1/space/api-keys", ""},
				{http.MethodPut, fmt.Sprintf("/api/v1/space/api-keys/%.0f", id), `{"name":"Renamed integration"}`},
			} {
				result := personalRequest(t, f.router, request.method, request.path, request.body, f.key, true)
				require.Equal(t, http.StatusOK, result.Code, result.Body.String())
				require.NotContains(t, result.Body.String(), `"api_key":`)
				require.NotContains(t, result.Body.String(), `"token":`)
				require.NotContains(t, result.Body.String(), token)
				require.NotContains(t, result.Body.String(), "enc:v1")
			}
			usable := personalRequest(t, f.router, http.MethodGet, "/api/v1/space", "", token, true)
			require.Equal(t, http.StatusOK, usable.Code, usable.Body.String())
		})
	}
}
