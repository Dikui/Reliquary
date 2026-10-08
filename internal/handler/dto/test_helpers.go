package dto

import (
	"context"
	"reliquary/internal/types"
)

func userContext() context.Context {
	return context.WithValue(context.Background(), types.UserIDContextKey, "personal-user")
}
