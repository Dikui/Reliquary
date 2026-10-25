package handler

import (
	"net/http"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"

	"github.com/gin-gonic/gin"
)

// StoryHandler exposes story timelines and manual grouping corrections.
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

// Fact resolves a retained or merged fact ID.
func (h *StoryHandler) Fact(c *gin.Context) {
	item, err := h.service.FactDetail(c.Request.Context(), c.Param("fact_id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// Move corrects the story containing one fact.
func (h *StoryHandler) Move(c *gin.Context) {
	var input struct {
		StoryID string `json:"story_id" binding:"required,max=36"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(werrors.NewBadRequestError("story_id required"))
		return
	}
	item, err := h.service.MoveFact(c.Request.Context(), c.Param("fact_id"), input.StoryID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
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

// Split creates a separate story for one fact.
func (h *StoryHandler) Split(c *gin.Context) {
	var input struct {
		Title string `json:"title" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(werrors.NewBadRequestError("title required"))
		return
	}
	item, err := h.service.SplitFact(c.Request.Context(), c.Param("fact_id"), input.Title)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// MergeFacts consolidates evidence under the selected retained fact.
func (h *StoryHandler) MergeFacts(c *gin.Context) {
	var input struct {
		KeepFactID string `json:"keep_fact_id" binding:"required,max=36"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(werrors.NewBadRequestError("keep_fact_id required"))
		return
	}
	item, err := h.service.MergeFacts(c.Request.Context(), c.Param("fact_id"), input.KeepFactID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// MergeStories retains the chosen story and its combined timeline.
func (h *StoryHandler) MergeStories(c *gin.Context) {
	var input struct {
		KeepStoryID string `json:"keep_story_id" binding:"required,max=36"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(werrors.NewBadRequestError("keep_story_id required"))
		return
	}
	item, err := h.service.MergeStories(c.Request.Context(), c.Param("story_id"), input.KeepStoryID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
