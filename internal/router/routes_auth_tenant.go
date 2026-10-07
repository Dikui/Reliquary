package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
	"reliquary/internal/middleware"
)

// RegisterTenantRoutes exposes settings and membership for the single team.
func RegisterTenantRoutes(r *gin.RouterGroup, handler *handler.TenantHandler, memberHandler *handler.TenantMemberHandler, invitationHandler *handler.TenantInvitationHandler, auditLogHandler *handler.AuditLogHandler, g *rbacGuards) {
	tenantRoutes := r.Group("/tenants")
	g.apiKeyRoute(tenantRoutes, http.MethodGet, "/kv/:key", apiKeyManageTenantSettings(apiKeyFullAccess()), g.Viewer(), handler.GetTenantKV)
	g.apiKeyRoute(tenantRoutes, http.MethodPut, "/kv/:key", apiKeyManageTenantSettings(apiKeyFullAccess()), g.Admin(), handler.UpdateTenantKV)
	team := tenantRoutes.Group("/:id", g.PathTenantMatch())
	g.apiKeyRoute(team, http.MethodGet, "", apiKeyManageTenantSettings(apiKeyFullAccess()), g.Viewer(), handler.GetTenant)
	g.apiKeyRoute(team, http.MethodPut, "", apiKeyManageTenantSettings(apiKeyFullAccess()), g.Owner(), handler.UpdateTenant)
	team.GET("/api-keys", g.Owner(), handler.ListAPIKeys)
	team.POST("/api-keys", g.Owner(), handler.CreateAPIKey)
	team.PUT("/api-keys/:key_id", g.Owner(), handler.UpdateAPIKey)
	team.DELETE("/api-keys/:key_id", g.Owner(), handler.DeleteAPIKey)
	team.GET("/api-principal-config", g.Owner(), handler.GetAPIPrincipalConfig)
	team.PUT("/api-principal-config", g.Owner(), handler.UpdateAPIPrincipalConfig)
	team.POST("/api-principal-test-token", g.Owner(), handler.CreateAPIPrincipalTestToken)
	if memberHandler != nil {
		g.apiKeyRoute(team, http.MethodGet, "/members", apiKeyManageMembers(apiKeyFullAccess()), g.Viewer(), memberHandler.ListMembers)
		g.apiKeyRoute(team, http.MethodPut, "/members/:user_id", apiKeyManageMembers(apiKeyFullAccess()), g.Owner(), memberHandler.UpdateMemberRole)
		g.apiKeyRoute(team, http.MethodDelete, "/members/:user_id", apiKeyManageMembers(apiKeyFullAccess()), g.Owner(), memberHandler.RemoveMember)
		team.POST("/leave", g.Viewer(), memberHandler.LeaveTenant)
	}
	if invitationHandler != nil {
		g.apiKeyRoute(team, http.MethodGet, "/invitations", apiKeyManageMembers(apiKeyFullAccess()), g.Viewer(), invitationHandler.ListTenantInvitations)
		g.apiKeyRoute(team, http.MethodPost, "/invitations", apiKeyManageMembers(apiKeyFullAccess()), g.Owner(), invitationHandler.CreateInvitation)
		g.apiKeyRoute(team, http.MethodDelete, "/invitations/:inv_id", apiKeyManageMembers(apiKeyFullAccess()), g.Owner(), invitationHandler.RevokeInvitation)
		g.apiKeyRoute(team, http.MethodPost, "/invite-links", apiKeyManageMembers(apiKeyFullAccess()), g.Owner(), invitationHandler.CreateInviteLink)
	}
	if auditLogHandler != nil {
		team.GET("/audit-log", g.Admin(), auditLogHandler.ListTenantAuditLog)
	}
}

