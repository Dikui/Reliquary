package handler

import (
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *EntityHandler) ShelfDocumentVersions(c *gin.Context) {
	rows, err := h.service.ShelfDocumentVersions(c.Request.Context(), c.Param("entity_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

func (h *EntityHandler) DownloadShelfDocumentVersion(c *gin.Context) {
	row, reader, err := h.service.OpenShelfDocumentVersion(c.Request.Context(), c.Param("entity_id"), c.Param("version_id"))
	if err != nil {
		c.Error(err)
		return
	}
	defer reader.Close()
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": row.Name}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, row.Size, "application/octet-stream", reader, nil)
}
