package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"reliquary/internal/config"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type invitedRegistrationUserService struct {
	interfaces.UserService
	registeredToken string
	updatedTenant   uint64
	updateCalls     []uint64
}

func (s *invitedRegistrationUserService) GetUserByEmail(context.Context, string) (*types.User, error) {
	return nil, nil
}

func (s *invitedRegistrationUserService) Register(_ context.Context, req *types.RegisterRequest) (*types.User, error) {
	s.registeredToken = req.InvitationToken
	return &types.User{ID: "new-user", Username: req.Username, Email: req.Email, IsActive: true, TenantID: types.SingleTenantID}, nil
}

func (s *invitedRegistrationUserService) GenerateTokens(context.Context, *types.User) (string, string, error) {
	return "access", "refresh", nil
}

type invitedRegistrationInvitationService struct {
	interfaces.TenantInvitationService
	acceptErr error
}

func (s *invitedRegistrationInvitationService) LookupByToken(context.Context, string) (*types.TenantInvitation, error) {
	return &types.TenantInvitation{TenantID: types.SingleTenantID, Role: types.TenantRoleViewer}, nil
}

type invitedRegistrationTenantService struct {
	interfaces.TenantService
}

func (s *invitedRegistrationTenantService) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: types.SingleTenantID, Name: "Invited Workspace"}, nil
}

func TestRegisterByInviteUsesInvitedTenantWithoutPersonalTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := &invitedRegistrationUserService{}
	h := &AuthHandler{
		userService:   users,
		tenantService: &invitedRegistrationTenantService{},
		invitationSvc: &invitedRegistrationInvitationService{},
	}
	r := gin.New()
	r.Use(errorCapture())
	r.POST("/auth/register-by-invite", h.RegisterByInvite)

	body := []byte(`{"token":"invite-token","email":"alice@example.com","username":"alice","password":"supersecret1"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/register-by-invite", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if users.registeredToken != "invite-token" {
		t.Fatalf("registered invitation token=%q, want invite-token", users.registeredToken)
	}
}

func TestRegisterByInviteRejectsSimplePasswordWhenComplexEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := &invitedRegistrationUserService{}
	h := &AuthHandler{
		userService:   users,
		tenantService: &invitedRegistrationTenantService{},
		invitationSvc: &invitedRegistrationInvitationService{},
		configInfo:    &config.Config{Auth: &config.AuthConfig{ComplexPasswordEnabled: true}},
	}
	r := gin.New()
	r.Use(errorCapture())
	r.POST("/auth/register-by-invite", h.RegisterByInvite)

	body := []byte(`{"token":"invite-token","email":"alice@example.com","username":"alice","password":"supersecret1"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/register-by-invite", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", w.Code, w.Body.String())
	}
	if users.registeredToken != "" {
		t.Fatalf("Register was called with mode=%q", users.registeredToken)
	}
}
