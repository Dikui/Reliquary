package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type TopicHandler struct{ service *service.TopicService }

func NewTopicHandler(s *service.TopicService) *TopicHandler { return &TopicHandler{service: s} }
func (h *TopicHandler) List(c *gin.Context) {
	items, err := h.service.ListTopics(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}
func (h *TopicHandler) Create(c *gin.Context) {
	var input struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("topic name is required"))
		return
	}
	item, err := h.service.CreateTopic(c.Request.Context(), &types.Topic{Name: input.Name, Description: input.Description})
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": item})
}
func (h *TopicHandler) Update(c *gin.Context) {
	var input struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("invalid topic update"))
		return
	}
	item, err := h.service.UpdateTopic(c.Request.Context(), c.Param("id"), input.Name, input.Description)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
func (h *TopicHandler) Delete(c *gin.Context) {
	if err := h.service.DeleteTopic(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
func (h *TopicHandler) Backfill(c *gin.Context) {
	item, err := h.service.BackfillStatus(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
func (h *TopicHandler) RetryBackfill(c *gin.Context) {
	item, err := h.service.RetryBackfill(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
func (h *TopicHandler) CorrectKnowledgeTopics(c *gin.Context) {
	var input struct {
		IDs *[]string `json:"topic_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.IDs == nil {
		c.Error(werrors.NewBadRequestError("topic_ids must be an array"))
		return
	}
	if err := h.service.SetUserKnowledgeTopics(c.Request.Context(), c.Param("id"), *input.IDs); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
