package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
)

func (h *EntityHandler) ShowcaseShelf(selector string) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, err := h.service.ShowcaseShelf(c.Request.Context(), selector)
		switch {
		case errors.Is(err, service.ErrShowcaseLibraryNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": service.ErrShowcaseLibraryNotFound.Error()})
		case errors.Is(err, service.ErrShowcaseLibraryAmbiguous):
			c.JSON(http.StatusConflict, gin.H{"error": service.ErrShowcaseLibraryAmbiguous.Error()})
		case err != nil:
			c.Error(err)
		default:
			c.JSON(http.StatusOK, result)
		}
	}
}
