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
// @Summary List knowledge domains
// @Tags Knowledge domains
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Security ApiKeyAuth
// @Router /knowledge-domains [get]
func (h *KnowledgeDomainHandler) List(c *gin.Context) {
	items, err := h.service.List(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// Get godoc
// @Summary Get a knowledge domain
// @Tags Knowledge domains
// @Produce json
// @Param id path string true "Domain ID"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Security ApiKeyAuth
// @Router /knowledge-domains/{id} [get]
func (h *KnowledgeDomainHandler) Get(c *gin.Context) {
	item, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// Create godoc
// @Summary Create a knowledge domain
// @Tags Knowledge domains
// @Accept json
// @Produce json
// @Param request body types.KnowledgeDomain true "Domain template"
// @Success 201 {object} map[string]interface{}
// @Security Bearer
// @Security ApiKeyAuth
// @Router /knowledge-domains [post]
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
// @Summary Update a knowledge domain
// @Tags Knowledge domains
// @Accept json
// @Produce json
// @Param id path string true "Domain ID"
// @Param request body types.KnowledgeDomain true "Domain template"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Security ApiKeyAuth
// @Router /knowledge-domains/{id} [put]
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
// @Summary Delete an unused custom knowledge domain
// @Tags Knowledge domains
// @Produce json
// @Param id path string true "Domain ID"
// @Success 200 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{} "Domain is referenced by active knowledge bases"
// @Security Bearer
// @Security ApiKeyAuth
// @Router /knowledge-domains/{id} [delete]
func (h *KnowledgeDomainHandler) Delete(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// OpenFields godoc
// @Summary List unmapped open fields with counts and sample facts
// @Tags Knowledge domains
// @Produce json
// @Param id path string true "Domain ID"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Security ApiKeyAuth
// @Router /knowledge-domains/{id}/open-fields [get]
func (h *KnowledgeDomainHandler) OpenFields(c *gin.Context) {
	items, err := h.service.OpenFields(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// MapField godoc
// @Summary Map an open field to a template field alias
// @Tags Knowledge domains
// @Accept json
// @Produce json
// @Param id path string true "Domain ID"
// @Param request body types.DomainFieldMapping true "Field mapping"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Security ApiKeyAuth
// @Router /knowledge-domains/{id}/field-mappings [post]
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
// @Summary Add an open field as a template field
// @Tags Knowledge domains
// @Accept json
// @Produce json
// @Param id path string true "Domain ID"
// @Param request body types.DomainField true "New template field"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Security ApiKeyAuth
// @Router /knowledge-domains/{id}/fields [post]
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
