package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"reliquary/internal/application/service"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type KnowledgeDomainHandler struct {
	service *service.KnowledgeDomainService
}

func NewKnowledgeDomainHandler(s *service.KnowledgeDomainService) *KnowledgeDomainHandler {
	return &KnowledgeDomainHandler{service: s}
}

// List godoc
func (h *KnowledgeDomainHandler) List(c *gin.Context) {
	items, err := h.service.List(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// Get godoc
func (h *KnowledgeDomainHandler) Get(c *gin.Context) {
	item, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// Create godoc
func (h *KnowledgeDomainHandler) Create(c *gin.Context) {
	var input types.KnowledgeDomain
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid domain template"))
		return
	}
	item, err := h.service.Create(c.Request.Context(), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": item})
}

// Update godoc
func (h *KnowledgeDomainHandler) Update(c *gin.Context) {
	var input types.KnowledgeDomain
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid domain template"))
		return
	}
	item, err := h.service.Update(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// Delete godoc
func (h *KnowledgeDomainHandler) Delete(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// OpenFields godoc
func (h *KnowledgeDomainHandler) OpenFields(c *gin.Context) {
	items, err := h.service.OpenFields(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// MapField godoc
func (h *KnowledgeDomainHandler) MapField(c *gin.Context) {
	var input types.DomainFieldMapping
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid field mapping"))
		return
	}
	item, err := h.service.MapField(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// AddField godoc
func (h *KnowledgeDomainHandler) AddField(c *gin.Context) {
	var input types.DomainField
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid template field"))
		return
	}
	item, err := h.service.AddField(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
