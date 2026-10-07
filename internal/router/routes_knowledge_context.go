package router

import (
	"github.com/gin-gonic/gin"
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
	teamRead.GET("/evidence", g.Viewer(), h.Evidence)
	teamRead.GET("/evidence/:evidence_id", g.Viewer(), h.GetEvidence)
	team.POST("/facts/:fact_id/correction", g.Contributor(), h.CorrectFact)
	team.POST("/entities/merge", g.Contributor(), h.MergeEntities)
	group := g.apiKeyGroup(r.Group("/knowledge-bases/:id/context"), apiKeyIngest(apiKeyFullAccess()))
	read := group.With(apiKeyRetrieve(apiKeyFullAccess()))
	read.POST("/query", g.Viewer(), g.KBAccessRead("id"), h.Query)
	read.GET("/entities", g.Viewer(), g.KBAccessRead("id"), h.Entities)
	read.GET("/events", g.Viewer(), g.KBAccessRead("id"), h.Events)
	read.GET("/evidence", g.Viewer(), g.KBAccessRead("id"), h.Evidence)
	read.GET("/evidence/:evidence_id", g.Viewer(), g.KBAccessRead("id"), h.GetEvidence)
	group.POST("/facts/:fact_id/correction", g.Contributor(), g.KBAccessWrite("id"), h.CorrectFact)
	group.POST("/entities/merge", g.Contributor(), g.KBAccessWrite("id"), h.MergeEntities)
	group.POST("/rebuild", g.Contributor(), g.KBAccessWrite("id"), h.Rebuild)
}
