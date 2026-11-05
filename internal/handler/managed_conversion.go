package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *EntityHandler) EntityConversionPreview(c *gin.Context) {
	result, err := h.service.EntityConversionPreview(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
