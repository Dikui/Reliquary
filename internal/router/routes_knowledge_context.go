package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/handler"
)

func RegisterKnowledgeContextRoutes(r *gin.RouterGroup, h *handler.KnowledgeContextHandler, g *accessGuards) {
	if h == nil {
		return
	}
	workspace := r.Group("/knowledge-context")
	workspace.POST("/query", h.Query)
	workspace.GET("/entities", h.Entities)
	workspace.GET("/entities/:entity_id", h.EntityDetail)
	workspace.GET("/events", h.Events)
	workspace.GET("/merge-suggestions", h.MergeSuggestions)
	workspace.GET("/evidence", h.Evidence)
	workspace.GET("/evidence/:evidence_id", h.GetEvidence)
	workspace.GET("/focus", h.FocusScope)
	workspace.PUT("/entities/:entity_id/star", h.SetEntityStar)
	workspace.DELETE("/entities/:entity_id/star", h.SetEntityStar)
	workspace.PUT("/topics/:tag_id/star", h.SetTopicStar)
	workspace.DELETE("/topics/:tag_id/star", h.SetTopicStar)
	workspace.GET("/name-embeddings/rebuild", h.NameEmbeddingRebuildStatus)
	workspace.POST("/name-embeddings/rebuild", h.RebuildNameEmbeddings)
	workspace.POST("/facts/:fact_id/correction", h.CorrectFact)
	workspace.PATCH("/entities/:entity_id/description", h.UpdateEntityDescription)
	workspace.POST("/entities/merge", h.MergeEntities)
	workspace.POST("/merge-suggestions/:suggestion_id/accept", h.AcceptMergeSuggestion)
	workspace.POST("/merge-suggestions/:suggestion_id/reject", h.RejectMergeSuggestion)
	group := r.Group("/knowledge-bases/:id/context")
	group.POST("/query", g.KBExists("id"), h.Query)
	group.GET("/entities", g.KBExists("id"), h.Entities)
	group.GET("/events", g.KBExists("id"), h.Events)
	group.GET("/evidence", g.KBExists("id"), h.Evidence)
	group.GET("/evidence/:evidence_id", g.KBExists("id"), h.GetEvidence)
	group.GET("/focus", g.KBExists("id"), h.FocusScope)
	group.POST("/facts/:fact_id/correction", g.KBExists("id"), h.CorrectFact)
	group.POST("/entities/merge", g.KBExists("id"), h.MergeEntities)
	group.POST("/rebuild", g.KBExists("id"), h.Rebuild)
}
