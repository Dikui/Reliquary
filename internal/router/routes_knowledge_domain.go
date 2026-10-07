package router

import (
	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
)

func RegisterKnowledgeDomainRoutes(r *gin.RouterGroup, h *handler.KnowledgeDomainHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	domains := g.apiKeyGroup(r.Group("/knowledge-domains"), apiKeyFullAccess())
	read := domains.With(apiKeyRetrieve(apiKeyFullAccess()))
	read.GET("", g.Viewer(), h.List)
	read.GET("/:id", g.Viewer(), h.Get)
	read.GET("/:id/open-fields", g.Viewer(), h.OpenFields)
	domains.POST("", g.Admin(), h.Create)
	domains.PUT("/:id", g.Admin(), h.Update)
	domains.DELETE("/:id", g.Admin(), h.Delete)
	domains.POST("/:id/field-mappings", g.Admin(), h.MapField)
	domains.POST("/:id/fields", g.Admin(), h.AddField)
}
