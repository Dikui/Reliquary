package handler

import (
	"io"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
	apperrors "reliquary/internal/errors"
)

func (h *EntityHandler) UploadEntityAttachment(c *gin.Context) {
	limit, err := h.service.EntityAttachmentLimit(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit+(1<<20))
	defer func() {
		if c.Request.MultipartForm != nil {
			_ = c.Request.MultipartForm.RemoveAll()
		}
	}()
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid attachment upload or configured size limit exceeded"))
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.Error(apperrors.NewBadRequestError("file is required"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
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

func (h *EntityHandler) DeleteEntityAttachment(c *gin.Context) {
	if err := h.service.DeleteEntityAttachment(c.Request.Context(), c.Param("attachment_id")); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
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
