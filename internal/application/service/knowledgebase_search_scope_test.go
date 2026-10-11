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
	kbs := []*types.KnowledgeBase{
		{ID: "kb-a"}, {ID: "kb-b"}, {ID: "kb-c"},
	}
	if pickPrimary(kbs, "kb-b").ID != "kb-b" {
		t.Errorf("hit case failed")
	}
	// Miss returns nil rather than falling back to kbs[0]; the helper's
	// godoc explains the rationale (surface caller bugs, avoid leaking
	// unintended KB metadata).
	if pickPrimary(kbs, "kb-missing") != nil {
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

func TestAuthorizeKBAccess_SameSpaceAllPass(t *testing.T) {
	t.Parallel()
	s := &knowledgeBaseService{}
	kbs := []*types.KnowledgeBase{
		{ID: "kb-1", SpaceID: 7},
		{ID: "kb-2", SpaceID: 7},
	}
	err := s.authorizeKBAccess(ctxWithSpaceForAuth(7), kbs, 7)
	require.NoError(t, err)
}

func TestAuthorizeKBAccess_ForeignSpaceForbidden(t *testing.T) {
	t.Parallel()
	s := &knowledgeBaseService{}
	kbs := []*types.KnowledgeBase{
		{ID: "kb-foreign", SpaceID: 99},
	}
	err := s.authorizeKBAccess(ctxWithSpaceForAuth(7), kbs, 7)
	require.Error(t, err)
	app, ok := apperrors.IsAppError(err)
	require.True(t, ok, "expected typed AppError, got %T", err)
	assert.Equal(t, apperrors.ErrForbidden, app.Code,
		"foreign knowledge bases must be rejected")
}

func TestAuthorizeKBAccess_EmptyKBs_OK(t *testing.T) {
	t.Parallel()
	s := &knowledgeBaseService{}
	err := s.authorizeKBAccess(ctxWithSpaceForAuth(7), nil, 7)
	require.NoError(t, err)
}

func ctxWithSpaceForAuth(spaceID uint64) context.Context {
	return context.WithValue(context.Background(), types.SpaceIDContextKey, spaceID)
}
