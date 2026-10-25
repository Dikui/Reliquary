package handler

import (
	"net/http"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"

	"github.com/gin-gonic/gin"
)

// DomainHandler exposes shared domains and manual material associations.
type DomainHandler struct{ service *service.DomainService }

// NewDomainHandler constructs the HTTP domain handler.
func NewDomainHandler(s *service.DomainService) *DomainHandler { return &DomainHandler{service: s} }

// List returns all maintained domains.
func (h *DomainHandler) List(c *gin.Context) {
	items, err := h.service.ListDomains(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// Save creates or replaces a domain.
func (h *DomainHandler) Save(c *gin.Context) {
	var input types.DomainInput
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(werrors.NewBadRequestError("domain name and definition required"))
		return
	}
	item, err := h.service.SaveDomain(c.Request.Context(), c.Param("domain_id"), input)
	if err != nil {
		_ = c.Error(err)
		return
	}
	status := http.StatusOK
	if c.Param("domain_id") == "" {
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{"success": true, "data": item})
}

// Delete removes a domain and its associations.
func (h *DomainHandler) Delete(c *gin.Context) {
	if err := h.service.DeleteDomain(c.Request.Context(), c.Param("domain_id")); err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// SetEntryDomains records a manual domain selection.
func (h *DomainHandler) SetEntryDomains(c *gin.Context) {
	var input struct {
		DomainIDs *[]string `json:"domain_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(werrors.NewBadRequestError("domain_ids array required"))
		return
	}
	item, err := h.service.SetEntryDomains(c.Request.Context(), c.Param("id"), *input.DomainIDs)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
