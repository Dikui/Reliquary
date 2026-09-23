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
