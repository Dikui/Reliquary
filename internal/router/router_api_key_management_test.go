package router

import (
	"encoding/json"
	"fmt"
	"net/http"
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
