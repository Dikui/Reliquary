package router

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"reliquary/internal/handler"
)

func RegisterModelRoutingRoutes(r *gin.RouterGroup, h *handler.ModelRoutingHandler, g *accessGuards) {
	team := r.Group("/tenants")
	team.Handle(http.MethodGet, "/model-tiers", h.Get)
	team.Handle(http.MethodPut, "/model-tiers", h.Update)
}
