package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"reliquary/internal/types"
)

// TestApplyAuthSessionSetsBothSurfaces locks the core invariant of the
// helper: every attached value must be readable from BOTH c.Keys (c.Get)
// and the request context (types.*FromContext). A key present on only one
// surface is the class of bug the helper exists to prevent.
func TestApplyAuthSessionSetsBothSurfaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/libraries", nil)

	user := &types.User{ID: "u1"}
	space := &types.Space{ID: 1}
	principal := types.Principal{Type: types.PrincipalWebUser, ID: "u1"}

	applyAuthSession(c, authSession{
		User:      user,
		Principal: principal,
		SpaceID:   1,
		Space:     space,
	})

	ctx := c.Request.Context()
	if got, ok := types.SpaceIDFromContext(ctx); !ok || got != 1 {
		t.Fatalf("ctx space id = %d, ok=%v", got, ok)
	}
	if got, ok := c.Get(types.SpaceIDContextKey.String()); !ok || got.(uint64) != 1 {
		t.Fatalf("keys space id = %v, ok=%v", got, ok)
	}
	if got, ok := types.SpaceInfoFromContext(ctx); !ok || got.ID != 1 {
		t.Fatalf("ctx space info = %#v, ok=%v", got, ok)
	}
	if got, ok := types.UserIDFromContext(ctx); !ok || got != "u1" {
		t.Fatalf("ctx user id = %q, ok=%v", got, ok)
	}
	if got, ok := c.Get(types.UserContextKey.String()); !ok || got.(*types.User).ID != "u1" {
		t.Fatalf("keys user = %#v, ok=%v", got, ok)
	}
	if got, ok := types.PrincipalFromContext(ctx); !ok || got != principal {
		t.Fatalf("ctx principal = %#v, ok=%v", got, ok)
	}

}

func TestBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		header string
		want   string
		ok     bool
	}{
		{"", "", false},
		{"Basic abc", "", false},
		{"Bearer", "", false},
		{"Bearer abc", "abc", true},
	}
	for _, tc := range cases {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.header != "" {
			c.Request.Header.Set("Authorization", tc.header)
		}
		got, ok := bearerToken(c)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("bearerToken(%q) = (%q, %v), want (%q, %v)", tc.header, got, ok, tc.want, tc.ok)
		}
	}
}
