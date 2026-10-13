package service

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"reliquary/internal/application/repository"
	"reliquary/internal/types"
)

// EnsurePersonalSpace runs before any request or background task starts.
func EnsurePersonalSpace(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(72651000)).Error; err != nil {
			return err
		}
		repo := repository.NewSpaceRepository(tx)
		spaces, err := repo.ListSpaces(ctx)
		if err != nil {
			return err
		}
		if len(spaces) == 1 && spaces[0].ID == types.SingleSpaceID {
			_, err := repository.EnsureSystemLibraries(ctx, tx, types.SingleSpaceID, "")
			return err
		}
		if len(spaces) != 0 {
			return fmt.Errorf("expected only personal workspace %d, found %d workspaces", types.SingleSpaceID, len(spaces))
		}
		svc := NewSpaceService(repo)
		_, err = svc.CreateSpace(ctx, &types.Space{
			ID: types.SingleSpaceID, Name: "Personal workspace", Description: "Personal entry", Status: "active",
		})
		if err != nil {
			return err
		}
		_, err = repository.EnsureSystemLibraries(ctx, tx, types.SingleSpaceID, "")
		return err
	})
}

// EnsureSystemLibraries repairs a missing library before serving requests.
func EnsureSystemLibraries(ctx context.Context, db *gorm.DB) error {
	var user types.User
	err := db.WithContext(ctx).Where("space_id = ?", types.SingleSpaceID).First(&user).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	// Registration creates the first library together with its owner.
	if err == gorm.ErrRecordNotFound {
		_, err = repository.EnsureSystemLibraries(ctx, db, types.SingleSpaceID, "")
		return err
	}
	_, err = repository.EnsureSystemLibraries(ctx, db, types.SingleSpaceID, user.ID)
	return err
}
