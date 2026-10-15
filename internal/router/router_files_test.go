package router

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var _ interfaces.FileService = (*stubFileService)(nil)

type stubFileService struct {
	getFile func(ctx context.Context, filePath string) (io.ReadCloser, error)
}

type stubResourceCatalog struct {
	resource *types.StoredResource
}

func (s *stubResourceCatalog) Register(
	context.Context,
	uint64,
	string,
	interfaces.ResourceRegistration,
) (string, error) {
	panic("unexpected Register")
}

func (s *stubResourceCatalog) Resolve(context.Context, string) (*types.StoredResource, error) {
	return s.resource, nil
}

func (s *stubResourceCatalog) ResolvePath(_ context.Context, value string) (string, *types.StoredResource, error) {
	if _, ok := types.ParseResourcePath(value); ok && s.resource != nil {
		return s.resource.PhysicalPath, s.resource, nil
	}
	return value, nil, nil
}

func (s *stubResourceCatalog) Bind(context.Context, string, string, string, string) error {
	panic("unexpected Bind")
}

func (s *stubResourceCatalog) Release(context.Context, string, string, string) (int64, error) {
	panic("unexpected Release")
}

func (s *stubResourceCatalog) MarkDeleted(context.Context, string) error {
	panic("unexpected MarkDeleted")
}

func (s *stubResourceCatalog) CreateAccessGrant(context.Context, string, time.Duration) (string, error) {
	panic("unexpected CreateAccessGrant")
}

func (s *stubResourceCatalog) ResolveAccessGrant(context.Context, string) (*types.StoredResource, error) {
	return s.resource, nil
}

func (s *stubFileService) CheckConnectivity(ctx context.Context) error {
	return nil
}

func (s *stubFileService) SaveFile(ctx context.Context, file *multipart.FileHeader, spaceID uint64, entryID string) (string, error) {
	panic("unexpected call to SaveFile")
}

func (s *stubFileService) SaveBytes(ctx context.Context, data []byte, spaceID uint64, fileName string, temp bool) (string, error) {
	panic("unexpected call to SaveBytes")
}

func (s *stubFileService) GetFile(ctx context.Context, filePath string) (io.ReadCloser, error) {
	if s.getFile == nil {
		panic("unexpected call to GetFile")
	}
	return s.getFile(ctx, filePath)
}

func (s *stubFileService) GetFileURL(ctx context.Context, filePath string) (string, error) {
	panic("unexpected call to GetFileURL")
}

func (s *stubFileService) DeleteFile(ctx context.Context, filePath string) error {
	panic("unexpected call to DeleteFile")
}

func (s *stubFileService) CopyFile(ctx context.Context, srcPath string, spaceID uint64, entryID string) (string, error) {
	panic("unexpected call to CopyFile")
}

func TestServeFilesFallsBackToGlobalFileService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	var requestedPath string
	serveFiles(engine, &stubFileService{
		getFile: func(ctx context.Context, filePath string) (io.ReadCloser, error) {
			requestedPath = filePath
			return io.NopCloser(strings.NewReader("fallback-body")), nil
		},
	})

	filePath := "local://42/docs/example.txt"
	req := httptest.NewRequest(http.MethodGet, "/files?file_path="+url.QueryEscape(filePath), nil)
	req = req.WithContext(context.WithValue(req.Context(), types.SpaceInfoContextKey, &types.Space{ID: 42}))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if requestedPath != filePath {
		t.Fatalf("requested path = %q, want %q", requestedPath, filePath)
	}
	if body := recorder.Body.String(); body != "fallback-body" {
		t.Fatalf("body = %q, want %q", body, "fallback-body")
	}
}

func TestServeFilesResolvesShortResourceReference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	const physical = "local://42/exports/a.png"

	engine := gin.New()
	var requestedPath string
	serveFilesWithResources(engine, &stubFileService{getFile: func(_ context.Context, path string) (io.ReadCloser, error) {
		requestedPath = path
		return io.NopCloser(strings.NewReader("image")), nil
	}}, &stubResourceCatalog{resource: &types.StoredResource{SpaceID: 42, PhysicalPath: physical}})

	req := httptest.NewRequest(http.MethodGet, "/files?file_path="+url.QueryEscape(ref), nil)
	req = req.WithContext(context.WithValue(req.Context(), types.SpaceInfoContextKey, &types.Space{ID: 42}))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if requestedPath != physical {
		t.Fatalf("requested path = %q, want %q", requestedPath, physical)
	}
}

