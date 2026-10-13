package service

import (
	"context"
	stderrors "errors"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// fakeLibraryRepo stores settings for service validation tests.
type fakeLibraryRepo struct {
	rows      map[string]*types.Library
	createErr error
}

func newFakeLibraryRepo() *fakeLibraryRepo {
	return &fakeLibraryRepo{rows: map[string]*types.Library{}}
}

func (r *fakeLibraryRepo) CreateLibrary(_ context.Context, library *types.Library) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.rows[library.ID] = library
	return nil
}
func (r *fakeLibraryRepo) GetSystemLibraryByRole(ctx context.Context, spaceID uint64, roles ...string) (*types.Library, error) {
	for _, library := range r.rows {
		if library.SpaceID == spaceID {
			return library, nil
		}
	}
	return nil, stderrors.New("not found")
}
func (r *fakeLibraryRepo) GetLibraryByID(_ context.Context, id string) (*types.Library, error) {
	return r.rows[id], nil
}
func (r *fakeLibraryRepo) GetLibraryByIDAndSpace(_ context.Context, id string, spaceID uint64) (*types.Library, error) {
	library := r.rows[id]
	if library == nil || library.SpaceID != spaceID {
		return nil, stderrors.New("not found")
	}
	return library, nil
}
func (r *fakeLibraryRepo) GetLibraryByIDs(_ context.Context, _ []string) ([]*types.Library, error) {
	return nil, nil
}
func (r *fakeLibraryRepo) UpdateLibrary(_ context.Context, _ *types.Library) error {
	return nil
}
func (r *fakeLibraryRepo) CountByModelID(_ context.Context, _ uint64, _ string) (int64, error) {
	return 0, nil
}

func (r *fakeLibraryRepo) ListModelUsages(_ context.Context, _ uint64, _ string) ([]types.ModelUsageResource, error) {
	return []types.ModelUsageResource{}, nil
}

// Force compile-time conformance check so any future interface change
// surfaces as a build error here rather than at usage site.
var _ interfaces.LibraryRepository = (*fakeLibraryRepo)(nil)

func (r *fakeLibraryRepo) ListLibraries(ctx context.Context, spaceID uint64) ([]*types.Library, error) {
	result := []*types.Library{}
	for _, l := range r.rows {
		if l.SpaceID == spaceID {
			result = append(result, l)
		}
	}
	return result, nil
}
