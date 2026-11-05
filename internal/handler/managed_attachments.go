package handler

import (
	"io"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	apperrors "reliquary/internal/errors"
)

func (h *EntityHandler) UploadEntityAttachment(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxEntityAttachmentBytes+(1<<20))
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid attachment upload (maximum 32 MiB)"))
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.Error(apperrors.NewBadRequestError("file is required"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, service.MaxEntityAttachmentBytes+1))
	if err != nil {
		c.Error(err)
		return
	}
	row, err := h.service.UploadEntityAttachment(c.Request.Context(), c.Param("id"), header.Filename, data)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": row})
}

func (h *EntityHandler) EntityAttachment(c *gin.Context) {
	row, err := h.service.EntityAttachment(c.Request.Context(), c.Param("attachment_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}

func (h *EntityHandler) DownloadEntityAttachment(c *gin.Context) {
	row, reader, err := h.service.OpenEntityAttachment(c.Request.Context(), c.Param("attachment_id"))
	if err != nil {
		c.Error(err)
		return
	}
	defer reader.Close()
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": row.Name}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, row.Size, "application/octet-stream", reader, nil)
}
