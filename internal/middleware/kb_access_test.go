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

// ---------- Agent-share fallback ----------

// ---------- EnableRBAC=false rollout window ----------

func TestRequireKBAccessTeamRoleMatrix(t *testing.T) {
	required := []types.TenantRole{types.TenantRoleViewer, types.TenantRoleContributor, types.TenantRoleAdmin, types.TenantRoleOwner}
	cases := []struct {
		role    types.TenantRole
		allowed [4]bool
	}{
		{types.TenantRoleViewer, [4]bool{true, false, false, false}},
		{types.TenantRoleContributor, [4]bool{true, true, false, false}},
		{types.TenantRoleAdmin, [4]bool{true, true, true, false}},
		{types.TenantRoleOwner, [4]bool{true, true, true, true}},
	}
	for _, tc := range cases {
		for i, floor := range required {
			t.Run(string(tc.role)+"/"+string(floor), func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Params = gin.Params{{Key: "id", Value: "kb"}}
				ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
				ctx = context.WithValue(ctx, types.TenantRoleContextKey, tc.role)
				c.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
				RequireKBAccess(KBIDFromParam("id"), floor, &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb": {ID: "kb", TenantID: 1, CreatorID: "another-member"}}}, cfgRBAC(true))(c)
				require.Equal(t, !tc.allowed[i], c.IsAborted())
				if !c.IsAborted() {
					a, ok := KBAccessFromContext(c)
					require.True(t, ok)
					require.Equal(t, tc.role, a.Permission)
				}
			})
		}
	}
}
func TestRequireKBAccessRejectsOutsideTeam(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "id", Value: "kb"}}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleOwner)
	c.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	RequireKBAccess(KBIDFromParam("id"), types.TenantRoleViewer, &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb": {ID: "kb", TenantID: 2}}}, cfgRBAC(true))(c)
	require.True(t, c.IsAborted())
}
func TestRequireKBAccessNotFoundAndLookupFailure(t *testing.T) {
	for _, err := range []error{apprepo.ErrKnowledgeBaseNotFound, errors.New("database unavailable")} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Params = gin.Params{{Key: "id", Value: "kb"}}
		ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleViewer)
		c.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
		RequireKBAccess(KBIDFromParam("id"), types.TenantRoleViewer, &stubKBLookup{getErr: err}, cfgRBAC(true))(c)
		require.True(t, c.IsAborted())
		require.NotEmpty(t, c.Errors)
	}
}
