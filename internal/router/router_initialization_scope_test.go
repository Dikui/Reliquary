package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
)

type stubInitializationLibraryLookup struct {
	libraries map[string]*types.Library
}

func (s *stubInitializationLibraryLookup) GetLibraryByID(_ context.Context, id string) (*types.Library, error) {
	if library, ok := s.libraries[id]; ok {
		return library, nil
	}
	return nil, apprepo.ErrLibraryNotFound
}

func newInitializationRouteTestEngine(t *testing.T, callerSpaceID uint64, libraryLookup *stubInitializationLibraryLookup) *gin.Engine {
	return newLibraryRouteTestEngine(t, callerSpaceID, libraryLookup, func(r *gin.RouterGroup, guards *accessGuards) {
		RegisterInitializationRoutes(r, &handler.InitializationHandler{}, guards)
	})
}

func newLibraryRouteTestEngine(
	t *testing.T,
	callerSpaceID uint64,
	libraryLookup *stubInitializationLibraryLookup,
	register func(r *gin.RouterGroup, guards *accessGuards),
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	guards := &accessGuards{
		libraryService: libraryLookup,
	}

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.SpaceIDContextKey, callerSpaceID)
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.SpaceIDContextKey.String(), callerSpaceID)
		c.Next()
	})

	register(r.Group("/api/v1"), guards)
	return r
}

func spaceLibraryLookupFixture() *stubInitializationLibraryLookup {
	return &stubInitializationLibraryLookup{
		libraries: map[string]*types.Library{
			"library-allowed": {ID: "library-allowed", SpaceID: 1, Type: types.LibraryTypeDocument},
			"library-other":   {ID: "library-other", SpaceID: 1, Type: types.LibraryTypeDocument},
		},
	}
}

func TestRemovedInitializationConfigRouteReturns404(t *testing.T) {
	libraryLookup := &stubInitializationLibraryLookup{
		libraries: map[string]*types.Library{
			"library-victim": {ID: "library-victim", SpaceID: 999},
		},
	}
	engine := newInitializationRouteTestEngine(t, 1, libraryLookup)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/initialization/config/library-victim", nil)
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code, "body=%s", rec.Body.String())
}
