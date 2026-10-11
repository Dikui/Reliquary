package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	filesvc "reliquary/internal/application/service/file"
	"reliquary/internal/config"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

func TestPresignedPreviewScopesResourcesBeforeIssuingGrants(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baseDir := t.TempDir()
	t.Setenv("LOCAL_STORAGE_BASE_DIR", baseDir)
	t.Setenv("APP_EXTERNAL_URL", "https://example.test")
	t.Setenv("SYSTEM_AES_KEY", "reliquary-test-aes-key-32bytes!!!")
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.AutoMigrate(&types.StoredResource{}, &types.ResourceAccessGrant{}))
	space := &types.Space{ID: 1}
	catalog := service.NewResourceCatalog(repository.NewResourceRepository(db))
	// Registered resources use catalog ownership, even when the physical path has no space segment.
	physical := writeTestFile(t, baseDir, "archive/synthetic.txt", "synthetic-resource")
	reference, err := catalog.Register(t.Context(), 1, physical, interfaces.ResourceRegistration{OriginalName: "synthetic.txt"})
	require.NoError(t, err)
	otherReference, err := catalog.Register(t.Context(), 2, "local://2/other.txt", interfaces.ResourceRegistration{})
	require.NoError(t, err)
	cfg := &config.Config{}
	preview := gin.New()
	preview.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.SpaceInfoContextKey, space)
		ctx = context.WithValue(ctx, types.UserIDContextKey, "personal-user")
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	servePresignedPreview(preview, cfg, catalog)
	allowed := httptest.NewRecorder()
	preview.ServeHTTP(allowed, httptest.NewRequest(http.MethodGet, "/api/v1/files/presigned-preview?file_path="+url.QueryEscape(reference), nil))
	require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
	var result struct {
		URL string `json:"url"`
	}
	require.NoError(t, json.Unmarshal(allowed.Body.Bytes(), &result))
	grantURL, err := url.Parse(result.URL)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(grantURL.Path, "/r/"))
	public := gin.New()
	serveResourceGrants(public, catalog, &stubSpaceService{get: func(_ context.Context, id uint64) (*types.Space, error) {
		require.Equal(t, space.ID, id)
		return space, nil
	}}, filesvc.NewLocalFileService(baseDir, "https://example.test"))
	download := httptest.NewRecorder()
	public.ServeHTTP(download, httptest.NewRequest(http.MethodGet, grantURL.RequestURI(), nil))
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	require.Equal(t, "synthetic-resource", download.Body.String())
	var grantsBefore int64
	require.NoError(t, db.Model(&types.ResourceAccessGrant{}).Count(&grantsBefore).Error)
	for _, path := range []string{otherReference, "local://2/other.txt"} {
		denied := httptest.NewRecorder()
		preview.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/api/v1/files/presigned-preview?file_path="+url.QueryEscape(path), nil))
		require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	}
	var grantsAfter int64
	require.NoError(t, db.Model(&types.ResourceAccessGrant{}).Count(&grantsAfter).Error)
	require.Equal(t, grantsBefore, grantsAfter, "rejected previews must not issue access grants")
}

// stubSpaceService is a minimal stub of interfaces.SpaceService that only
// implements GetSpaceByID. Every other method panics — those code paths
// must not be exercised by presigned routes; the panic surfaces test bugs
// instead of silently returning zero values.
type stubSpaceService struct {
	get func(ctx context.Context, id uint64) (*types.Space, error)
}

var _ interfaces.SpaceService = (*stubSpaceService)(nil)

func (s *stubSpaceService) GetSpaceByID(ctx context.Context, id uint64) (*types.Space, error) {
	if s.get == nil {
		return nil, os.ErrNotExist
	}
	return s.get(ctx, id)
}

func (s *stubSpaceService) CreateSpace(context.Context, *types.Space) (*types.Space, error) {
	panic("unexpected")
}

func (s *stubSpaceService) GetSpacesByIDs(context.Context, []uint64) (map[uint64]*types.Space, error) {
	panic("unexpected")
}
func (s *stubSpaceService) ListSpaces(context.Context) ([]*types.Space, error) {
	panic("unexpected")
}

func (s *stubSpaceService) UpdateSpace(context.Context, *types.Space) (*types.Space, error) {
	panic("unexpected")
}
func (s *stubSpaceService) DeleteSpace(context.Context, uint64) error { panic("unexpected") }
func (s *stubSpaceService) ListAllSpaces(context.Context) ([]*types.Space, error) {
	panic("unexpected")
}

func (s *stubSpaceService) SearchSpaces(context.Context, string, uint64, int, int) ([]*types.Space, int64, error) {
	panic("unexpected")
}

