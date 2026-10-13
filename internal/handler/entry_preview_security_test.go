package handler

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/require"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type previewEntryServiceStub struct {
	interfaces.EntryService
	filename string
	content  []byte
}

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool {
	ch := make(chan bool, 1)
	return ch
}

func (s *previewEntryServiceStub) GetEntryByIDOnly(context.Context, string) (*types.Entry, error) {
	return &types.Entry{ID: "k1", SpaceID: 42, LibraryID: "library1"}, nil
}

func (s *previewEntryServiceStub) GetEntryFile(context.Context, string) (io.ReadCloser, string, error) {
	if s.content != nil {
		return io.NopCloser(bytes.NewReader(s.content)), s.filename, nil
	}
	return io.NopCloser(strings.NewReader(`<html><script>alert(1)</script></html>`)), s.filename, nil
}

func TestPreviewEntryFileForcesActiveContentDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Use(func(c *gin.Context) {
		c.Set(types.SpaceIDContextKey.String(), uint64(42))
		c.Next()
	})

	h := &EntryHandler{kgService: &previewEntryServiceStub{filename: "payload.html"}}
	router.GET("/entries/:id/preview", h.PreviewEntryFile)

	req := httptest.NewRequest(http.MethodGet, "/entries/k1/preview", nil)
	w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder()}
	router.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Fatalf("Content-Disposition = %q, want attachment", got)
	}
}

func TestPreviewRangeCleansOnlyTemporaryCopy(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.SpaceIDContextKey.String(), uint64(42))
		c.Next()
	})
	h := &EntryHandler{kgService: &previewEntryServiceStub{filename: "original.md"}}
	router.GET("/entries/:id/preview", h.PreviewEntryFile)
	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "/entries/k1/preview", nil)
		req.Header.Set("Range", "bytes=0-5")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusPartialContent || w.Body.String() != "<html>" {
			t.Fatalf("original must remain readable after preview cleanup: status=%d body=%q", w.Code, w.Body.String())
		}
		files, err := os.ReadDir(tempDir)
		if err != nil || len(files) != 0 {
			t.Fatalf("temporary preview was retained: files=%v err=%v", files, err)
		}
	}
}

func TestEntryOriginalDownloadAndPreviewBytesAndMIME(t *testing.T) {
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	for _, sample := range []struct {
		name, contentType string
		data              []byte
	}{
		{"image.png", "image/png", imageBytes.Bytes()},
		{"source.md", "text/markdown; charset=utf-8", []byte("# Original\r\n![image](original.png)\r\n")},
	} {
		t.Run(sample.name, func(t *testing.T) {
			router := gin.New()
			router.Use(middleware.ErrorHandler(), func(c *gin.Context) { c.Set(types.SpaceIDContextKey.String(), uint64(42)); c.Next() })
			h := &EntryHandler{kgService: &previewEntryServiceStub{filename: sample.name, content: sample.data}}
			router.GET("/entries/:id/download", h.DownloadEntryFile)
			router.GET("/entries/:id/preview", h.PreviewEntryFile)
			for _, action := range []string{"download", "preview"} {
				w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder()}
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/entries/k1/"+action, nil))
				require.Equal(t, http.StatusOK, w.Code)
				require.Equal(t, sample.data, w.Body.Bytes())
				require.Equal(t, sample.contentType, w.Header().Get("Content-Type"))
				require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
			}
		})
	}
}
