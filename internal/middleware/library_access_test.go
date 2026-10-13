package middleware

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/types"
)

// stubLibraryLookup is a tiny LibraryLookup stand-in for tests; satisfies the
// LibraryLookup interface (a single method) without dragging in the full
// LibraryService surface.
type stubLibraryLookup struct {
	libraries map[string]*types.Library
	getErr    error
}

func (s *stubLibraryLookup) GetLibraryByID(_ context.Context, id string) (*types.Library, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if library, ok := s.libraries[id]; ok {
		return library, nil
	}
	return nil, apprepo.ErrLibraryNotFound
}

// TestIsResourceNotFound_RecognisesEntrySentinel pins that a missing
// *document* (entry) is treated as not-found, not a transient error.
// Regression: ErrEntryNotFound was absent from the predicate, so
// GET/DELETE /entries/:id and chunk list resolved a missing doc into a
// raw 500 instead of a 404 — which the CLI then surfaced as a retryable
// server.error (exit 7), looping agents on a permanently-absent doc.
func TestIsResourceNotFound_RecognisesEntrySentinel(t *testing.T) {
	require.True(t, isResourceNotFound(apprepo.ErrEntryNotFound),
		"missing document (ErrEntryNotFound) must classify as not-found")
	require.True(t, isResourceNotFound(apprepo.ErrLibraryNotFound),
		"missing Library must still classify as not-found")
	require.True(t, isResourceNotFound(apprepo.ErrChunkNotFound),
		"missing chunk (ErrChunkNotFound) must classify as not-found — chunk view/by-id resolved a missing chunk into a raw 500 (exit 7) otherwise")
	require.True(t, isResourceNotFound(ErrResourceNotFound),
		"generic resource-not-found sentinel must still classify as not-found")
	require.False(t, isResourceNotFound(errors.New("connection refused")),
		"a genuine transient error must NOT be classified as not-found")
}

func TestRequireLibraryExistsWithinWorkspace(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "id", Value: "library"}}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	c.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	RequireLibraryExists(LibraryIDFromParam("id"), &stubLibraryLookup{libraries: map[string]*types.Library{"library": {ID: "library", SpaceID: 1}}})(c)
	require.False(t, c.IsAborted())
	access, ok := LibraryAccessFromContext(c)
	require.True(t, ok)
	require.Equal(t, uint64(1), access.EffectiveSpaceID)
	require.Equal(t, "library", access.Library.ID)
}

func TestRequireLibraryExistsRejectsOutsideWorkspace(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "id", Value: "library"}}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))

	c.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	RequireLibraryExists(LibraryIDFromParam("id"), &stubLibraryLookup{libraries: map[string]*types.Library{"library": {ID: "library", SpaceID: 2}}})(c)
	require.True(t, c.IsAborted())
}
func TestRequireLibraryExistsNotFoundAndLookupFailure(t *testing.T) {
	for _, err := range []error{apprepo.ErrLibraryNotFound, errors.New("database unavailable")} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Params = gin.Params{{Key: "id", Value: "library"}}
		ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))

		c.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
		RequireLibraryExists(LibraryIDFromParam("id"), &stubLibraryLookup{getErr: err})(c)
		require.True(t, c.IsAborted())
		require.NotEmpty(t, c.Errors)
	}
}
