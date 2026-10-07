package router

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"reliquary/internal/handler"
)

func RegisterModelRoutingRoutes(r *gin.RouterGroup, h *handler.ModelRoutingHandler, g *rbacGuards) {
	team := r.Group("/tenants")
	g.apiKeyRoute(team, http.MethodGet, "/model-tiers", apiKeyManageTenantSettings(apiKeyFullAccess()), g.Viewer(), h.Get)
	g.apiKeyRoute(team, http.MethodPut, "/model-tiers", apiKeyManageTenantSettings(apiKeyFullAccess()), g.Admin(), h.Update)
}
