package middleware

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	apprepo "reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strconv"
	"strings"
)

var noAuthAPI = map[string][]string{
	"/health":                 {"GET"},
	"/api/v1/files/presigned": {"GET", "HEAD"},
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

// Auth verifies API keys, or treats a request without credentials as the sole user.
func Auth(spaces interfaces.SpaceService, users interfaces.UserService, keys interfaces.APIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions || isNoAuthAPI(c.Request.URL.Path, c.Request.Method) {
			c.Next()
			return
		}
		ctx := c.Request.Context()
		mcpRequest := c.Request.URL.Path == "/mcp"
		var user *types.User
		var principal types.Principal
		credential, hasBearer := bearerToken(c)
		if !hasBearer && !mcpRequest {
			credential = c.GetHeader("X-API-Key")
		}
		if credential != "" {
			if keys != nil {
				key, err := keys.AuthenticateAPIKey(ctx, credential)
				if authUnavailable(c, err) {
					return
				}
				if err == nil && key != nil && key.SpaceIDValue() == types.SingleSpaceID {
					user, err = users.GetUserBySpaceID(ctx, types.SingleSpaceID)
					if err != nil && !errors.Is(err, apprepo.ErrUserNotFound) {
						authUnavailable(c, apperrors.NewServiceUnavailableError("Authentication is temporarily unavailable"))
						return
					}
					principal = types.Principal{Type: types.PrincipalAPIKey, ID: strconv.FormatUint(key.ID, 10), Name: key.Name}
				}
			}
		} else if !mcpRequest && localRequest(c) {
			// Login was removed (ADR 0015): the local web UI acts as the sole user.
			var err error
			user, err = users.EnsureSoleUser(ctx)
			if err != nil {
				authUnavailable(c, apperrors.NewServiceUnavailableError("Authentication is temporarily unavailable"))
				return
			}
		}
		if user == nil || !user.IsActive || user.SpaceID != types.SingleSpaceID {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: invalid or missing credentials"})
			return
		}
		if raw := c.GetHeader("X-Tenant-ID"); raw != "" {
			id, err := strconv.ParseUint(raw, 10, 64)
			if err != nil || id != types.SingleSpaceID {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "only the personal workspace is available"})
				return
			}
		}
		space, err := spaces.GetSpaceByID(ctx, types.SingleSpaceID)
		if err != nil && !errors.Is(err, apprepo.ErrSpaceNotFound) {
			authUnavailable(c, apperrors.NewServiceUnavailableError("Authentication is temporarily unavailable"))
			return
		}
		if err != nil || space == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid personal workspace"})
			return
		}
		if !principal.Valid() {
			principal = types.Principal{Type: types.PrincipalWebUser, ID: user.ID}
		}
		applyAuthSession(c, authSession{User: user, Principal: principal, SpaceID: types.SingleSpaceID, Space: space})
		c.Next()
	}
}

// localRequest admits credential-less callers only on a loopback Host (blocks DNS
// rebinding) and never from another site's page (blocks CSRF and cross-site reads).
func localRequest(c *gin.Context) bool {
	switch c.GetHeader("Sec-Fetch-Site") {
	case "", "none", "same-origin", "same-site":
	default:
		return false
	}
	host := c.Request.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// authUnavailable preserves credentials when their database checks cannot run.
func authUnavailable(c *gin.Context, err error) bool {
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.HTTPCode != http.StatusServiceUnavailable {
		return false
	}
	c.Header("Retry-After", "5")
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": appErr})
	return true
}