// RegisterMyInvitationRoutes wires the per-user invitation inbox under
// /me/invitations. The v1 group already applies middleware.Auth so we
// don't need a role gate here — the service enforces "only the invitee
// can accept/decline". The list endpoint mounts under /me to make the
// "show me MY invitations" semantics obvious in URLs and logs (vs the
// tenant-scoped /tenants/:id/invitations which lists ALL invitations
// for the tenant). pending-count is a separate, ultra-light endpoint
// the avatar-row badge polls; splitting it off so polling doesn't
// transfer the full list every cycle.
//
// invitationHandler may be nil in environments built without the
// invitation dependency wired; that's a no-op registration which is
// preferable to a startup crash.
func RegisterMyInvitationRoutes(r *gin.RouterGroup, invitationHandler *handler.TenantInvitationHandler) {
	if invitationHandler == nil {
		return
	}
	me := r.Group("/me")
	{
		me.GET("/invitations", invitationHandler.ListMyInvitations)
		me.GET("/invitations/pending-count", invitationHandler.CountMyPendingInvitations)
		me.POST("/invitations/:inv_id/accept", invitationHandler.AcceptMyInvitation)
		me.POST("/invitations/:inv_id/decline", invitationHandler.DeclineMyInvitation)
		// 已登录用户用共享链接 token 加入空间（对应 register-by-invite，但不建新账号）。
		me.POST("/invitations/accept-by-token", invitationHandler.AcceptMyInvitationByToken)
	}
}

// RegisterMyEnvVarRoutes wires the caller's own environment variables under
// /me/env-vars. The v1 group already applies middleware.Auth, and no role gate
// is added on purpose: these are the caller's own values, and the service
// derives whose they are from the context rather than the request.
//
// This deliberately does not reuse /sandbox-configs/:id/skills*, which is
// Admin+ even for reads (see routes_infra.go): an upload there drives a root
// shell whose output is baked into the image, and the listing names what that
// image carries. This endpoint returns declarations and set/unset status only.
//
// h may be nil in environments built without the dependency wired; a no-op
// registration is preferable to a startup crash, as with the invitation inbox.
func RegisterMyEnvVarRoutes(r *gin.RouterGroup, h *handler.MeEnvVarHandler) {
	if h == nil {
		return
	}
	me := r.Group("/me/env-vars")
	{
		me.GET("", h.List)
		me.PUT("/skill", h.SetSkill)
		me.DELETE("/skill", h.DeleteSkill)
		me.PUT("/sandbox", h.SetSandbox)
		me.DELETE("/sandbox", h.DeleteSandbox)
	}
}

// RegisterAuthRoutes registers authentication routes
func RegisterAuthRoutes(r *gin.RouterGroup, handler *handler.AuthHandler, g *rbacGuards) {
	r.POST("/auth/register", handler.Register)
	// Share-link surfaces are unauthenticated and accept a plaintext
	// token from the caller; rate-limit by IP to bound brute-force /
	// enumeration / abuse traffic. Limiter is shared across both
	// endpoints (see middleware/auth_public_ratelimit.go) so total
	// budget per IP is intuitive.
	publicAuthRL := middleware.PublicAuthRateLimit()
	r.POST("/auth/register-by-invite", publicAuthRL, handler.RegisterByInvite)
	r.POST("/auth/invitations/lookup", publicAuthRL, handler.LookupInvitationByToken)
	r.POST("/auth/login", handler.Login)
	r.GET("/auth/config", handler.GetAuthConfig)
	r.POST("/auth/refresh", handler.RefreshToken)
	r.GET("/auth/validate", handler.ValidateToken)
	r.POST("/auth/logout", handler.Logout)
	// auth/me returns only the caller's own identity/profile, so it is safe
	// for any valid API key. Chat clients / MCP call it to discover "who am I";
	// leaving it default-deny was why scoped keys got a 403 here.
	g.apiKeyRoute(r, http.MethodGet, "/auth/me", apiKeyAny(), handler.GetCurrentUser)
	r.POST("/auth/change-password", handler.ChangePassword)
}

