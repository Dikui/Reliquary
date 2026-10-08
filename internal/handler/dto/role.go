package dto

import "context"

// Authenticated requests share the personal user's full configuration access.
func CanViewIntegrationSecrets(ctx context.Context) bool { return true }
