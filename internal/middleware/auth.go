package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strconv"
	"strings"
)

var noAuthAPI = map[string][]string{
	"/health":                    {"GET"},
	"/api/v1/auth/register":      {"POST"},
	"/api/v1/auth/login":         {"POST"},
	"/api/v1/auth/config":        {"GET"},
	"/api/v1/auth/refresh":       {"POST"},
	"/api/v1/mcp-oauth/callback": {"GET"},
	"/api/v1/files/presigned":    {"GET", "HEAD"},
}

func isNoAuthAPI(path, method string) bool {
	for _, allowed := range noAuthAPI[path] {
		if method == allowed {
			return true
		}
	}
	return false
}

func bearerToken(c *gin.Context) (string, bool) {
	value := c.GetHeader("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return "", false
	}
	return strings.TrimPrefix(value, "Bearer "), true
}

// Auth verifies credentials and attaches the sole user's workspace identity.
func Auth(tenants interfaces.TenantService, users interfaces.UserService, keys interfaces.TenantAPIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions || isNoAuthAPI(c.Request.URL.Path, c.Request.Method) {
			c.Next()
			return
		}
		ctx := c.Request.Context()
		var user *types.User
		if token, ok := bearerToken(c); ok {
			candidate, tenantID, err := users.ValidateToken(ctx, token)
			if err == nil && tenantID == types.SingleTenantID {
				user = candidate
			}
		}
		if user == nil && keys != nil && c.GetHeader("X-API-Key") != "" {
			key, err := keys.AuthenticateAPIKey(ctx, c.GetHeader("X-API-Key"))
			if err == nil && key != nil && key.TenantIDValue() == types.SingleTenantID {
				user, _ = users.GetUserByTenantID(ctx, types.SingleTenantID)
			}
		}
		if user == nil || !user.IsActive || user.TenantID != types.SingleTenantID {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: invalid or missing credentials"})
			return
		}
		if raw := c.GetHeader("X-Tenant-ID"); raw != "" {
			id, err := strconv.ParseUint(raw, 10, 64)
			if err != nil || id != types.SingleTenantID {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "only the personal workspace is available"})
				return
			}
		}
		tenant, err := tenants.GetTenantByID(ctx, types.SingleTenantID)
		if err != nil || tenant == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid personal workspace"})
			return
		}
		applyAuthSession(c, authSession{User: user, Principal: types.Principal{Type: types.PrincipalWebUser, ID: user.ID}, TenantID: types.SingleTenantID, Tenant: tenant})
		c.Next()
	}
}
