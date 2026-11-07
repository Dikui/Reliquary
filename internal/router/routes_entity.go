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
	r.POST("/libraries/:id/shelf/changesets", h.SubmitShelfChangeset)
	r.GET("/libraries/:id/shelf/changesets", h.ShelfChangesets)
	r.GET("/libraries/:id/shelf/changesets/:changeset_id", h.ShelfChangeset)
	r.PATCH("/libraries/:id/shelf/changesets/:changeset_id", h.ReviewShelfChangeset)
	r.POST("/libraries/:id/shelf/changesets/:changeset_id/confirm", h.ReviewShelfChangeset)
	r.GET("/libraries/:id/entity-conversion", h.EntityConversionPreview)
	r.POST("/libraries/:id/entity-attachments", h.UploadEntityAttachment)
	r.GET("/entity-attachments/:attachment_id", h.EntityAttachment)
	r.DELETE("/entity-attachments/:attachment_id", h.DeleteEntityAttachment)
	r.GET("/entity-attachments/:attachment_id/download", h.DownloadEntityAttachment)
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
	group.DELETE("/:entity_id", h.DeleteManagedEntity)
	group.GET("/:entity_id/history", h.ManagedEntityHistory)
	group.GET("/:entity_id/attributes", h.EntityAttributes)
	group.GET("/:entity_id/attribute-history", h.EntityAttributeHistory)
	group.GET("/:entity_id/reorganizations", h.EntityReorganizations)
	group.POST("/:entity_id/merge", h.MergeManagedEntities)
	group.POST("/:entity_id/split", h.SplitManagedEntity)
	group.GET("/:entity_id/relations", h.Relations)
	group.GET("/:entity_id/merge-records", h.MergeRecords)
	group.PATCH("/:entity_id/description", h.UpdateEntityDescription)
	group.PATCH("/:entity_id/name", h.RenameEntity)
}
