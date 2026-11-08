package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/types"
)

func (h *EntityHandler) ExportShelf(c *gin.Context) {
	var input types.ShelfExportRequest
	if err := decodeLibraryRequest(c, &input); err != nil {
		c.Error(err)
		return
	}
	data, err := h.service.ExportShelf(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="product-shelf.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}
