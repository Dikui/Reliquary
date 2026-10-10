package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/handler"
)

func TestMCPServiceOAuthRoutesRemoved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterMCPServiceRoutes(r.Group("/api/v1"), handler.NewMCPServiceHandler(nil, nil, nil), handler.NewMCPCredentialsHandler(nil), nil)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/mcp-oauth/callback"},
		{http.MethodPost, "/api/v1/mcp-services/service-1/oauth/authorize-url"},
		{http.MethodGet, "/api/v1/mcp-services/service-1/oauth/status"},
		{http.MethodDelete, "/api/v1/mcp-services/service-1/oauth/token"},
		{http.MethodPost, "/api/v1/agent/mcp-oauth-resolutions/pending-1"},
		{http.MethodPost, "/api/v1/agent/mcp-oauth-resolutions/pending-1/cancel"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
		})
	}
}
