package dto

import "reliquary/internal/types"

// AuthLoginResponse is the HTTP-safe login / switch-space response shape.
type AuthLoginResponse struct {
	Success      bool           `json:"success"`
	Message      string         `json:"message,omitempty"`
	User         *types.User    `json:"user,omitempty"`
	ActiveSpace  *SpaceResponse `json:"active_space,omitempty"`
	Token        string         `json:"token,omitempty"`
	RefreshToken string         `json:"refresh_token,omitempty"`
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
		ActiveSpace:  NewSpaceResponseWithSecrets(resp.ActiveSpace),
		Token:        resp.Token,
		RefreshToken: resp.RefreshToken,
	}
}
