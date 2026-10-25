package handler

import (
	"net/http"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"

	"github.com/gin-gonic/gin"
)

// Graph returns the requested center's direct visible entity relationships.
func (h *EntityHandler) Graph(c *gin.Context) {
	var query types.EntityFactQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		_ = c.Error(werrors.NewBadRequestError("invalid entity activity filter"))
		return
	}
	graph, err := h.service.EntityGraph(c.Request.Context(), c.Param("entity_id"), query)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": graph})
}

// Facts returns a bounded entity timeline with live source quotations.
func (h *EntityHandler) Facts(c *gin.Context) {
	var query types.EntityFactQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		_ = c.Error(werrors.NewBadRequestError("invalid entity activity filter"))
		return
	}
	page, err := h.service.EntityFacts(c.Request.Context(), c.Param("entity_id"), query)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": page})
}
