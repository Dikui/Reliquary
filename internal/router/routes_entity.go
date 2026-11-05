package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/handler"
)

func RegisterEntityRoutes(r *gin.RouterGroup, h *handler.EntityHandler) {
	if h == nil {
		return
	}
	r.DELETE("/relations/:relation_id", h.DeleteRelation)
	r.PATCH("/relations/:relation_id", h.SaveManagedRelation)
	r.POST("/libraries/:id/entities", h.CreateManagedEntity)
	r.POST("/libraries/:id/relations", h.SaveManagedRelation)
	r.GET("/entries/:id/entity-links", h.EntryEntityLinks)
	r.PUT("/entries/:id/entity-links/:entity_id", h.SetEntryEntityLink)
	r.POST("/libraries/:id/relation-types/merge", h.MergeRelationTypes)
	group := r.Group("/entities")
	group.GET("", h.Entities)
	group.GET("/merge-suggestions", h.MergeSuggestions)
	group.POST("/merge-suggestions/:suggestion_id/accept", h.AcceptMergeSuggestion)
	group.POST("/merge-suggestions/:suggestion_id/reject", h.RejectMergeSuggestion)
	group.GET("/name-embeddings/rebuild", h.NameEmbeddingRebuildStatus)
	group.POST("/name-embeddings/rebuild", h.RebuildNameEmbeddings)
	group.POST("/merge", h.MergeEntities)
	group.GET("/:entity_id", h.EntityDetail)
	group.PATCH("/:entity_id", h.UpdateManagedEntity)
	group.GET("/:entity_id/history", h.ManagedEntityHistory)
	group.GET("/:entity_id/relations", h.Relations)
	group.GET("/:entity_id/merge-records", h.MergeRecords)
	group.PATCH("/:entity_id/description", h.UpdateEntityDescription)
	group.PATCH("/:entity_id/name", h.RenameEntity)
}
