package router

import (
	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
)

// RegisterSpaceRoutes exposes personal workspace settings and API keys.
func RegisterSpaceRoutes(r *gin.RouterGroup, h *handler.SpaceHandler, g *accessGuards) {
	routes := r.Group("/space")
	routes.GET("/kv/:key", h.GetSpaceKV)
	routes.PUT("/kv/:key", h.UpdateSpaceKV)
	routes.GET("", h.GetSpace)
	routes.PUT("", h.UpdateSpace)
	routes.GET("/api-keys", h.ListAPIKeys)
	routes.POST("/api-keys", h.CreateAPIKey)
	routes.PUT("/api-keys/:key_id", h.UpdateAPIKey)
	routes.DELETE("/api-keys/:key_id", h.DeleteAPIKey)
}

func RegisterSystemRoutes(r *gin.RouterGroup, h *handler.SystemHandler, g *accessGuards) {
	routes := r.Group("/system")
	routes.GET("/capabilities", h.GetDeploymentCapabilities)
	routes.GET("/info", h.GetSystemInfo)
	routes.GET("/settings", h.ListSystemSettings)
	routes.GET("/settings/:key", h.GetSystemSetting)
	routes.PUT("/settings/:key", h.UpdateSystemSetting)
	routes.DELETE("/settings/:key", h.ResetSystemSetting)
	routes.GET("/runtime/queues", h.GetRuntimeQueues)
	routes.GET("/runtime/queues/:queue/tasks", h.ListRuntimeTasks)
	routes.POST("/runtime/queues/:queue/tasks/:task_id/actions/:action", h.MutateRuntimeTask)
	routes.DELETE("/runtime/queues/:queue/archived", h.PurgeArchivedRuntimeTasks)
}
