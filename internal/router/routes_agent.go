package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
)

func RegisterCustomAgentRoutes(r *gin.RouterGroup, agentHandler *handler.CustomAgentHandler, g *accessGuards) {
	agents := r.Group("/agents")
	agentsRead := agents
	// agentsWrite are the agent authoring endpoints. Owner by default, but a
	// key granted manage_agents may author agents without full Owner.
	agentsWrite := agents
	{
		agentsRead.GET("/placeholders", agentHandler.GetPlaceholders)
		agentsRead.GET("/type-presets", agentHandler.GetAgentTypePresets)
		agentsWrite.POST("", agentHandler.CreateAgent)
		agentsRead.GET("", agentHandler.ListAgents)
		agentsRead.GET("/:id", agentHandler.GetAgent)
		agentsWrite.PUT("/:id", agentHandler.UpdateAgent)
		agentsWrite.DELETE("/:id", agentHandler.DeleteAgent)
		agentsWrite.POST("/:id/copy", agentHandler.CopyAgent)
	}
	r.Handle(http.MethodGet, "/agents/:id/suggested-questions", agentHandler.GetSuggestedQuestions)
}

func RegisterUserFavoriteRoutes(r *gin.RouterGroup, h *handler.UserResourceFavoriteHandler, g *accessGuards) {
	// Favorites are per-user; not declared for API keys (default-deny).
	favs := r.Group("/user/favorites")
	{
		favs.GET("", h.ListFavorites)
		favs.POST("", h.AddFavorite)
		favs.DELETE("/:type/:id", h.RemoveFavorite)
	}
}
