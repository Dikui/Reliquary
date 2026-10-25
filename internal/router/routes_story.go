package router

import (
	"reliquary/internal/handler"

	"github.com/gin-gonic/gin"
)

// RegisterStoryRoutes mounts story reads and explicit grouping corrections.
func RegisterStoryRoutes(r *gin.RouterGroup, h *handler.StoryHandler) {
	if h == nil {
		return
	}
	r.GET("/entries/:id/grouping-decisions", h.Decisions)
	r.GET("/stories", h.List)
	r.GET("/stories/:story_id", h.Get)
	r.POST("/stories/:story_id/merge", h.MergeStories)
	r.GET("/facts/:fact_id", h.Fact)
	r.POST("/facts/:fact_id/move", h.Move)
	r.POST("/facts/:fact_id/split", h.Split)
	r.POST("/facts/:fact_id/merge", h.MergeFacts)
}
