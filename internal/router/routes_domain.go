package router

import (
	"reliquary/internal/handler"

	"github.com/gin-gonic/gin"
)

// RegisterDomainRoutes mounts domain maintenance and material editing.
func RegisterDomainRoutes(r *gin.RouterGroup, h *handler.DomainHandler) {
	if h == nil {
		return
	}
	r.GET("/domains", h.List)
	r.POST("/domains", h.Save)
	r.PATCH("/domains/:domain_id", h.Save)
	r.DELETE("/domains/:domain_id", h.Delete)
	r.PUT("/entries/:id/domains", h.SetEntryDomains)
}
