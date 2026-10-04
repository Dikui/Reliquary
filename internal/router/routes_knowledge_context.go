package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/handler"
)

func RegisterKnowledgeContextRoutes(r *gin.RouterGroup, h *handler.KnowledgeContextHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	group := g.apiKeyGroup(r.Group("/knowledge-bases/:id/context"), apiKeyIngest(apiKeyFullAccess()))
	read := group.With(apiKeyRetrieve(apiKeyFullAccess()))
	read.POST("/query", g.Viewer(), g.KBAccessRead("id"), h.Query)
	read.GET("/entities", g.Viewer(), g.KBAccessRead("id"), h.Entities)
	read.GET("/events", g.Viewer(), g.KBAccessRead("id"), h.Events)
	read.GET("/evidence", g.Viewer(), g.KBAccessRead("id"), h.Evidence)
	read.GET("/evidence/:evidence_id", g.Viewer(), g.KBAccessRead("id"), h.GetEvidence)
	group.POST("/facts/:fact_id/correction", g.OwnedKBOrAdmin(), g.KBAccessWrite("id"), h.CorrectFact)
	group.POST("/entities/merge", g.OwnedKBOrAdmin(), g.KBAccessWrite("id"), h.MergeEntities)
	group.POST("/rebuild", g.OwnedKBOrAdmin(), g.KBAccessWrite("id"), h.Rebuild)
}
