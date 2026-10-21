package handler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types/interfaces"
)

type markdownBodyStub struct{ interfaces.EntryService }

func (markdownBodyStub) GetEntryBody(context.Context, string) (io.ReadCloser, string, error) {
	return io.NopCloser(strings.NewReader("# Stored Markdown\n\nComplete body.")), "body.md", nil
}
func TestStoredMarkdownBodySupportsRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &EntryHandler{kgService: markdownBodyStub{}}
	r.GET("/entries/:id/body", h.ReadEntryBody)
	req := httptest.NewRequest(http.MethodGet, "/entries/id/body", nil)
	req.Header.Set("Range", "bytes=0-7")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, req)
	require.Equal(t, http.StatusPartialContent, response.Code)
	require.Equal(t, "# Stored", response.Body.String())
	require.Equal(t, "text/markdown; charset=utf-8", response.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	full := httptest.NewRecorder()
	r.ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/entries/id/body", nil))
	require.Equal(t, http.StatusOK, full.Code)
	require.Equal(t, fmt.Sprintf("\"%x\"", sha256.Sum256(full.Body.Bytes())), full.Header().Get("ETag"))
	require.Equal(t, full.Header().Get("ETag"), response.Header().Get("ETag"), "ranges identify the same complete body")
}
