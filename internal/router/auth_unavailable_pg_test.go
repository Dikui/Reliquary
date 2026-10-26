package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	apperrors "reliquary/internal/errors"
)

func TestAuthenticationDatabaseOutagePreservesCredentialErrors(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for _, credential := range []struct {
		value  string
		apiKey bool
	}{{"invalid-token", false}, {"invalid-key", true}} {
		response := personalRequest(t, f.router, http.MethodGet, "/api/v1/space", "", credential.value, credential.apiKey)
		require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
	}
	pool, err := f.db.DB()
	require.NoError(t, err)
	require.NoError(t, pool.Close())
	for _, credential := range []struct {
		name, value string
		apiKey      bool
	}{{"local user", "", false}, {"API key header", f.key, true}, {"API key bearer", f.key, false}} {
		t.Run(credential.name, func(t *testing.T) {
			response := personalRequest(t, f.router, http.MethodGet, "/api/v1/space", "", credential.value, credential.apiKey)
			require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
			require.Equal(t, "5", response.Header().Get("Retry-After"))
			var body struct {
				Error apperrors.AppError `json:"error"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, apperrors.ErrServiceUnavailable, body.Error.Code)
			require.NotContains(t, response.Body.String(), "sql:")
		})
	}
}
