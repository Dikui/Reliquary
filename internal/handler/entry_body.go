package handler

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ReadEntryBody serves the stored Markdown used by the intake preview.
func (h *EntryHandler) ReadEntryBody(c *gin.Context) {
	reader, _, err := h.kgService.GetEntryBody(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		c.Error(err)
		return
	}
	c.Header("Content-Type", "text/markdown; charset=utf-8")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, no-store")
	c.Header("ETag", fmt.Sprintf("\"%x\"", sha256.Sum256(data)))
	http.ServeContent(c.Writer, c.Request, "body.md", time.Time{}, bytes.NewReader(data))
}
