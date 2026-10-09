package router

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"reliquary/internal/handler"
)

func RegisterModelRoutingRoutes(r *gin.RouterGroup, h *handler.ModelRoutingHandler, g *accessGuards) {
	space := r.Group("/space")
	space.Handle(http.MethodGet, "/model-tiers", h.Get)
	space.Handle(http.MethodPut, "/model-tiers", h.Update)
}