// RegisterSystemRoutes registers system information routes
//
// Reads (GetSystemInfo / ListParserEngines / GetStorageEngineStatus)
// are gated to Viewer+ — any tenant member can see "is the parser
// reachable". The /*-check / /reconnect endpoints actively probe
// remote services with tenant credentials and could trigger network
// fanout, so they're Admin+.
func RegisterSystemRoutes(
	r *gin.RouterGroup,
	handler *handler.SystemHandler,
	g *rbacGuards,
) {
	systemRoutes := g.apiKeyGroup(r.Group("/system"), apiKeyManageVectorStores(apiKeyFullAccess()))
	{
		systemRoutes.With(apiKeyAny()).GET("/capabilities", g.Viewer(), handler.GetDeploymentCapabilities)
		systemRoutes.GET("/info", g.Viewer(), handler.GetSystemInfo)
		systemRoutes.GET("/parser-engines", g.Viewer(), handler.ListParserEngines)
		systemRoutes.POST("/parser-engines/check", g.Admin(), handler.CheckParserEngines)
		systemRoutes.POST("/docreader/reconnect", g.Admin(), handler.ReconnectDocReader)
		systemRoutes.GET("/storage-engine-status", g.Viewer(), handler.GetStorageEngineStatus)
		systemRoutes.POST("/storage-engine-check", g.Admin(), handler.CheckStorageEngine)
		systemRoutes.POST("/sandbox-check", g.Admin(), handler.CheckSandboxConfig)
	}
}

// RegisterSystemAdminRoutes registers system administration routes.
//
// All endpoints under this group are gated to SystemAdmin users (i.e.
// User.IsSystemAdmin == true). These are platform-wide operations
// independent of per-tenant Owner/Admin/Contributor/Viewer roles —
// they let org-level superusers grant/revoke system-admin status and,
// in later milestones, will host global settings, built-in models, and
// cross-tenant observability.
//
// Mounted under /api/v1/system/admin/* so the URL scheme stays aligned
// with the existing /api/v1/system/info family. Front-end clients live
// in frontend/src/api/system/index.ts.
//
// auditLogHandler may be nil in environments wired without the audit
// dependency; the /audit-log subroute is then omitted. This mirrors
// the optional wiring in RegisterTenantRoutes.
func RegisterSystemAdminRoutes(
	r *gin.RouterGroup,
	handler *handler.SystemHandler,
	auditLogHandler *handler.AuditLogHandler,
	g *rbacGuards,
) {
	// Apply SystemAdmin() at the group level — every route below inherits
	// the guard, so adding new endpoints can't accidentally drop the gate.
	adminRoutes := r.Group("/system/admin", g.SystemAdmin())
	{
		// P0: SystemAdmin role management
		adminRoutes.POST("/promote", handler.PromoteUserToSystemAdmin)
		adminRoutes.POST("/revoke", handler.RevokeSystemAdmin)
		adminRoutes.GET("/list", handler.ListSystemAdmins)
		adminRoutes.POST("/users/reset-password", handler.ResetUserPassword)

		// P1: platform-wide system settings (DB-backed runtime tunables).
		// Reads return raw model rows / arrays (no `gin.H{"data":...}`
		// wrapping), matching the project's axios interceptor convention
		// — see frontend/src/utils/request.ts:97.
		// The single-team deployment keeps these JWT-only system admin routes.
		adminRoutes.GET("/settings", handler.ListSystemSettings)
		adminRoutes.GET("/settings/:key", handler.GetSystemSetting)
		adminRoutes.PUT("/settings/:key", handler.UpdateSystemSetting)
		adminRoutes.DELETE("/settings/:key", handler.ResetSystemSetting)

		// Queue operations expose safe task projections and state-checked actions.
		adminRoutes.GET("/runtime/queues", handler.GetRuntimeQueues)
		adminRoutes.GET("/runtime/queues/:queue/tasks", handler.ListRuntimeTasks)
		adminRoutes.POST("/runtime/queues/:queue/tasks/:task_id/actions/:action", handler.MutateRuntimeTask)
		adminRoutes.DELETE("/runtime/queues/:queue/archived", handler.PurgeArchivedRuntimeTasks)

		// Platform-wide audit feed (tenant_id=0 rows). Covers
		// system.setting_changed / system.admin_promoted /
		// system.admin_revoked etc. — events written by the routes
		// above. Without this endpoint those audit rows would have
		// no UI surface (per-tenant ListTenantAuditLog filters them
		// out by tenant_id). Optional: skip when audit deps are
		// absent, matching RegisterTenantRoutes' /audit-log handling.
		if auditLogHandler != nil {
			adminRoutes.GET("/audit-log", auditLogHandler.ListSystemAuditLog)
		}
	}
}
