package interfaces

import (
	"context"
	"reliquary/internal/types"
)

// UserService provides the personal workspace owner and historical actor names.
type UserService interface {
	EnsureSoleUser(ctx context.Context) (*types.User, error)
	GetUsersByIDs(ctx context.Context, ids []string) (map[string]*types.User, error)
	GetUserBySpaceID(ctx context.Context, spaceID uint64) (*types.User, error)
}

// UserRepository stores the sole owner and resolves historical actor names.
type UserRepository interface {
	RegisterSingleUser(ctx context.Context, user *types.User) error
	GetUsersByIDs(ctx context.Context, ids []string) (map[string]*types.User, error)
	GetUserBySpaceID(ctx context.Context, spaceID uint64) (*types.User, error)
}
