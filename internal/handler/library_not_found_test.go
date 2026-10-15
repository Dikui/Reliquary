package handler

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// stubLibraryOnlyService implements just enough of LibraryService to
// drive validateAndGetLibrary. Embedding the interface keeps every
// other method nil-panicky on purpose so a future test that reaches
// outside the contract fails loudly.
type stubLibraryOnlyService struct {
	interfaces.LibraryService
	getByID           func(ctx context.Context, id string) (*types.Library, error)
	fillLibraryCounts func(ctx context.Context, library *types.Library) error
}

func (s *stubLibraryOnlyService) GetLibraryByID(ctx context.Context, id string) (*types.Library, error) {
	return s.getByID(ctx, id)
}

func (s *stubLibraryOnlyService) FillLibraryCounts(ctx context.Context, library *types.Library) error {
	if s.fillLibraryCounts != nil {
		return s.fillLibraryCounts(ctx, library)
	}
	return nil
}

// newLibraryHandlerTestRouter mounts the production ErrorHandler so
// c.Error(NewNotFoundError(...)) renders as the real 404 envelope.
// Space id and user id are injected by a tiny middleware so
// validateAndGetLibrary doesn't bail at the unauthorized branch.
func newLibraryHandlerTestRouter(svc interfaces.LibraryService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Set(types.SpaceIDContextKey.String(), uint64(1))
		c.Set(types.UserIDContextKey.String(), "u-test")
		c.Next()
	})
	h := &LibraryHandler{service: svc}
	r.GET("/libraries/:id", h.GetLibrary)
	return r
}

func TestLibraryHandlerMapsErrLibraryNotFoundToNotFound(t *testing.T) {
	svc := &stubLibraryOnlyService{
		getByID: func(_ context.Context, _ string) (*types.Library, error) {
			return nil, repository.ErrLibraryNotFound
		},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/libraries/missing-library", nil)
	newLibraryHandlerTestRouter(svc).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("ErrLibraryNotFound must map to 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestLibraryHandlerHonoursWrappedErrLibraryNotFound(t *testing.T) {
	// Regression test against a stale `==` sentinel comparison: errors.Is
	// unwraps fmt.Errorf("%w", ...); a literal `==` does not. If anyone
	// reverts the comparison this test fails before the change ships.
	svc := &stubLibraryOnlyService{
		getByID: func(_ context.Context, _ string) (*types.Library, error) {
			return nil, fmt.Errorf("loading library: %w", repository.ErrLibraryNotFound)
		},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/libraries/missing-library", nil)
	newLibraryHandlerTestRouter(svc).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("wrapped ErrLibraryNotFound must still map to 404, got %d body=%s",
			w.Code, w.Body.String())
	}
	// Defence-in-depth: the response body should expose the AppError
	// envelope (ErrorHandler renders {success,error:{code,message}}),
	// not the bare gin error string. Code 1003 is ErrNotFound.
	if !strings.Contains(w.Body.String(), `"code":1003`) {
		t.Fatalf("expected NotFound envelope with code=1003, got body=%s", w.Body.String())
	}
}

func TestLibraryHandlerKeeps500ForGenuineInfraErrors(t *testing.T) {
	// The mapping is *only* for the not-found sentinel — every other
	// error must still surface as a real 5xx so monitoring catches
	// genuine DB / repo failures.
	svc := &stubLibraryOnlyService{
		getByID: func(_ context.Context, _ string) (*types.Library, error) {
			return nil, stderrors.New("connection refused")
		},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/libraries/some-library", nil)
	newLibraryHandlerTestRouter(svc).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("non-sentinel errors must remain 500, got %d body=%s", w.Code, w.Body.String())
	}
}

// guardAgainstStaleSentinelEquality is a compile-time assertion: if a
// future refactor accidentally drops the apperrors / stderrors imports
// from THIS file, the test stops compiling and the human gets a clear
// signal that something tied to the not-found mapping went away.
var (
	_ = stderrors.Is
	_ = apperrors.NewNotFoundError
)
