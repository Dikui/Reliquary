package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type teamLibraryRepo struct {
	interfaces.LibraryRepository
	rows []*types.Library
}

func (r *teamLibraryRepo) GetLibraryByID(_ context.Context, id string) (*types.Library, error) {
	for _, library := range r.rows {
		if library.ID == id {
			return library, nil
		}
	}
	return nil, repository.ErrLibraryNotFound
}

func (r *teamLibraryRepo) GetLibraryByIDs(context.Context, []string) ([]*types.Library, error) {
	return r.rows, nil
}

func TestLibraryLookupsRejectObjectsOutsideTeam(t *testing.T) {
	teamLibrary := &types.Library{ID: "team-library", SpaceID: types.SingleSpaceID, CreatorID: "another-member"}
	foreignLibrary := &types.Library{ID: "foreign-library", SpaceID: 2}
	svc := &libraryService{repo: &teamLibraryRepo{rows: []*types.Library{teamLibrary, foreignLibrary}}}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID)
	for _, get := range []func(context.Context, string) (*types.Library, error){svc.GetLibraryByID, svc.GetLibraryByIDOnly} {
		got, err := get(ctx, teamLibrary.ID)
		require.NoError(t, err)
		require.Equal(t, teamLibrary.ID, got.ID)
		_, err = get(ctx, foreignLibrary.ID)
		require.ErrorIs(t, err, repository.ErrLibraryNotFound)
	}
	got, err := svc.GetLibrariesByIDsOnly(ctx, []string{teamLibrary.ID, foreignLibrary.ID})
	require.NoError(t, err)
	require.Equal(t, []*types.Library{teamLibrary}, got)
}
