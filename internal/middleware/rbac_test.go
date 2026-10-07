package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"reliquary/internal/config"
	"reliquary/internal/types"
)

func rbacTestHarness(role types.TenantRole, userID string, mw gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		// Mirror what middleware/auth.go's JWT path sets.
		ctx := context.WithValue(c.Request.Context(), types.TenantRoleContextKey, role)
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.GET("/protected", mw, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	r.ServeHTTP(w, req)
	return w
}

func cfgRBAC(enabled bool) *config.Config {
	return &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}
}

// ---------- RequireRole ----------

func TestRequireRole_AllowsAtMin(t *testing.T) {
	w := rbacTestHarness(types.TenantRoleAdmin, "u1",
		RequireRole(types.TenantRoleAdmin, cfgRBAC(true)))
	if w.Code != http.StatusOK {
		t.Fatalf("Admin should clear Admin gate, got %d", w.Code)
	}
}

func TestRequireRole_AllowsAboveMin(t *testing.T) {
	w := rbacTestHarness(types.TenantRoleOwner, "u1",
		RequireRole(types.TenantRoleAdmin, cfgRBAC(true)))
	if w.Code != http.StatusOK {
		t.Fatalf("Owner should clear Admin gate, got %d", w.Code)
	}
}

func TestRequireRole_RejectsBelowMin(t *testing.T) {
	w := rbacTestHarness(types.TenantRoleContributor, "u1",
		RequireRole(types.TenantRoleAdmin, cfgRBAC(true)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("Contributor must NOT clear Admin gate, got %d", w.Code)
	}
}

func TestRequireRoleOrSystemAdmin_AllowsSystemAdminBelowTenantRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantRoleContextKey, types.TenantRoleViewer)
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.GET("/protected",
		RequireRoleOrSystemAdmin(types.TenantRoleAdmin, cfgRBAC(true)),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/protected", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireRoleOrSystemAdmin_RejectsOrdinaryViewer(t *testing.T) {
	w := rbacTestHarness(types.TenantRoleViewer, "u1",
		RequireRoleOrSystemAdmin(types.TenantRoleAdmin, cfgRBAC(true)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("ordinary Viewer must not clear Admin-or-SystemAdmin gate, got %d", w.Code)
	}
}

func TestRequireRole_FailOpenWhenRBACDisabled(t *testing.T) {
	// EnableRBAC=false: the middleware should log but not block, so the
	// downstream handler still runs. This is the rollout-safety guarantee.
	w := rbacTestHarness(types.TenantRoleViewer, "u1",
		RequireRole(types.TenantRoleOwner, cfgRBAC(false)))
	if w.Code != http.StatusOK {
		t.Fatalf("EnableRBAC=false must let Viewer through Owner gate, got %d", w.Code)
	}
}

func TestRequireRole_NilConfigFailsOpen(t *testing.T) {
	// Defensive: nil config must not panic and must fail open (no enforcement
	// configured = behave like the legacy path).
	w := rbacTestHarness(types.TenantRoleViewer, "u1",
		RequireRole(types.TenantRoleAdmin, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("nil config must fail open, got %d", w.Code)
	}
}
