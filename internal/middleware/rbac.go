package middleware

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"reliquary/internal/config"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// ErrResourceNotFound identifies a resource outside the current team.
var ErrResourceNotFound = errors.New("rbac: resource not found")

// RequireRole returns a gin middleware that aborts the request with
// HTTP 403 unless the caller's TenantRole (set by the auth middleware
// in TenantRoleContextKey) is at least min.
//
// When cfg.Tenant.EnableRBAC is false, the middleware logs the would-be
// rejection but lets the request through — preserving today's behaviour
// during the rollout window. Once operators flip the flag to true,
// the same code paths start rejecting unauthorised callers.
//
// The auth middleware always sets a TenantRole; if for some reason it
// is missing, TenantRoleFromContext defaults to TenantRoleViewer, which
// is the safest fail-closed value: anything that requires more than
// Viewer will reject.
func RequireRole(min types.TenantRole, cfg *config.Config) gin.HandlerFunc {
	warnOnNilConfig(cfg)
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		// API-key principals are authorized solely by the APIKeyGate
		// (role + KB scope + default-deny). The JWT role ladder does not
		// apply to a machine principal, so short-circuit here.
		if _, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
			c.Next()
			return
		}
		role := types.TenantRoleFromContext(ctx)
		if role.HasPermission(min) {
			c.Next()
			return
		}

		uid, _ := types.UserIDFromContext(ctx)
		if !rbacEnforcementEnabled(cfg) {
			logger.Warnf(ctx,
				"[rbac] role insufficient (logged but not enforced): user=%s have=%s need=%s path=%s",
				uid, role, min, c.Request.URL.Path)
			c.Next()
			return
		}
		logger.Warnf(ctx,
			"[rbac] role insufficient: user=%s have=%s need=%s path=%s",
			uid, role, min, c.Request.URL.Path)
		// Durable audit row for the reject. AuditServiceProvider
		// injects the service; subject to 1-minute sliding-window
		// dedup inside the service so probing clients can't fill the
		// table.
		if svc := AuditServiceFromContext(c); svc != nil {
			tenantID, _ := types.TenantIDFromContext(ctx)
			_ = svc.LogDenied(ctx, c, tenantID, uid, string(role), min)
		}
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Forbidden: insufficient workspace role",
		})
		c.Abort()
	}
}

// RequireRoleOrSystemAdmin applies the tenant role floor while also allowing
// platform system administrators. Use it for routes that normally mutate
// tenant infrastructure but have a narrowly-scoped platform-owned resource
// (for example built-in models) that system administrators must be able to
// maintain independently of their role in the active tenant.
//
// API-key behavior remains identical to RequireRole: the APIKeyGate is the
// source of truth for machine principals, so they short-circuit the role
// check here as well.
func RequireRoleOrSystemAdmin(min types.TenantRole, cfg *config.Config) gin.HandlerFunc {
	requireRole := RequireRole(min, cfg)
	return func(c *gin.Context) {
		if types.IsSystemAdminFromContext(c.Request.Context()) {
			c.Next()
			return
		}
		requireRole(c)
	}
}

// RequireSystemAdmin returns a gin middleware that aborts the request with
// HTTP 403 unless the caller is a system administrator
// (User.IsSystemAdmin = true).
//
// System administrators operate independently of tenant-scoped roles and
// are not bound by the per-tenant RBAC matrix. Use this guard for
// platform-wide administrative endpoints (managing other system admins,
// editing global settings, cross-workspace operations) where the per-tenant
// Owner/Admin/Contributor/Viewer ladder does not apply.
//
// Unlike tenant-role guards, this check is always enforced. The
// tenant RBAC rollout switch only controls per-tenant Owner/Admin/etc.
// checks; it must not turn platform-wide administration endpoints into
// "any authenticated user can call this" endpoints.
func RequireSystemAdmin(cfg *config.Config) gin.HandlerFunc {
	warnOnNilConfig(cfg)
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		// The API-key gate runs before this guard and default-denies undeclared
		// routes. Only platform keys that passed an explicit platform capability
		// policy may reuse the system-admin handlers below.
		if _, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {

			logger.Warnf(ctx,
				"[rbac] system admin required: API-key principal denied path=%s",
				c.Request.URL.Path)
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Forbidden: API keys cannot access this endpoint",
			})
			c.Abort()
			return
		}
		if types.IsSystemAdminFromContext(ctx) {
			c.Next()
			return
		}
		uid, _ := types.UserIDFromContext(ctx)
		logger.Warnf(ctx,
			"[rbac] system admin required: user=%s path=%s",
			uid, c.Request.URL.Path)
		// Durable audit row for the reject — same dedup as RequireRole.
		if svc := AuditServiceFromContext(c); svc != nil {
			tenantID, _ := types.TenantIDFromContext(ctx)
			_ = svc.LogDenied(ctx, c, tenantID, uid, "user", "system_admin")
		}
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Forbidden: system administrator required",
		})
		c.Abort()
	}
}

// rbacEnforcementEnabled reports whether middleware should actually
// reject failed checks. When the flag is off the middleware still runs
// role-only checks (logging, fast paths), but rejection is downgraded
// to a warning and ownership lookups are skipped entirely so the dormant
// rollout window incurs no per-request DB cost.
func rbacEnforcementEnabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Tenant.IsRBACEnforced()
}

// ErrOwnershipForbidden is returned by EvaluateOwnershipOrRole when the
// caller is neither the resource creator nor meets the minimum role.

// warnOnNilConfig emits a one-shot startup warning when a guard is
// constructed with a nil-or-incomplete config. nil cfg makes
// rbacEnforcementEnabled return false, which means an entire deployment
// silently runs with RBAC disabled — usually because of a configuration
// bug rather than an intentional choice. Operators should see a noisy
// log line at boot pointing at the misconfiguration.
var nilCfgWarnOnce sync.Once

func warnOnNilConfig(cfg *config.Config) {
	if cfg != nil && cfg.Tenant != nil {
		return
	}
	nilCfgWarnOnce.Do(func() {
		logger.Errorf(context.Background(),
			"[rbac] middleware constructed with nil/incomplete config "+
				"(cfg=%v); enforcement is permanently disabled. This is "+
				"almost certainly a wiring bug.", cfg)
	})
}
