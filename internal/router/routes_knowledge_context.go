package router

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"reliquary/internal/handler"
)

func RegisterKnowledgeContextRoutes(r *gin.RouterGroup, h *handler.KnowledgeContextHandler, g *accessGuards) {
	if h == nil {
		return
	}
	team := r.Group("/knowledge-context")
	teamRead := team
	teamRead.POST("/query", h.Query)
	teamRead.GET("/entities", h.Entities)
	teamRead.GET("/entities/:entity_id", h.EntityDetail)
	teamRead.GET("/events", h.Events)
	teamRead.GET("/merge-suggestions", h.MergeSuggestions)
	teamRead.GET("/evidence", h.Evidence)
	teamRead.GET("/evidence/:evidence_id", h.GetEvidence)
	teamRead.GET("/focus", h.FocusScope)
	team.PUT("/entities/:entity_id/star", h.SetEntityStar)
	team.DELETE("/entities/:entity_id/star", h.SetEntityStar)
	team.PUT("/topics/:tag_id/star", h.SetTopicStar)
	team.DELETE("/topics/:tag_id/star", h.SetTopicStar)
	admin := r.Group("/knowledge-context")
	admin.Handle(http.MethodGet, "/name-embeddings/rebuild", h.NameEmbeddingRebuildStatus)
	admin.Handle(http.MethodPost, "/name-embeddings/rebuild", h.RebuildNameEmbeddings)
	team.POST("/facts/:fact_id/correction", h.CorrectFact)
	team.PATCH("/entities/:entity_id/description", h.UpdateEntityDescription)
	team.POST("/entities/merge", h.MergeEntities)
	team.POST("/merge-suggestions/:suggestion_id/accept", h.AcceptMergeSuggestion)
	team.POST("/merge-suggestions/:suggestion_id/reject", h.RejectMergeSuggestion)
	group := r.Group("/knowledge-bases/:id/context")
	read := group
	read.POST("/query", g.KBAccessRead("id"), h.Query)
	read.GET("/entities", g.KBAccessRead("id"), h.Entities)
	read.GET("/events", g.KBAccessRead("id"), h.Events)
	read.GET("/evidence", g.KBAccessRead("id"), h.Evidence)
	read.GET("/evidence/:evidence_id", g.KBAccessRead("id"), h.GetEvidence)
	read.GET("/focus", g.KBAccessRead("id"), h.FocusScope)
	group.POST("/facts/:fact_id/correction", g.KBAccessWrite("id"), h.CorrectFact)
	group.POST("/entities/merge", g.KBAccessWrite("id"), h.MergeEntities)
	group.POST("/rebuild", g.KBAccessWrite("id"), h.Rebuild)
}
