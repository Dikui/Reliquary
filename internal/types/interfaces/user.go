package interfaces

import (
	"context"

	"reliquary/internal/types"
)

// UserService defines the user service interface
type UserService interface {
	HasUsers(ctx context.Context) (bool, error)
	// Register creates a new user account
	Register(ctx context.Context, req *types.RegisterRequest) (*types.User, error)
	// Login authenticates a user and returns tokens
	Login(ctx context.Context, req *types.LoginRequest) (*types.LoginResponse, error)
	// GetUserByID gets a user by ID
	GetUserByID(ctx context.Context, id string) (*types.User, error)
	// GetUsersByIDs batch-fetches users by id, returning a map keyed by
	// user id. Missing ids are simply absent from the result; the call
	// is not an error when some ids resolve to no row.
	GetUsersByIDs(ctx context.Context, ids []string) (map[string]*types.User, error)
	// GetUserByEmail gets a user by email
	GetUserByEmail(ctx context.Context, email string) (*types.User, error)
	// GetUserByUsername gets a user by username
	GetUserByUsername(ctx context.Context, username string) (*types.User, error)
	// GetUserBySpaceID gets the sole user of the personal workspace
	GetUserBySpaceID(ctx context.Context, spaceID uint64) (*types.User, error)
	// UpdateUser updates user information
	UpdateUser(ctx context.Context, user *types.User) error
	// DeleteUser deletes a user
	DeleteUser(ctx context.Context, id string) error
	// ChangePassword changes user password
	ChangePassword(ctx context.Context, userID string, oldPassword, newPassword string) error
	ValidatePassword(ctx context.Context, userID string, password string) error
	// GenerateTokens generates access and refresh tokens for user
	GenerateTokens(ctx context.Context, user *types.User) (accessToken, refreshToken string, err error)
	ValidateToken(ctx context.Context, token string) (*types.User, uint64, error)
	// RefreshToken refreshes access token using refresh token
	RefreshToken(ctx context.Context, refreshToken string) (accessToken, newRefreshToken string, err error)
	// RevokeToken revokes a token
	RevokeToken(ctx context.Context, token string) error
	// Logout revokes every outstanding access/refresh token for the user
	// identified by the presented JWT.
	Logout(ctx context.Context, token string) error
	// GetCurrentUser gets current user from context
	GetCurrentUser(ctx context.Context) (*types.User, error)
	// SearchUsers searches users by username or email
	SearchUsers(ctx context.Context, query string, limit int) ([]*types.User, error)
}

// UserRepository defines the user repository interface
type UserRepository interface {
	// CreateUser creates a user
	CreateUser(ctx context.Context, user *types.User) error
	RegisterSingleUser(ctx context.Context, user *types.User) error
	HasUsers(ctx context.Context) (bool, error)
	// GetUserByID gets a user by ID
	GetUserByID(ctx context.Context, id string) (*types.User, error)
	// GetUsersByIDs batch-fetches users by id, returning a map keyed by
	// user id. Missing ids are simply absent from the result.
	GetUsersByIDs(ctx context.Context, ids []string) (map[string]*types.User, error)
	// GetUserByEmail gets a user by email
	GetUserByEmail(ctx context.Context, email string) (*types.User, error)
	// GetUserByUsername gets a user by username
	GetUserByUsername(ctx context.Context, username string) (*types.User, error)
	// GetUserBySpaceID gets the sole user of the personal workspace
	GetUserBySpaceID(ctx context.Context, spaceID uint64) (*types.User, error)
	// UpdateUser updates a user
	UpdateUser(ctx context.Context, user *types.User) error
	// DeleteUser deletes a user
	DeleteUser(ctx context.Context, id string) error
	// ListUsers lists users with pagination
	ListUsers(ctx context.Context, offset, limit int) ([]*types.User, error)
	SearchUsers(ctx context.Context, query string, limit int) ([]*types.User, error)
}

// AuthTokenRepository defines the auth token repository interface
type AuthTokenRepository interface {
	// CreateToken creates an auth token
	CreateToken(ctx context.Context, token *types.AuthToken) error
	// GetTokenByValue gets a token by its value
	GetTokenByValue(ctx context.Context, tokenValue string) (*types.AuthToken, error)
	// GetTokensByUserID gets all tokens for a user
	GetTokensByUserID(ctx context.Context, userID string) ([]*types.AuthToken, error)
	// UpdateToken updates a token
	UpdateToken(ctx context.Context, token *types.AuthToken) error
	// DeleteToken deletes a token
	DeleteToken(ctx context.Context, id string) error
	// DeleteExpiredTokens deletes all expired tokens
	DeleteExpiredTokens(ctx context.Context) error
	// RevokeTokensByUserID revokes all tokens for a user
	RevokeTokensByUserID(ctx context.Context, userID string) error
}
