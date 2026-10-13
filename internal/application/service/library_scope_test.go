package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestSingleLibraryRegistrationStartupAndConstraint(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID)
	require.NoError(t, EnsurePersonalSpace(ctx, db))
	spaces := NewSpaceService(repository.NewSpaceRepository(db))
	users := NewUserService(&config.Config{}, repository.NewUserRepository(db), repository.NewAuthTokenRepository(db), spaces)
	user, err := users.Register(ctx, &types.RegisterRequest{Username: "personal", Email: "library@example.test", Password: "initial-password"})
	require.NoError(t, err)
	libraryRepo := repository.NewLibraryRepository(db)
	library, err := libraryRepo.GetLibraryBySpaceID(ctx, types.SingleSpaceID)
	require.NoError(t, err)
	require.Equal(t, user.ID, library.CreatorID)
	require.Contains(t, []string{"库", "Library"}, library.Name)
	require.Equal(t, types.LibraryTypeDocument, library.Type)
	for range 3 {
		require.NoError(t, EnsureSingleLibrary(ctx, db))
	}
	same, err := libraryRepo.GetLibraryBySpaceID(ctx, types.SingleSpaceID)
	require.NoError(t, err)
	require.Equal(t, library.ID, same.ID)
	second := &types.Library{ID: "second", SpaceID: types.SingleSpaceID, Name: "Second"}
	second.EnsureDefaults()
	require.ErrorContains(t, db.Create(second).Error, "idx_libraries_active_space")
	var count int64
	require.NoError(t, db.Model(&types.Library{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Delete(library).Error)
	require.NoError(t, EnsureSingleLibrary(ctx, db))
	repaired, err := libraryRepo.GetLibraryBySpaceID(ctx, types.SingleSpaceID)
	require.NoError(t, err)
	require.NotEqual(t, library.ID, repaired.ID)
	require.Equal(t, user.ID, repaired.CreatorID)
	require.NoError(t, EnsureSingleLibrary(ctx, db))
	require.NoError(t, db.Model(&types.Library{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Unscoped().Model(&types.Library{}).Count(&count).Error)
	require.EqualValues(t, 2, count, "the partial unique index permits replacement of a deleted library")
}
