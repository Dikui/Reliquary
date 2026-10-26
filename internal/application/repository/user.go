package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var ErrUserNotFound = errors.New("user not found")

// userRepository implements user repository interface
type userRepository struct {
	db *gorm.DB
}

// NewUserRepository creates a new user repository
func NewUserRepository(db *gorm.DB) interfaces.UserRepository {
	return &userRepository{db: db}
}

// GetUsersByIDs batch-fetches users by id with a single SELECT … WHERE id IN (…)
// and projects the result into a map keyed by user id. Returns an empty
// map for an empty input slice. Missing ids are absent from the result.
func (r *userRepository) GetUsersByIDs(ctx context.Context, ids []string) (map[string]*types.User, error) {
	out := make(map[string]*types.User, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var users []*types.User
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, u := range users {
		out[u.ID] = u
	}
	return out, nil
}

// GetUserBySpaceID gets the sole user of the personal workspace
func (r *userRepository) GetUserBySpaceID(ctx context.Context, spaceID uint64) (*types.User, error) {
	var user types.User
	if err := r.db.WithContext(ctx).Where("space_id = ?", spaceID).Order("created_at ASC").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}