func TestServeFilesRejectsCrossSpaceResourceReference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	engine := gin.New()
	serveFilesWithResources(engine, &stubFileService{getFile: func(context.Context, string) (io.ReadCloser, error) {
		t.Fatal("GetFile should not be called")
		return nil, nil
	}}, &stubResourceCatalog{resource: &types.StoredResource{SpaceID: 7, PhysicalPath: "local://7/exports/a.png"}})

	req := httptest.NewRequest(http.MethodGet, "/files?file_path="+url.QueryEscape(ref), nil)
	req = req.WithContext(context.WithValue(req.Context(), types.SpaceInfoContextKey, &types.Space{ID: 42}))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestResourceGrantServesShortPublicURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	physical := "local://42/exports/a.png"
	engine := gin.New()
	serveResourceGrants(
		engine,
		&stubResourceCatalog{resource: &types.StoredResource{
			ID:           "resource-1",
			SpaceID:      42,
			PhysicalPath: physical,
			OriginalName: "a.png",
			MimeType:     "image/png",
		}},
		&stubSpaceService{get: func(_ context.Context, id uint64) (*types.Space, error) {
			return &types.Space{ID: id}, nil
		}},
		&stubFileService{getFile: func(_ context.Context, path string) (io.ReadCloser, error) {
			if path != physical {
				t.Fatalf("path = %q, want %q", path, physical)
			}
			return io.NopCloser(strings.NewReader("image")), nil
		}},
	)

	req := httptest.NewRequest(http.MethodGet, "/r/GrantTokenAbCdEfGhIjKlM", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "image" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

func TestServeFilesRejectsCrossSpacePath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	serveFiles(engine, &stubFileService{
		getFile: func(ctx context.Context, filePath string) (io.ReadCloser, error) {
			t.Fatalf("GetFile should not be called for cross-space path, got %q", filePath)
			return nil, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/files?file_path="+url.QueryEscape("local://7/knowledge/secret.pdf"), nil)
	req = req.WithContext(context.WithValue(req.Context(), types.SpaceInfoContextKey, &types.Space{ID: 42}))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestServeFilesRejectsPathWithoutSpaceSegment(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	serveFiles(engine, &stubFileService{
		getFile: func(ctx context.Context, filePath string) (io.ReadCloser, error) {
			t.Fatalf("GetFile should not be called without space segment, got %q", filePath)
			return nil, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/files?file_path="+url.QueryEscape("local://docs/example.txt"), nil)
	req = req.WithContext(context.WithValue(req.Context(), types.SpaceInfoContextKey, &types.Space{ID: 42}))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func newLibraryScopedFilesTestEngine(
	effectiveSpaceID uint64,
	spaceSvc interfaces.SpaceService,
	global interfaces.FileService,
) *gin.Engine {
	engine := gin.New()
	engine.GET("/libraries/:id/files",
		func(c *gin.Context) {
			ctx := context.WithValue(c.Request.Context(), types.SpaceIDContextKey, effectiveSpaceID)
			c.Request = c.Request.WithContext(ctx)
			c.Next()
		},
		newLibraryScopedFileServeHandler(spaceSvc, global),
	)
	return engine
}

func TestLibraryScopedFilesServesOwnerSpacePath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const ownerSpaceID = uint64(10008)
	var requestedPath string
	engine := newLibraryScopedFilesTestEngine(
		ownerSpaceID,
		&stubSpaceService{get: func(_ context.Context, id uint64) (*types.Space, error) {
			return &types.Space{ID: id}, nil
		}},
		&stubFileService{getFile: func(_ context.Context, filePath string) (io.ReadCloser, error) {
			requestedPath = filePath
			return io.NopCloser(strings.NewReader("shared-body")), nil
		}},
	)

	filePath := "local://10008/exports/img.jpg"
	req := httptest.NewRequest(http.MethodGet, "/libraries/library-1/files?file_path="+url.QueryEscape(filePath), nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, recorder.Body.String())
	}
	if requestedPath != filePath {
		t.Fatalf("requested path = %q, want %q", requestedPath, filePath)
	}
	if body := recorder.Body.String(); body != "shared-body" {
		t.Fatalf("body = %q, want %q", body, "shared-body")
	}
}

// The path must belong to the effective (owner) space. A path pointing at a
// different space than the resolved Library owner is still rejected, so the guard
// cannot be used to reach arbitrary spaces' files.
func TestLibraryScopedFilesRejectsPathNotOwnedByLibrarySpace(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const ownerSpaceID = uint64(10008)
	engine := newLibraryScopedFilesTestEngine(
		ownerSpaceID,
		&stubSpaceService{get: func(_ context.Context, id uint64) (*types.Space, error) {
			t.Fatalf("GetSpaceByID should not be called for mismatched path, got %d", id)
			return nil, nil
		}},
		&stubFileService{getFile: func(_ context.Context, filePath string) (io.ReadCloser, error) {
			t.Fatalf("GetFile should not be called for mismatched path, got %q", filePath)
			return nil, nil
		}},
	)

	req := httptest.NewRequest(http.MethodGet,
		"/libraries/library-1/files?file_path="+url.QueryEscape("local://9999/exports/other.jpg"), nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

// Library-scoped proxy is for embedded exports/ images only; raw entry uploads
// must use /entries/:id/download even when the space matches.
func TestLibraryScopedFilesRejectsNonExportsPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const ownerSpaceID = uint64(10008)
	engine := newLibraryScopedFilesTestEngine(
		ownerSpaceID,
		&stubSpaceService{get: func(_ context.Context, id uint64) (*types.Space, error) {
			t.Fatalf("GetSpaceByID should not be called for non-exports path, got %d", id)
			return nil, nil
		}},
		&stubFileService{getFile: func(_ context.Context, filePath string) (io.ReadCloser, error) {
			t.Fatalf("GetFile should not be called for non-exports path, got %q", filePath)
			return nil, nil
		}},
	)

	req := httptest.NewRequest(http.MethodGet,
		"/libraries/library-1/files?file_path="+url.QueryEscape("local://10008/entry-id/123.pdf"), nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestLibraryScopedFilesRequiresFilePath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := newLibraryScopedFilesTestEngine(
		10008,
		&stubSpaceService{},
		&stubFileService{},
	)

	req := httptest.NewRequest(http.MethodGet, "/libraries/library-1/files", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestServeFilesForcesActiveContentDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	serveFiles(engine, &stubFileService{
		getFile: func(_ context.Context, _ string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(`<svg onload="alert(1)"></svg>`)), nil
		},
	})

	filePath := "local://42/docs/payload.svg"
	req := httptest.NewRequest(http.MethodGet, "/files?file_path="+url.QueryEscape(filePath), nil)
	req = req.WithContext(context.WithValue(req.Context(), types.SpaceInfoContextKey, &types.Space{ID: 42}))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != "attachment" {
		t.Fatalf("Content-Disposition = %q, want attachment", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}
