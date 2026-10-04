package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
)

type KnowledgeStrategyHandler struct {
	service *service.KnowledgeStrategyService
}

func NewKnowledgeStrategyHandler(s *service.KnowledgeStrategyService) *KnowledgeStrategyHandler {
	return &KnowledgeStrategyHandler{service: s}
}

func (h *KnowledgeStrategyHandler) List(c *gin.Context) {
	items, err := h.service.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

func (h *KnowledgeStrategyHandler) Propose(c *gin.Context) {
	var input struct {
		Instruction string   `json:"instruction" binding:"required"`
		SampleIDs   []string `json:"sample_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("instruction and sample_ids required"))
		return
	}
	item, err := h.service.Propose(c.Request.Context(), c.Param("id"), input.Instruction, input.SampleIDs)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *KnowledgeStrategyHandler) Activate(c *gin.Context) {
	item, err := h.service.Activate(c.Request.Context(), c.Param("id"), c.Param("version_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *KnowledgeStrategyHandler) Evaluate(c *gin.Context) {
	var input struct {
		Samples []service.KnowledgeStrategyEvaluationSample `json:"samples" binding:"required,min=1,max=20,dive"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("one to twenty labeled samples required"))
		return
	}
	result, err := h.service.Evaluate(c.Request.Context(), c.Param("id"), c.Param("version_id"), input.Samples)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *KnowledgeStrategyHandler) Reprocess(c *gin.Context) {
	var input struct {
		MessageIDs []string `json:"message_ids"`
		TagID      string   `json:"tag_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("invalid reprocessing scope"))
		return
	}
	result, err := h.service.Reprocess(c.Request.Context(), c.Param("id"), input.MessageIDs, input.TagID)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
