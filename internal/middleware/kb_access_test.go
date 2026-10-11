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

// stubKBLookup is a tiny KBLookup stand-in for tests; satisfies the
// KBLookup interface (a single method) without dragging in the full
// KnowledgeBaseService surface.
type stubKBLookup struct {
	kbs    map[string]*types.KnowledgeBase
	getErr error
}

func (s *stubKBLookup) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if kb, ok := s.kbs[id]; ok {
		return kb, nil
	}
	return nil, apprepo.ErrKnowledgeBaseNotFound
}

// TestIsResourceNotFound_RecognisesKnowledgeSentinel pins that a missing
// *document* (knowledge) is treated as not-found, not a transient error.
// Regression: ErrKnowledgeNotFound was absent from the predicate, so
// GET/DELETE /knowledge/:id and chunk list resolved a missing doc into a
// raw 500 instead of a 404 — which the CLI then surfaced as a retryable
// server.error (exit 7), looping agents on a permanently-absent doc.
func TestIsResourceNotFound_RecognisesKnowledgeSentinel(t *testing.T) {
	require.True(t, isResourceNotFound(apprepo.ErrKnowledgeNotFound),
		"missing document (ErrKnowledgeNotFound) must classify as not-found")
	require.True(t, isResourceNotFound(apprepo.ErrKnowledgeBaseNotFound),
		"missing KB must still classify as not-found")
	require.True(t, isResourceNotFound(apprepo.ErrChunkNotFound),
		"missing chunk (ErrChunkNotFound) must classify as not-found — chunk view/by-id resolved a missing chunk into a raw 500 (exit 7) otherwise")
	require.True(t, isResourceNotFound(ErrResourceNotFound),
		"generic resource-not-found sentinel must still classify as not-found")
	require.False(t, isResourceNotFound(errors.New("connection refused")),
		"a genuine transient error must NOT be classified as not-found")
}

func TestRequireKBExistsWithinWorkspace(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "id", Value: "kb"}}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	c.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	RequireKBExists(KBIDFromParam("id"), &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb": {ID: "kb", SpaceID: 1}}})(c)
	require.False(t, c.IsAborted())
	access, ok := KBAccessFromContext(c)
	require.True(t, ok)
	require.Equal(t, uint64(1), access.EffectiveSpaceID)
	require.Equal(t, "kb", access.KnowledgeBase.ID)
}

func TestRequireKBExistsRejectsOutsideWorkspace(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "id", Value: "kb"}}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))

	c.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	RequireKBExists(KBIDFromParam("id"), &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb": {ID: "kb", SpaceID: 2}}})(c)
	require.True(t, c.IsAborted())
}
func TestRequireKBExistsNotFoundAndLookupFailure(t *testing.T) {
	for _, err := range []error{apprepo.ErrKnowledgeBaseNotFound, errors.New("database unavailable")} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Params = gin.Params{{Key: "id", Value: "kb"}}
		ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))

		c.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
		RequireKBExists(KBIDFromParam("id"), &stubKBLookup{getErr: err})(c)
		require.True(t, c.IsAborted())
		require.NotEmpty(t, c.Errors)
	}
}
