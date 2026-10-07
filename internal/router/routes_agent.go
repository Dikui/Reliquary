package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
)

func RegisterCustomAgentRoutes(r *gin.RouterGroup, agentHandler *handler.CustomAgentHandler, g *rbacGuards) {
	agents := g.apiKeyGroup(r.Group("/agents"), apiKeyFullAccess())
	// agentsRead are the agent read endpoints. They stay full-access only for
	// plain scoped keys (agent config can carry sensitive model/MCP bindings),
	// but read_agents, chat, or manage_agents may read them.
	agentsRead := agents.With(apiKeyReadAgents(apiKeyManageAgents(apiKeyChat(apiKeyFullAccess()))))
	// agentsWrite are the agent authoring endpoints. Owner by default, but a
	// key granted manage_agents may author agents without full Owner.
	agentsWrite := agents.With(apiKeyManageAgents(apiKeyFullAccess()))
	{
		// Get placeholder definitions (must be before /:id to avoid conflict) — Viewer+
		agentsRead.GET("/placeholders", g.Viewer(), agentHandler.GetPlaceholders)
		// List smart-reasoning agent type presets (rag-qa / wiki-qa / hybrid / custom) — Viewer+
		agentsRead.GET("/type-presets", g.Viewer(), agentHandler.GetAgentTypePresets)
		// Create custom agent — Contributor+
		agentsWrite.POST("", g.Contributor(), agentHandler.CreateAgent)
		// List all agents (including built-in) — Viewer+
		agentsRead.GET("", g.Viewer(), agentHandler.ListAgents)
		// Get agent by ID — Viewer+
		agentsRead.GET("/:id", g.Viewer(), agentHandler.GetAgent)
		agentsWrite.PUT("/:id", g.Contributor(), agentHandler.UpdateAgent)
		agentsWrite.DELETE("/:id", g.Contributor(), agentHandler.DeleteAgent)
		// Copy agent — Contributor+ (copy is owned by the caller)
		agentsWrite.POST("/:id/copy", g.Contributor(), agentHandler.CopyAgent)
	}
	g.apiKeyRoute(r, http.MethodGet, "/agents/:id/suggested-questions",
		apiKeyReadAgents(apiKeyManageAgents(apiKeyChat(apiKeyFullAccess()))), g.Viewer(), agentHandler.GetSuggestedQuestions)
}

func RegisterUserFavoriteRoutes(r *gin.RouterGroup, h *handler.UserResourceFavoriteHandler, g *rbacGuards) {
	// Favorites are per-user; not declared for API keys (default-deny).
	favs := r.Group("/user/favorites")
	{
		favs.GET("", g.Viewer(), h.ListFavorites)
		favs.POST("", g.Viewer(), h.AddFavorite)
		favs.DELETE("/:type/:id", g.Viewer(), h.RemoveFavorite)
	}
}

// RegisterSkillRoutes registers skill routes.
//
// PR 2 currently only exposes a read-only `ListSkills`; gated to
// Viewer+. Future skill upload / enable endpoints must use Admin+ since
// skills run sandboxed code on tenant resources.
func RegisterSkillRoutes(r *gin.RouterGroup, skillHandler *handler.SkillHandler, g *rbacGuards) {
	skills := r.Group("/skills")
	{
		// Usable skills for @ mention / chat — Viewer+
		skills.GET("", g.Viewer(), skillHandler.ListSkills)
		// Catalog reads are Viewer+ so the agent editor can show uninstalled skills.
		skills.GET("/catalog", g.Viewer(), skillHandler.ListCatalog)
	}
	// Catalog writes bake into sandbox images; scoped API keys cannot hold them.
	catalogWrite := g.apiKeyGroup(r.Group("/skills/catalog"), apiKeyFullAccess())
	{
		catalogWrite.POST("", g.Admin(), skillHandler.RegisterCatalog)
		catalogWrite.POST("/:id/install", g.Admin(), skillHandler.InstallCatalog)
		catalogWrite.GET("/:id/files", g.Admin(), skillHandler.ListCatalogFiles)
		catalogWrite.GET("/:id/files/content", g.Admin(), skillHandler.GetCatalogFile)
		catalogWrite.DELETE("/:id", g.Admin(), skillHandler.DeleteCatalog)
	}
}

// RegisterIMRoutes registers IM callback routes.
// These are registered BEFORE auth middleware since IM platforms use their own signature verification.
func RegisterIMRoutes(r *gin.Engine, imHandler *handler.IMHandler) {
	im := r.Group("/api/v1/im")
	{
		im.GET("/callback/:channel_id", imHandler.IMCallback)
		im.POST("/callback/:channel_id", imHandler.IMCallback)
	}
}

// RegisterIMChannelRoutes registers IM channel CRUD routes (requires authentication).
//
// Feishu channels store bot credentials. Viewing requires viewer, changes require admin.
func RegisterIMChannelRoutes(r *gin.RouterGroup, imHandler *handler.IMHandler, g *rbacGuards) {
	// Channel CRUD under agents
	agentChannels := g.apiKeyGroup(r.Group("/agents/:id/im-channels"), apiKeyManageChannels(apiKeyFullAccess()))
	{
		agentChannels.POST("", g.Admin(), imHandler.CreateIMChannel)
		agentChannels.GET("", g.Viewer(), imHandler.ListIMChannels)
	}

	// Channel operations by channel ID
	channels := g.apiKeyGroup(r.Group("/im-channels"), apiKeyManageChannels(apiKeyFullAccess()))
	{
		channels.GET("", g.Viewer(), imHandler.ListAllIMChannels)
		channels.PUT("/:id", g.Admin(), imHandler.UpdateIMChannel)
		channels.DELETE("/:id", g.Admin(), imHandler.DeleteIMChannel)
		channels.POST("/:id/toggle", g.Admin(), imHandler.ToggleIMChannel)
	}

}
