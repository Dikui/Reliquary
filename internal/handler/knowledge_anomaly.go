package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
)

type KnowledgeAnomalyHandler struct {
	service *service.KnowledgeAnomalyService
}

func NewKnowledgeAnomalyHandler(s *service.KnowledgeAnomalyService) *KnowledgeAnomalyHandler {
	return &KnowledgeAnomalyHandler{service: s}
}

func (h *KnowledgeAnomalyHandler) List(c *gin.Context) {
	items, err := h.service.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

func (h *KnowledgeAnomalyHandler) Create(c *gin.Context) {
	var input service.KnowledgeAnomalyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("invalid anomaly report"))
		return
	}
	item, err := h.service.Create(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *KnowledgeAnomalyHandler) Resolve(c *gin.Context) {
	var input struct {
		Action string `json:"action" binding:"required"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("invalid anomaly decision"))
		return
	}
	item, err := h.service.Resolve(c.Request.Context(), c.Param("id"), c.Param("anomaly_id"), input.Action, input.Note)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
