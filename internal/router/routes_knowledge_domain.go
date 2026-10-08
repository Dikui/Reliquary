package router

import (
	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
)

func RegisterKnowledgeDomainRoutes(r *gin.RouterGroup, h *handler.KnowledgeDomainHandler, g *accessGuards) {
	if h == nil {
		return
	}
	domains := r.Group("/knowledge-domains")
	read := domains
	read.GET("", h.List)
	read.GET("/:id", h.Get)
	read.GET("/:id/open-fields", h.OpenFields)
	domains.POST("", h.Create)
	domains.PUT("/:id", h.Update)
	domains.DELETE("/:id", h.Delete)
	domains.POST("/:id/field-mappings", h.MapField)
	domains.POST("/:id/fields", h.AddField)
}
