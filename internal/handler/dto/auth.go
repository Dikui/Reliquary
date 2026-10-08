package dto

import "reliquary/internal/types"

// AuthLoginResponse is the HTTP-safe login / switch-tenant response shape.
type AuthLoginResponse struct {
	Success      bool            `json:"success"`
	Message      string          `json:"message,omitempty"`
	User         *types.User     `json:"user,omitempty"`
	ActiveTenant *TenantResponse `json:"active_tenant,omitempty"`
	Token        string          `json:"token,omitempty"`
	RefreshToken string          `json:"refresh_token,omitempty"`
}

// NewAuthLoginResponse converts a service-layer login response for HTTP output.
func NewAuthLoginResponse(resp *types.LoginResponse) *AuthLoginResponse {
	if resp == nil {
		return nil
	}
	return &AuthLoginResponse{
		Success:      resp.Success,
		Message:      resp.Message,
		User:         resp.User,
		ActiveTenant: NewTenantResponseWithSecrets(resp.ActiveTenant),
		Token:        resp.Token,
		RefreshToken: resp.RefreshToken,
	}
}
