package router

import (
	"reliquary/internal/handler"

	"github.com/gin-gonic/gin"
)

// RegisterStoryRoutes mounts story lists and detail reads.
func RegisterStoryRoutes(r *gin.RouterGroup, h *handler.StoryHandler) {
	if h == nil {
		return
	}
	r.GET("/entries/:id/grouping-decisions", h.Decisions)
	r.GET("/stories", h.List)
	r.GET("/stories/:story_id", h.Get)
}
