package middleware

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"reliquary/internal/config"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

func RequirePathTenantMatch(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		raw := strings.TrimSpace(c.Param("id"))
		if raw == "" {
			_ = c.Error(apperrors.NewValidationError("workspace id is required"))
			c.Abort()
			return
		}
		pathTenantID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || pathTenantID == 0 {
			_ = c.Error(apperrors.NewValidationError("workspace id must be a positive integer"))
			c.Abort()
			return
		}
		ctxTenantID, ok := types.TenantIDFromContext(ctx)
		if !ok || ctxTenantID == 0 {
			// Auth middleware should always have set this; if not we
			// fail closed rather than silently treating "no context" as
			// a match.
			logger.Warnf(ctx, "[rbac] path-tenant-match: no tenant in ctx, path=%s", c.Request.URL.Path)
			_ = c.Error(apperrors.NewUnauthorizedError("workspace context missing"))
			c.Abort()
			return
		}
		if pathTenantID == ctxTenantID {
			c.Next()
			return
		}

		uid, _ := types.UserIDFromContext(ctx)
		logger.Warnf(ctx,
			"[rbac] path-tenant-match rejected: user=%s ctx_tenant=%d path_tenant=%d path=%s",
			uid, ctxTenantID, pathTenantID, c.Request.URL.Path)
		_ = c.Error(apperrors.NewForbiddenError(
			"Access denied: URL workspace does not match the active workspace"))
		c.Abort()
	}
}
