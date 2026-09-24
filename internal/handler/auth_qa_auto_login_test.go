package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"reliquary/internal/config"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type qaAutoLoginUserService struct{ interfaces.UserService }

func (*qaAutoLoginUserService) GetUserByEmail(_ context.Context, _ string) (*types.User, error) {
	return &types.User{ID: "qa-user", Email: "qa@example.test", TenantID: 1, IsActive: true}, nil
}

func (*qaAutoLoginUserService) GenerateTokens(_ context.Context, _ *types.User) (string, string, error) {
	return "qa-access", "qa-refresh", nil
}

func (*qaAutoLoginUserService) BuildLoginMemberships(_ context.Context, _ *types.User, _ *types.Tenant) []types.Membership {
	return []types.Membership{{TenantID: 1, Role: types.TenantRoleOwner}}
}

type qaAutoLoginTenantService struct{ interfaces.TenantService }

func (*qaAutoLoginTenantService) GetTenantByID(_ context.Context, _ uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: 1, Name: "QA"}, nil
}

func TestQAAutoLoginIsLocalAndOptIn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldEdition := Edition
	Edition = "standard"
	defer func() { Edition = oldEdition }()
	t.Setenv("RELIQUARY_QA_AUTO_LOGIN_EMAIL", "qa@example.test")

	h := &AuthHandler{
		configInfo:    &config.Config{Server: &config.ServerConfig{Host: "127.0.0.1"}},
		userService:   &qaAutoLoginUserService{},
		tenantService: &qaAutoLoginTenantService{},
	}
	r := gin.New()
	r.Use(errorCapture())
	r.POST("/auth/auto-setup", h.AutoSetup)

	request := func(remote, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/auto-setup", nil)
		req.RemoteAddr = remote
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if got := request("127.0.0.1:1234", "http://127.0.0.1:8091"); got.Code != http.StatusOK ||
		!strings.Contains(got.Body.String(), `"token":"qa-access"`) ||
		!strings.Contains(got.Body.String(), `"active_tenant"`) {
		t.Fatalf("local QA auto-login failed: %d %s", got.Code, got.Body.String())
	}
	if got := request("127.0.0.1:1234", "https://outside.example"); got.Code != http.StatusForbidden {
		t.Fatalf("foreign origin must be denied: %d", got.Code)
	}
	if got := request("192.0.2.1:1234", ""); got.Code != http.StatusForbidden {
		t.Fatalf("remote client must be denied: %d", got.Code)
	}
	h.configInfo.Server.Host = "0.0.0.0"
	if got := request("127.0.0.1:1234", ""); got.Code != http.StatusForbidden {
		t.Fatalf("nonlocal binding must be denied: %d", got.Code)
	}
	h.configInfo.Server.Host = "127.0.0.1"
	t.Setenv("RELIQUARY_QA_AUTO_LOGIN_EMAIL", "")
	if got := request("127.0.0.1:1234", ""); got.Code != http.StatusForbidden {
		t.Fatalf("QA auto-login must be disabled by default: %d", got.Code)
	}
	t.Setenv("RELIQUARY_QA_AUTO_LOGIN_EMAIL", "qa@example.test")
	gin.SetMode(gin.ReleaseMode)
	defer gin.SetMode(gin.TestMode)
	if got := request("127.0.0.1:1234", ""); got.Code != http.StatusForbidden {
		t.Fatalf("release mode must deny QA auto-login: %d", got.Code)
	}
}
