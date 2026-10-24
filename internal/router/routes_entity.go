package router

import (
	"reliquary/internal/handler"

	"github.com/gin-gonic/gin"
)

func RegisterEntityRoutes(r *gin.RouterGroup, h *handler.EntityHandler) {
	if h == nil {
		return
	}
	group := r.Group("/entities")
	group.GET("", h.Entities)
	group.GET("/merge-suggestions", h.MergeSuggestions)
	group.POST("/merge-suggestions/:suggestion_id/accept", h.AcceptMergeSuggestion)
	group.POST("/merge-suggestions/:suggestion_id/reject", h.RejectMergeSuggestion)
	group.GET("/name-embeddings/rebuild", h.NameEmbeddingRebuildStatus)
	group.POST("/name-embeddings/rebuild", h.RebuildNameEmbeddings)
	group.POST("/merge", h.MergeEntities)
	group.GET("/:entity_id", h.EntityDetail)
	group.GET("/:entity_id/merge-records", h.MergeRecords)
	group.PATCH("/:entity_id/description", h.UpdateEntityDescription)
	group.PATCH("/:entity_id/name", h.RenameEntity)
}
