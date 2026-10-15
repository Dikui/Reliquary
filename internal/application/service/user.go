package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// userService implements the UserService interface
type userService struct {
	userRepo     interfaces.UserRepository
	spaceService interfaces.SpaceService
	config       *config.Config
}

// NewUserService creates a new user service instance
func NewUserService(
	configInfo *config.Config,
	userRepo interfaces.UserRepository,
	spaceService interfaces.SpaceService,
) interfaces.UserService {
	return &userService{
		userRepo:     userRepo,
		spaceService: spaceService,
		config:       configInfo,
	}
}

// EnsureSoleUser returns the personal workspace's only user, creating it on first use.
func (s *userService) EnsureSoleUser(ctx context.Context) (*types.User, error) {
	user, err := s.userRepo.GetUserBySpaceID(ctx, types.SingleSpaceID)
	if !errors.Is(err, apprepo.ErrUserNotFound) {
		return user, err
	}
	now := time.Now()
	user = &types.User{
		ID: uuid.NewString(), Username: "me", Email: "me@localhost",
		SpaceID: types.SingleSpaceID, IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	// A concurrent first request may have created the user already.
	if err := s.userRepo.RegisterSingleUser(ctx, user); err != nil && !errors.Is(err, apprepo.ErrRegistrationClosed) {
		return nil, err
	}
	return s.userRepo.GetUserBySpaceID(ctx, types.SingleSpaceID)
}

// GetUserByID gets a user by ID
func (s *userService) GetUserByID(ctx context.Context, id string) (*types.User, error) {
	return s.userRepo.GetUserByID(ctx, id)
}

// GetUsersByIDs proxies to the repository batch fetch. Returns an empty
// map for an empty input; missing ids are absent from the result.
func (s *userService) GetUsersByIDs(ctx context.Context, ids []string) (map[string]*types.User, error) {
	return s.userRepo.GetUsersByIDs(ctx, ids)
}

// GetUserByEmail gets a user by email
func (s *userService) GetUserByEmail(ctx context.Context, email string) (*types.User, error) {
	return s.userRepo.GetUserByEmail(ctx, email)
}

// GetUserByUsername gets a user by username
func (s *userService) GetUserByUsername(ctx context.Context, username string) (*types.User, error) {
	return s.userRepo.GetUserByUsername(ctx, username)
}

// GetUserBySpaceID gets the sole user of the personal workspace
func (s *userService) GetUserBySpaceID(ctx context.Context, spaceID uint64) (*types.User, error) {
	return s.userRepo.GetUserBySpaceID(ctx, spaceID)
}

// UpdateUser updates user information
func (s *userService) UpdateUser(ctx context.Context, user *types.User) error {
	user.UpdatedAt = time.Now()
	return s.userRepo.UpdateUser(ctx, user)
}

// DeleteUser deletes a user
func (s *userService) DeleteUser(ctx context.Context, id string) error {
	return s.userRepo.DeleteUser(ctx, id)
}

// GetCurrentUser gets current user from context
func (s *userService) GetCurrentUser(ctx context.Context) (*types.User, error) {
	user, ok := ctx.Value(types.UserContextKey).(*types.User)
	if !ok {
		return nil, errors.New("user not found in context")
	}

	return user, nil
}

// SearchUsers searches users by username or email
func (s *userService) SearchUsers(ctx context.Context, query string, limit int) ([]*types.User, error) {
	if query == "" {
		return []*types.User{}, nil
	}
	return s.userRepo.SearchUsers(ctx, query, limit)
}
