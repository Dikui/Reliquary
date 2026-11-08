package handler

import (
	"mime"
	"net/http"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"strconv"

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

func (h *EntityHandler) ShelfDocuments(c *gin.Context) {
	limit, e1 := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, e2 := strconv.Atoi(c.DefaultQuery("offset", "0"))
	history, e3 := strconv.ParseBool(c.DefaultQuery("include_history", "false"))
	if e1 != nil || e2 != nil || e3 != nil {
		c.Error(apperrors.NewBadRequestError("invalid document filters"))
		return
	}
	result, err := h.service.ShelfDocuments(c.Request.Context(), c.Param("id"), types.ShelfDocumentListRequest{Query: c.Query("q"), Group: c.Query("group"), OwnerKind: c.Query("owner_kind"), ParseStatus: c.Query("parse_status"), IncludeHistory: history, Limit: limit, Offset: offset})
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
