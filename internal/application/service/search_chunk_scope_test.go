package service

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"testing"
)

func TestPickPrimary(t *testing.T) {
	t.Parallel()
	libraries := []*types.Library{
		{ID: "library-a"}, {ID: "library-b"}, {ID: "library-c"},
	}
	if pickPrimary(libraries, "library-b").ID != "library-b" {
		t.Errorf("hit case failed")
	}
	// Miss returns nil rather than falling back to libraries[0]; the helper's
	// godoc explains the rationale (surface caller bugs, avoid leaking
	// unintended Library metadata).
	if pickPrimary(libraries, "library-missing") != nil {
		t.Errorf("miss should return nil")
	}
	if pickPrimary(nil, "any") != nil {
		t.Errorf("nil slice should return nil")
	}
}

func TestTotalHits(t *testing.T) {
	t.Parallel()
	got := totalHits([]*types.RetrieveResult{
		{Results: []*types.IndexWithScore{{}, {}}},
		{Results: []*types.IndexWithScore{{}}},
		{Results: nil},
	})
	if got != 3 {
		t.Errorf("want 3, got %d", got)
	}
	if totalHits(nil) != 0 {
		t.Errorf("nil → 0")
	}
}

func TestAuthorizeLibraryAccess_SameSpaceAllPass(t *testing.T) {
	t.Parallel()
	s := &chunkSearchService{}
	libraries := []*types.Library{
		{ID: "library-1", SpaceID: 7},
		{ID: "library-2", SpaceID: 7},
	}
	err := s.authorizeLibraryAccess(ctxWithSpaceForAuth(7), libraries, 7)
	require.NoError(t, err)
}

func TestAuthorizeLibraryAccess_ForeignSpaceForbidden(t *testing.T) {
	t.Parallel()
	s := &chunkSearchService{}
	libraries := []*types.Library{
		{ID: "library-foreign", SpaceID: 99},
	}
	err := s.authorizeLibraryAccess(ctxWithSpaceForAuth(7), libraries, 7)
	require.Error(t, err)
	app, ok := apperrors.IsAppError(err)
	require.True(t, ok, "expected typed AppError, got %T", err)
	assert.Equal(t, apperrors.ErrForbidden, app.Code,
		"foreign libraries must be rejected")
}

func TestAuthorizeLibraryAccess_EmptyKBs_OK(t *testing.T) {
	t.Parallel()
	s := &chunkSearchService{}
	err := s.authorizeLibraryAccess(ctxWithSpaceForAuth(7), nil, 7)
	require.NoError(t, err)
}

func ctxWithSpaceForAuth(spaceID uint64) context.Context {
	return context.WithValue(context.Background(), types.SpaceIDContextKey, spaceID)
}
