package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"reliquary/internal/config"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func runPathTenantHandler(
	cfg *config.Config, ctxTenantID uint64, user *types.User, urlPath string,
) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ErrorHandler()) // see the response envelope checks
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		if ctxTenantID != 0 {
			ctx = context.WithValue(ctx, types.TenantIDContextKey, ctxTenantID)
		}
		if user != nil {
			ctx = context.WithValue(ctx, types.UserContextKey, user)
			ctx = context.WithValue(ctx, types.UserIDContextKey, user.ID)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.GET("/tenants/:id", RequirePathTenantMatch(cfg), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, urlPath, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestRequirePathTenantMatch_MatchAllows(t *testing.T) {
	w := runPathTenantHandler(nil, 7, nil, "/tenants/7")
	if w.Code != http.StatusOK {
		t.Fatalf("matching :id must allow, got %d", w.Code)
	}
}

func TestRequirePathTenantMatch_MismatchRejects(t *testing.T) {
	w := runPathTenantHandler(nil, 7, nil, "/tenants/9")
	if w.Code != http.StatusForbidden {
		t.Fatalf("mismatch must 403, got %d", w.Code)
	}
}

func TestRequirePathTenantMatch_MissingCtxTenantRejects(t *testing.T) {
	// If the auth middleware never set TenantIDContextKey we fail
	// closed — silently treating "no ctx" as a match would be a footgun
	// the next time someone refactors the auth chain.
	w := runPathTenantHandler(nil, 0, nil, "/tenants/7")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing tenant ctx must 401, got %d", w.Code)
	}
}

func TestRequirePathTenantMatch_NonNumericIDRejects(t *testing.T) {
	w := runPathTenantHandler(nil, 7, nil, "/tenants/abc")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-numeric :id must 400, got %d", w.Code)
	}
}

func TestRequirePathTenantMatch_ZeroIDRejects(t *testing.T) {
	w := runPathTenantHandler(nil, 7, nil, "/tenants/0")
	if w.Code != http.StatusBadRequest {
		t.Fatalf(":id=0 must 400, got %d", w.Code)
	}
}

// TestAccessMiddleware_ResponseEnvelope pins the wire format produced
// by RequirePathTenantMatch when they
// reject. Both used to live in handlers that called
// c.Error(apperrors.NewForbiddenError(...)) and rendered through
// ErrorHandler — clients (frontend axios interceptor, Go SDK) key off
// `success` and `error.code`. If a future change drops c.Error in
// favour of a raw c.JSON, this test catches the regression.
func TestAccessMiddleware_ResponseEnvelope(t *testing.T) {
	t.Run("RequirePathTenantMatch_Mismatch", func(t *testing.T) {
		w := runPathTenantHandler(nil, 7, nil, "/tenants/9")
		assertEnvelope(t, w, http.StatusForbidden, apperrors.ErrForbidden)
	})
	t.Run("RequirePathTenantMatch_NonNumeric", func(t *testing.T) {
		w := runPathTenantHandler(nil, 7, nil, "/tenants/abc")
		// NewValidationError uses ErrValidation, not ErrBadRequest — both
		// surface as HTTP 400 but the code in the body is ErrValidation.
		assertEnvelope(t, w, http.StatusBadRequest, apperrors.ErrValidation)
	})
	t.Run("RequirePathTenantMatch_NoCtxTenant", func(t *testing.T) {
		w := runPathTenantHandler(nil, 0, nil, "/tenants/7")
		assertEnvelope(t, w, http.StatusUnauthorized, apperrors.ErrUnauthorized)
	})
}

func assertEnvelope(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantCode apperrors.ErrorCode) {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("status: got %d, want %d (body=%s)", w.Code, wantStatus, w.Body.String())
	}
	var body struct {
		Success bool `json:"success"`
		Error   struct {
			Code    apperrors.ErrorCode `json:"code"`
			Message string              `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the standard envelope: %v (body=%s)", err, w.Body.String())
	}
	if body.Success {
		t.Fatalf("envelope success must be false on rejection, got body=%s", w.Body.String())
	}
	if body.Error.Code != wantCode {
		t.Fatalf("envelope error.code: got %d, want %d (body=%s)",
			body.Error.Code, wantCode, w.Body.String())
	}
	if body.Error.Message == "" {
		t.Fatalf("envelope error.message must be non-empty, got body=%s", w.Body.String())
	}
}
