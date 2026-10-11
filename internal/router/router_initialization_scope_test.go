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

type stubInitializationKBLookup struct {
	kbs map[string]*types.KnowledgeBase
}

func (s *stubInitializationKBLookup) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	if kb, ok := s.kbs[id]; ok {
		return kb, nil
	}
	return nil, apprepo.ErrKnowledgeBaseNotFound
}

func newInitializationRouteTestEngine(t *testing.T, callerTenantID uint64, kbLookup *stubInitializationKBLookup) *gin.Engine {
	return newKBRouteTestEngine(t, callerTenantID, kbLookup, func(r *gin.RouterGroup, guards *accessGuards) {
		RegisterInitializationRoutes(r, &handler.InitializationHandler{}, guards)
	})
}

func newKBRouteTestEngine(
	t *testing.T,
	callerTenantID uint64,
	kbLookup *stubInitializationKBLookup,
	register func(r *gin.RouterGroup, guards *accessGuards),
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	guards := &accessGuards{
		kbService: kbLookup,
	}

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.TenantIDContextKey, callerTenantID)
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), callerTenantID)
		c.Next()
	})

	register(r.Group("/api/v1"), guards)
	return r
}

func tenantKBLookupFixture() *stubInitializationKBLookup {
	return &stubInitializationKBLookup{
		kbs: map[string]*types.KnowledgeBase{
			"kb-allowed": {ID: "kb-allowed", TenantID: 1, Type: types.KnowledgeBaseTypeDocument},
			"kb-other":   {ID: "kb-other", TenantID: 1, Type: types.KnowledgeBaseTypeDocument},
		},
	}
}

func TestRemovedInitializationConfigRouteReturns404(t *testing.T) {
	kbLookup := &stubInitializationKBLookup{
		kbs: map[string]*types.KnowledgeBase{
			"kb-victim": {ID: "kb-victim", TenantID: 999},
		},
	}
	engine := newInitializationRouteTestEngine(t, 1, kbLookup)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/initialization/config/kb-victim", nil)
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code, "body=%s", rec.Body.String())
}
