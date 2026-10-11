package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/handler"
)

func RegisterEmbeddingRebuildRoutes(r *gin.RouterGroup, h *handler.EmbeddingRebuildHandler) {
	if h == nil {
		return
	}
	r.GET("/embeddings/rebuild", h.Status)
	r.POST("/embeddings/rebuild", h.Action)
}
