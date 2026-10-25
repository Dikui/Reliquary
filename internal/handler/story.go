package handler

import (
	"net/http"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"

	"github.com/gin-gonic/gin"
)

// StoryHandler exposes read-only story lists and fact timelines.
type StoryHandler struct{ service *service.StoryService }

// NewStoryHandler constructs the HTTP story handler.
func NewStoryHandler(s *service.StoryService) *StoryHandler { return &StoryHandler{service: s} }

// List returns stories ordered by their latest new fact.
func (h *StoryHandler) List(c *gin.Context) {
	var query struct {
		Limit  int `form:"limit" binding:"min=0,max=100"`
		Offset int `form:"offset" binding:"min=0"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		_ = c.Error(werrors.NewBadRequestError("invalid story pagination"))
		return
	}
	page, err := h.service.ListStories(c.Request.Context(), types.StoryQuery{Limit: query.Limit, Offset: query.Offset})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": page})
}

// Get returns a story with its fact timeline and source quotations.
func (h *StoryHandler) Get(c *gin.Context) {
	detail, err := h.service.StoryDetail(c.Request.Context(), c.Param("story_id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": detail})
}

// Decisions reads candidate scores and the outcome of verification.
func (h *StoryHandler) Decisions(c *gin.Context) {
	decisions, err := h.service.GroupingDecisions(c.Request.Context(), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": decisions})
}
