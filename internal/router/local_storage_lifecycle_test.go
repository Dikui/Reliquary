package router

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	filesvc "reliquary/internal/application/service/file"
	"reliquary/internal/storageurl"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// Uploaded entry files and rewritten Markdown images share the local
// filesystem and the resource catalog, including deletion of their bytes.
func TestLocalFileResourceLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SYSTEM_AES_KEY", "reliquary-test-aes-key-32bytes!!!")
	baseDir := t.TempDir()
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.AutoMigrate(&types.StoredResource{}, &types.ResourceBinding{}, &types.ResourceAccessGrant{}))
	catalog := service.NewResourceCatalog(repository.NewResourceRepository(db))
	raw := filesvc.NewLocalFileService(baseDir, "")
	engine := gin.New()
	serveResourceGrants(engine, catalog, &stubSpaceService{get: func(context.Context, uint64) (*types.Space, error) {
		return &types.Space{ID: 1}, nil
	}}, raw)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	t.Setenv("APP_EXTERNAL_URL", server.URL)
	files := filesvc.NewResourceCatalogFileService(raw, catalog)
	ctx := t.Context()
	var pngSample bytes.Buffer
	require.NoError(t, png.Encode(&pngSample, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	for _, sample := range []struct {
		name    string
		content []byte
	}{
		{"entry.md", []byte("# Local storage\n\nEntry file content.")},
		{"figure.png", pngSample.Bytes()},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var upload bytes.Buffer
			writer := multipart.NewWriter(&upload)
			part, err := writer.CreateFormFile("file", sample.name)
			require.NoError(t, err)
			_, err = part.Write(sample.content)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			request := httptest.NewRequest(http.MethodPost, "/", &upload)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			require.NoError(t, request.ParseMultipartForm(1024))
			t.Cleanup(func() { require.NoError(t, request.MultipartForm.RemoveAll()) })
			ref, err := files.SaveFile(ctx, request.MultipartForm.File["file"][0], 1, "entry-1")
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(ref, types.ResourceScheme))
			physical, _, err := catalog.ResolvePath(ctx, ref)
			require.NoError(t, err)
			reader, err := raw.GetFile(ctx, physical)
			require.NoError(t, err)
			stored, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.NoError(t, reader.Close())
			require.Equal(t, sample.content, stored)
			image := "![figure](" + ref + ")"
			rewritten := storageurl.NewRewriter(storageurl.NewFileServiceResolver(&types.Space{ID: 1}, files), "TEST").String(ctx, image)
			require.NotContains(t, rewritten, "resource://")
			link := strings.TrimSuffix(strings.TrimPrefix(rewritten, "![figure]("), ")")
			response, err := server.Client().Get(link)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.Equal(t, sample.content, body)
			if sample.name == "figure.png" {
				require.Equal(t, "image/png", response.Header.Get("Content-Type"))
			}
			require.NoError(t, files.DeleteFile(ctx, ref))
			_, err = raw.GetFile(ctx, physical)
			require.Error(t, err, "deletion removes the local file bytes")
			response, err = server.Client().Get(link)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, http.StatusNotFound, response.StatusCode)
		})
	}
}
