package router

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"reliquary/internal/handler"
)

func RegisterKnowledgeContextRoutes(r *gin.RouterGroup, h *handler.KnowledgeContextHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	team := g.apiKeyGroup(r.Group("/knowledge-context"), apiKeyIngest(apiKeyFullAccess()))
	teamRead := team.With(apiKeyRetrieve(apiKeyFullAccess()))
	teamRead.POST("/query", g.Viewer(), h.Query)
	teamRead.GET("/entities", g.Viewer(), h.Entities)
	teamRead.GET("/entities/:entity_id", g.Viewer(), h.EntityDetail)
	teamRead.GET("/events", g.Viewer(), h.Events)
	teamRead.GET("/merge-suggestions", g.Viewer(), h.MergeSuggestions)
	teamRead.GET("/evidence", g.Viewer(), h.Evidence)
	teamRead.GET("/evidence/:evidence_id", g.Viewer(), h.GetEvidence)
	teamRead.GET("/focus", g.Viewer(), h.FocusScope)
	team.PUT("/entities/:entity_id/star", g.Contributor(), h.SetEntityStar)
	team.DELETE("/entities/:entity_id/star", g.Contributor(), h.SetEntityStar)
	team.PUT("/topics/:tag_id/star", g.Contributor(), h.SetTopicStar)
	team.DELETE("/topics/:tag_id/star", g.Contributor(), h.SetTopicStar)
	admin := r.Group("/knowledge-context")
	g.apiKeyRoute(admin, http.MethodGet, "/name-embeddings/rebuild", apiKeyManageTenantSettings(apiKeyFullAccess()), g.Admin(), h.NameEmbeddingRebuildStatus)
	g.apiKeyRoute(admin, http.MethodPost, "/name-embeddings/rebuild", apiKeyManageTenantSettings(apiKeyFullAccess()), g.Admin(), h.RebuildNameEmbeddings)
	team.POST("/facts/:fact_id/correction", g.Contributor(), h.CorrectFact)
	team.PATCH("/entities/:entity_id/description", g.Contributor(), h.UpdateEntityDescription)
	team.POST("/entities/merge", g.Contributor(), h.MergeEntities)
	team.POST("/merge-suggestions/:suggestion_id/accept", g.Contributor(), h.AcceptMergeSuggestion)
	team.POST("/merge-suggestions/:suggestion_id/reject", g.Contributor(), h.RejectMergeSuggestion)
	group := g.apiKeyGroup(r.Group("/knowledge-bases/:id/context"), apiKeyIngest(apiKeyFullAccess()))
	read := group.With(apiKeyRetrieve(apiKeyFullAccess()))
	read.POST("/query", g.Viewer(), g.KBAccessRead("id"), h.Query)
	read.GET("/entities", g.Viewer(), g.KBAccessRead("id"), h.Entities)
	read.GET("/events", g.Viewer(), g.KBAccessRead("id"), h.Events)
	read.GET("/evidence", g.Viewer(), g.KBAccessRead("id"), h.Evidence)
	read.GET("/evidence/:evidence_id", g.Viewer(), g.KBAccessRead("id"), h.GetEvidence)
	read.GET("/focus", g.Viewer(), g.KBAccessRead("id"), h.FocusScope)
	group.POST("/facts/:fact_id/correction", g.Contributor(), g.KBAccessWrite("id"), h.CorrectFact)
	group.POST("/entities/merge", g.Contributor(), g.KBAccessWrite("id"), h.MergeEntities)
	group.POST("/rebuild", g.Contributor(), g.KBAccessWrite("id"), h.Rebuild)
}