func (s *stubSpaceService) GetSpaceByIDForUser(context.Context, uint64, string) (*types.Space, error) {
	panic("unexpected")
}

// writeTestFile creates baseDir/<relPath> with the given content and returns
// the matching `local://<relPath>` storage path.
func writeTestFile(t *testing.T, baseDir, relPath, content string) string {
	t.Helper()
	full := filepath.Join(baseDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return "local://" + relPath
}

func TestPresignedFile_HEAD_Returns200WithoutBody(t *testing.T) {
	engine, baseDir, signURL := setupPresignedTestServer(t)
	storagePath := writeTestFile(t, baseDir, "1/img.png", "PNG-BYTES")

	req := httptest.NewRequest(http.MethodHead, signURL(storagePath, 1, time.Hour), nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d (body=%q)", got, want, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD response body should be empty, got %d bytes", w.Body.Len())
	}
}

func TestPresignedFile_MissingFile_404(t *testing.T) {
	engine, _, signURL := setupPresignedTestServer(t)

	// A legitimately signed URL pointing at a file that does not exist.
	req := httptest.NewRequest(http.MethodGet, signURL("local://1/nope.png", 1, time.Hour), nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestPresignedFile_GET_ReturnsContent(t *testing.T) {
	engine, baseDir, signURL := setupPresignedTestServer(t)
	storagePath := writeTestFile(t, baseDir, "1/img.png", "PNG-BYTES")

	req := httptest.NewRequest(http.MethodGet, signURL(storagePath, 1, time.Hour), nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got := w.Body.String(); got != "PNG-BYTES" {
		t.Fatalf("body = %q, want %q", got, "PNG-BYTES")
	}
}

// setupPresignedTestServer wires presignedFileHandler with a real local file
// service rooted at a temp dir, returning the engine, baseDir, and the
// presigned URL generator helper.
func setupPresignedTestServer(t *testing.T) (engine *gin.Engine, baseDir string, signURL func(filePath string, spaceID uint64, ttl time.Duration) string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("SYSTEM_AES_KEY", "reliquary-test-aes-key-32bytes!!!")

	baseDir = t.TempDir()

	space := &types.Space{
		ID: 1,
	}
	stubTS := &stubSpaceService{
		get: func(_ context.Context, id uint64) (*types.Space, error) {
			if id == space.ID {
				return space, nil
			}
			return nil, os.ErrNotExist
		},
	}

	engine = gin.New()
	handler := presignedFileHandler(stubTS, baseDir)
	engine.GET("/api/v1/files/presigned", handler)
	engine.HEAD("/api/v1/files/presigned", handler)

	signURL = func(filePath string, spaceID uint64, ttl time.Duration) string {
		signed, err := secutils.SignFileURL("https://reliquary.example.com", filePath, spaceID, ttl)
		if err != nil {
			t.Fatalf("SignFileURL: %v", err)
		}
		// Re-parse so we only keep the query part: the test server is on
		// a different host than the canonical signing baseURL.
		u, _ := url.Parse(signed)
		return "/api/v1/files/presigned?" + u.RawQuery
	}

	return engine, baseDir, signURL
}

func TestPresignedFile_ForcesActiveContentDownload(t *testing.T) {
	engine, baseDir, signURL := setupPresignedTestServer(t)
	storagePath := writeTestFile(t, baseDir, "1/payload.svg", `<svg onload="alert(1)"></svg>`)

	req := httptest.NewRequest(http.MethodGet, signURL(storagePath, 1, time.Hour), nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got := w.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := w.Header().Get("Content-Disposition"); got != "attachment" {
		t.Fatalf("Content-Disposition = %q, want attachment", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestPresignedFile_InvalidSig_403(t *testing.T) {
	engine, _, _ := setupPresignedTestServer(t)

	// Hand-craft a URL with a tampered signature.
	q := url.Values{}
	q.Set("file_path", "local://1/img.png")
	q.Set("space_id", "1")
	q.Set("expires", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
	q.Set("sig", "deadbeefdeadbeef")

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/v1/files/presigned?"+q.Encode(), nil)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusForbidden; got != want {
				t.Fatalf("status = %d, want %d", got, want)
			}
		})
	}
}

func TestPresignedFile_MissingParams_400(t *testing.T) {
	engine, _, _ := setupPresignedTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files/presigned?file_path=local%3A%2F%2F1%2Fimg.png", nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if !strings.Contains(w.Body.String(), "missing required parameters") {
		t.Fatalf("body = %q, want missing-params error", w.Body.String())
	}
}
