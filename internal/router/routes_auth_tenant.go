package router

import (
	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
	"reliquary/internal/middleware"
)

// RegisterTenantRoutes exposes personal workspace settings and API keys.
func RegisterTenantRoutes(r *gin.RouterGroup, h *handler.TenantHandler, g *accessGuards) {
	routes := r.Group("/space")
	routes.GET("/kv/:key", h.GetTenantKV)
	routes.PUT("/kv/:key", h.UpdateTenantKV)
	routes.GET("", h.GetTenant)
	routes.PUT("", h.UpdateTenant)
	routes.GET("/api-keys", h.ListAPIKeys)
	routes.POST("/api-keys", h.CreateAPIKey)
	routes.PUT("/api-keys/:key_id", h.UpdateAPIKey)
	routes.DELETE("/api-keys/:key_id", h.DeleteAPIKey)
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

func RegisterSystemRoutes(r *gin.RouterGroup, h *handler.SystemHandler, g *accessGuards) {
	routes := r.Group("/system")
	routes.GET("/capabilities", h.GetDeploymentCapabilities)
	routes.GET("/info", h.GetSystemInfo)
	routes.GET("/parser-engines", h.ListParserEngines)
	routes.POST("/parser-engines/check", h.CheckParserEngines)
	routes.GET("/settings", h.ListSystemSettings)
	routes.GET("/settings/:key", h.GetSystemSetting)
	routes.PUT("/settings/:key", h.UpdateSystemSetting)
	routes.DELETE("/settings/:key", h.ResetSystemSetting)
	routes.GET("/runtime/queues", h.GetRuntimeQueues)
	routes.GET("/runtime/queues/:queue/tasks", h.ListRuntimeTasks)
	routes.POST("/runtime/queues/:queue/tasks/:task_id/actions/:action", h.MutateRuntimeTask)
	routes.DELETE("/runtime/queues/:queue/archived", h.PurgeArchivedRuntimeTasks)
}
