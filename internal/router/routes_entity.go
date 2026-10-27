package router

import (
	"reliquary/internal/handler"

	"github.com/gin-gonic/gin"
)

func RegisterEntityRoutes(r *gin.RouterGroup, h *handler.EntityHandler) {
	if h == nil {
		return
	}
	group := r.Group("/entities")
	group.GET("", h.Entities)
	group.POST("", h.SaveEntity)
	group.PATCH("/:entity_id", h.SaveEntity)
	group.DELETE("/:entity_id", h.DeleteEntity)
	r.PUT("/entries/:id/entities", h.SetEntryEntities)
	group.POST("/merge", h.MergeEntities)
	group.GET("/:entity_id", h.EntityDetail)
	group.GET("/:entity_id/facts", h.Facts)
	group.GET("/:entity_id/merge-records", h.MergeRecords)
}
