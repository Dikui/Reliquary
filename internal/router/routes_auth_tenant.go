package router

import (
	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
	"reliquary/internal/middleware"
)

// RegisterTenantRoutes exposes personal workspace settings and API keys.
func RegisterTenantRoutes(r *gin.RouterGroup, h *handler.TenantHandler, g *accessGuards) {
	routes := r.Group("/tenants")
	routes.GET("/kv/:key", h.GetTenantKV)
	routes.PUT("/kv/:key", h.UpdateTenantKV)
	team := routes.Group("/:id", g.PathTenantMatch())
	team.GET("", h.GetTenant)
	team.PUT("", h.UpdateTenant)
	team.GET("/api-keys", h.ListAPIKeys)
	team.POST("/api-keys", h.CreateAPIKey)
	team.PUT("/api-keys/:key_id", h.UpdateAPIKey)
	team.DELETE("/api-keys/:key_id", h.DeleteAPIKey)
}

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
func RegisterAuthRoutes(r *gin.RouterGroup, h *handler.AuthHandler, g *accessGuards) {
	public := r.Group("/auth", middleware.PublicAuthRateLimit())
	public.POST("/register", h.Register)
	public.POST("/login", h.Login)
	public.GET("/config", h.GetAuthConfig)
	public.POST("/refresh", h.RefreshToken)
	r.GET("/auth/validate", h.ValidateToken)
	r.POST("/auth/logout", h.Logout)
	r.GET("/auth/me", h.GetCurrentUser)
	r.POST("/auth/change-password", h.ChangePassword)
}

func RegisterSystemRoutes(
	r *gin.RouterGroup,
	handler *handler.SystemHandler,
	g *accessGuards) {
	systemRoutes := r.Group("/system")
	{
		systemRoutes.GET("/capabilities", handler.GetDeploymentCapabilities)
		systemRoutes.GET("/info", handler.GetSystemInfo)
		systemRoutes.GET("/parser-engines", handler.ListParserEngines)
		systemRoutes.POST("/parser-engines/check", handler.CheckParserEngines)
		systemRoutes.POST("/docreader/reconnect", handler.ReconnectDocReader)
		systemRoutes.GET("/storage-engine-status", handler.GetStorageEngineStatus)
		systemRoutes.POST("/storage-engine-check", handler.CheckStorageEngine)
		systemRoutes.POST("/sandbox-check", handler.CheckSandboxConfig)
	}
}

func RegisterSystemAdminRoutes(r *gin.RouterGroup, h *handler.SystemHandler, g *accessGuards) {
	routes := r.Group("/system/admin")
	routes.GET("/settings", h.ListSystemSettings)
	routes.GET("/settings/:key", h.GetSystemSetting)
	routes.PUT("/settings/:key", h.UpdateSystemSetting)
	routes.DELETE("/settings/:key", h.ResetSystemSetting)
	routes.GET("/runtime/queues", h.GetRuntimeQueues)
	routes.GET("/runtime/queues/:queue/tasks", h.ListRuntimeTasks)
	routes.POST("/runtime/queues/:queue/tasks/:task_id/actions/:action", h.MutateRuntimeTask)
	routes.DELETE("/runtime/queues/:queue/archived", h.PurgeArchivedRuntimeTasks)
}
