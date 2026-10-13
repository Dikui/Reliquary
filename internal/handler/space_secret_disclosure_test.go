package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reliquary/internal/middleware"
	"reliquary/internal/types"
)

type stubSpaceService struct {
	space *types.Space
}

func (s *stubSpaceService) UpdateSpace(_ context.Context, space *types.Space) (*types.Space, error) {
	s.space = space
	return space, nil
}

func (s *stubSpaceService) CreateSpace(context.Context, *types.Space) (*types.Space, error) {
	return nil, nil
}
func (s *stubSpaceService) GetSpaceByID(context.Context, uint64) (*types.Space, error) {
	return s.space, nil
}
func (s *stubSpaceService) GetSpacesByIDs(context.Context, []uint64) (map[uint64]*types.Space, error) {
	return map[uint64]*types.Space{s.space.ID: s.space}, nil
}
func (s *stubSpaceService) DeleteSpace(context.Context, uint64) error { return nil }
func (s *stubSpaceService) ListSpaces(context.Context) ([]*types.Space, error) {
	return []*types.Space{s.space}, nil
}
func (s *stubSpaceService) ListAllSpaces(context.Context) ([]*types.Space, error) {
	return nil, nil
}
func (s *stubSpaceService) SearchSpaces(context.Context, string, uint64, int, int) ([]*types.Space, int64, error) {
	return nil, 0, nil
}

func (s *stubSpaceService) GetSpaceByIDForUser(context.Context, uint64, string) (*types.Space, error) {
	return s.space, nil
}

func newSpaceHandlerTestEngine(t *testing.T, space *types.Space) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := &SpaceHandler{service: &stubSpaceService{space: space}}

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.SpaceIDContextKey, space.ID)
		ctx = context.WithValue(ctx, types.SpaceInfoContextKey, space)
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.SpaceIDContextKey.String(), space.ID)
		c.Next()
	})
	r.GET("/spaces/:id", h.GetSpace)
	r.GET("/spaces/kv/:key", h.GetSpaceKV)
	r.PUT("/spaces/kv/:key", h.UpdateSpaceKV)
	return r
}

func secretSpaceFixture() *types.Space {
	return &types.Space{
		ID:   42,
		Name: "space",
	}
}

func TestGetSpaceKVAllowedForNonSecretKey(t *testing.T) {
	space := secretSpaceFixture()
	space.RetrievalConfig = &types.RetrievalConfig{}
	engine := newSpaceHandlerTestEngine(t, space)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/spaces/kv/retrieval-config", nil)
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
