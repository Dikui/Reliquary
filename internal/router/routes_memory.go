package router

import (
	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
)

func RegisterMemoryRoutes(r *gin.RouterGroup, memoryHandler *handler.MemoryHandler, g *accessGuards) {
	if memoryHandler == nil {
		return
	}
	memoryGroup := r.Group("/memory")
	{
		memoryGroup.GET("/settings", memoryHandler.GetSettings)
		memoryGroup.PUT("/settings", memoryHandler.UpdateSettings)
		memoryGroup.GET("/items", memoryHandler.ListItems)
		memoryGroup.POST("/items", memoryHandler.CreateItem)
		memoryGroup.DELETE("/items", memoryHandler.Clear)
		memoryGroup.PUT("/items/:id", memoryHandler.UpdateItem)
		memoryGroup.DELETE("/items/:id", memoryHandler.DeleteItem)
		memoryGroup.POST("/items/:id/confirm", memoryHandler.ConfirmItem)
		memoryGroup.POST("/items/:id/reject", memoryHandler.RejectItem)
		memoryGroup.GET("/topics", memoryHandler.ListTopics)
		memoryGroup.DELETE("/topics/:id", memoryHandler.DeleteTopic)
		memoryGroup.POST("/topics/:id/promote", memoryHandler.PromoteTopic)
		memoryGroup.GET("/documents", memoryHandler.ListDocuments)
		memoryGroup.DELETE("/documents/:id", memoryHandler.DeleteDocument)
		memoryGroup.GET("/export", memoryHandler.Export)
		memoryGroup.POST("/consolidate", memoryHandler.Consolidate)
	}
}
