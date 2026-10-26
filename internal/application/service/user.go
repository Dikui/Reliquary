package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// userService implements the UserService interface
type userService struct {
	userRepo interfaces.UserRepository
}

// NewUserService creates a new user service instance
func NewUserService(
	userRepo interfaces.UserRepository,
) interfaces.UserService {
	return &userService{
		userRepo: userRepo,
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

// GetUsersByIDs proxies to the repository batch fetch. Returns an empty
// map for an empty input; missing ids are absent from the result.
func (s *userService) GetUsersByIDs(ctx context.Context, ids []string) (map[string]*types.User, error) {
	return s.userRepo.GetUsersByIDs(ctx, ids)
}

// GetUserBySpaceID gets the sole user of the personal workspace
func (s *userService) GetUserBySpaceID(ctx context.Context, spaceID uint64) (*types.User, error) {
	return s.userRepo.GetUserBySpaceID(ctx, spaceID)
}
