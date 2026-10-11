package repository

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"reliquary/internal/types"
)

var ErrRegistrationClosed = errors.New("registration is closed")

func (r *userRepository) HasUsers(ctx context.Context) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Unscoped().Model(&types.User{}).Count(&count).Error
	return count > 0, err
}

// RegisterSingleUser serializes registration across server processes.
func (r *userRepository) RegisterSingleUser(ctx context.Context, user *types.User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(72651001)).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Unscoped().Model(&types.User{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrRegistrationClosed
		}
		user.TenantID = types.SingleTenantID
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		_, err := EnsureTenantKnowledgeBase(ctx, tx, user.TenantID, user.ID)
		return err
	})
}
