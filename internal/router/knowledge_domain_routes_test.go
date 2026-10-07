package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/types"
)

func TestKnowledgeDomainRoutesDeclareReadAndAdministrationPolicies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enforce := true
	g := &rbacGuards{cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enforce}}}
	r := gin.New()
	RegisterKnowledgeDomainRoutes(r.Group("/api/v1"), &handler.KnowledgeDomainHandler{}, g)
	g.assertAPIKeyPoliciesMatchRoutes(r)
	for _, path := range []string{"/knowledge-domains", "/knowledge-domains/:id", "/knowledge-domains/:id/open-fields"} {
		policy := mustLookupAPIKeyPolicy(t, g, http.MethodGet, "/api/v1"+path)
		require.True(t, policyHasCapability(policy, types.APIKeyCapabilityRetrieve))
	}
	for _, operation := range []struct{ method, path string }{
		{http.MethodPost, "/knowledge-domains"},
		{http.MethodPut, "/knowledge-domains/:id"},
		{http.MethodDelete, "/knowledge-domains/:id"},
		{http.MethodPost, "/knowledge-domains/:id/field-mappings"},
		{http.MethodPost, "/knowledge-domains/:id/fields"},
	} {
		policy := mustLookupAPIKeyPolicy(t, g, operation.method, "/api/v1"+operation.path)
		require.True(t, policy.RequireFullAccess)
		require.Empty(t, policy.Capabilities)
		for _, role := range []types.TenantRole{types.TenantRoleViewer, types.TenantRoleContributor} {
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
			ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
			request := httptest.NewRequest(operation.method, "/api/v1"+strings.ReplaceAll(operation.path, ":id", "domain-id"), strings.NewReader(`{}`)).WithContext(ctx)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			require.Equal(t, http.StatusForbidden, response.Code, "%s %s role=%s", operation.method, operation.path, role)
		}
	}
}
