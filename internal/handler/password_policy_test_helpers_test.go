package handler

import (
	"context"
	"reliquary/internal/types/interfaces"
)

type passwordPolicySettingService struct {
	interfaces.SystemSettingService
	enabled bool
}

func (s *passwordPolicySettingService) GetBool(_ context.Context, key, env string, fallback bool) bool {
	if key == "auth.complex_password_enabled" {
		return s.enabled
	}
	return fallback
}
