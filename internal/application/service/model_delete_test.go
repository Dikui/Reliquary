package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type stubLibraryRepoForModelDelete struct {
	usages   []types.ModelUsageResource
	count    *int64
	usageErr error
}

func (s *stubLibraryRepoForModelDelete) CreateLibrary(context.Context, *types.Library) error {
	return nil
}
func (s *stubLibraryRepoForModelDelete) GetLibraryByID(context.Context, string) (*types.Library, error) {
	return nil, nil
}
func (s *stubLibraryRepoForModelDelete) GetLibraryByIDAndSpace(context.Context, string, uint64) (*types.Library, error) {
	return nil, nil
}
func (s *stubLibraryRepoForModelDelete) GetLibraryByIDs(context.Context, []string) ([]*types.Library, error) {
	return nil, nil
}
func (s *stubLibraryRepoForModelDelete) GetSystemLibraryByRole(context.Context, uint64, ...string) (*types.Library, error) {
	return nil, nil
}
func (s *stubLibraryRepoForModelDelete) UpdateLibrary(context.Context, *types.Library) error {
	return nil
}
func (s *stubLibraryRepoForModelDelete) CountByModelID(context.Context, uint64, string) (int64, error) {
	if s.count != nil {
		return *s.count, s.usageErr
	}
	return int64(len(s.usages)), s.usageErr
}

func (s *stubLibraryRepoForModelDelete) ListModelUsages(
	context.Context, uint64, string,
) ([]types.ModelUsageResource, error) {
	return s.usages, s.usageErr
}

type stubModelRepoForDelete struct {
	model  *types.Model
	delete func(id string) error
	update func(model *types.Model) error
}

func (s *stubModelRepoForDelete) Create(context.Context, *types.Model) error { return nil }
func (s *stubModelRepoForDelete) GetByID(_ context.Context, _ uint64, id string) (*types.Model, error) {
	if s.model != nil && s.model.ID == id {
		return s.model, nil
	}
	return nil, nil
}
func (s *stubModelRepoForDelete) List(context.Context, uint64, types.ModelType, types.ModelSource) ([]*types.Model, error) {
	return nil, nil
}
func (s *stubModelRepoForDelete) Update(_ context.Context, model *types.Model) error {
	if s.update != nil {
		return s.update(model)
	}
	return nil
}
func (s *stubModelRepoForDelete) Delete(_ context.Context, _ uint64, id string) error {
	if s.delete != nil {
		return s.delete(id)
	}
	return nil
}
func (s *stubModelRepoForDelete) ClearDefaultByType(context.Context, uint, types.ModelType, string) error {
	return nil
}

func TestDeleteModel_RejectsWhenReferenced(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	modelID := "model-in-use"

	svc := NewModelService(
		&stubModelRepoForDelete{model: &types.Model{ID: modelID, SpaceID: 1}},
		&stubLibraryRepoForModelDelete{usages: []types.ModelUsageResource{
			{ID: "library-1", Name: "Product docs", Bindings: []types.ModelUsageBinding{types.ModelUsageBinding("generation_model_tier")}},
			{ID: "library-2", Name: "Engineering", Bindings: []types.ModelUsageBinding{types.ModelUsageBinding("generation_model_tier")}},
		}},
		nil, nil, nil,
	)

	err := svc.DeleteModel(ctx, modelID)
	require.Error(t, err)
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrModelInUse, appErr.Code)
	assert.Contains(t, appErr.Message, "2 library(s)")
	details, ok := appErr.Details.(types.ModelUsageDetails)
	require.True(t, ok)
	require.Len(t, details.Libraries, 2)
	assert.Equal(t, int64(2), details.LibraryTotal)
	assert.Equal(t, "Product docs", details.Libraries[0].Name)
	assert.Equal(t, []types.ModelUsageBinding{types.ModelUsageBinding("generation_model_tier")}, details.Libraries[0].Bindings)
	assert.Equal(t, "Engineering", details.Libraries[1].Name)
	assert.Equal(t, []types.ModelUsageBinding{types.ModelUsageBinding("generation_model_tier")}, details.Libraries[1].Bindings)
}

func TestDeleteModel_SucceedsWhenUnreferenced(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	modelID := "free-model"
	deleted := false

	svc := NewModelService(
		&stubModelRepoForDelete{
			model: &types.Model{ID: modelID, SpaceID: 1},
			delete: func(id string) error {
				assert.Equal(t, modelID, id)
				deleted = true
				return nil
			},
		},
		&stubLibraryRepoForModelDelete{},
		nil, nil, nil,
	)

	require.NoError(t, svc.DeleteModel(ctx, modelID))
	assert.True(t, deleted)
}

func TestGetModelUsageDetails_NormalizesEmptyCollections(t *testing.T) {
	svc := NewModelService(
		&stubModelRepoForDelete{},
		&stubLibraryRepoForModelDelete{},
		nil, nil, nil,
	)

	details, err := svc.(*modelService).getModelUsageDetails(context.Background(), 1, "unused-model")
	require.NoError(t, err)
	assert.NotNil(t, details.Libraries)
	assert.Equal(t, int64(0), details.LibraryTotal)
}

func TestDeleteModel_DoesNotDeleteWhenUsageLookupFails(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	modelID := "lookup-failure"
	wantErr := errors.New("usage lookup failed")
	deleted := false

	svc := NewModelService(
		&stubModelRepoForDelete{
			model: &types.Model{ID: modelID, SpaceID: 1},
			delete: func(string) error {
				deleted = true
				return nil
			},
		},
		&stubLibraryRepoForModelDelete{usageErr: wantErr},
		nil, nil, nil,
	)

	err := svc.DeleteModel(ctx, modelID)
	require.ErrorIs(t, err, wantErr)
	assert.False(t, deleted)
}

func TestDeleteModel_ReportsUntruncatedTotalsWhenListsAreCapped(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	modelID := "popular-embedding"
	libraryTotal := int64(80)

	svc := NewModelService(
		&stubModelRepoForDelete{model: &types.Model{ID: modelID, SpaceID: 1}},
		&stubLibraryRepoForModelDelete{
			count: &libraryTotal,
			usages: []types.ModelUsageResource{
				{ID: "library-1", Name: "Alpha", Bindings: []types.ModelUsageBinding{types.ModelUsageBindingEmbeddingModel}},
				{ID: "library-2", Name: "Beta", Bindings: []types.ModelUsageBinding{types.ModelUsageBindingEmbeddingModel}},
			},
		},

		nil, nil, nil,
	)

	err := svc.DeleteModel(ctx, modelID)
	require.Error(t, err)
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok)
	assert.Contains(t, appErr.Message, "80 library(s)")
	details, ok := appErr.Details.(types.ModelUsageDetails)
	require.True(t, ok)
	assert.Equal(t, int64(80), details.LibraryTotal)
	assert.Len(t, details.Libraries, 2)
}

type stubSpaceServiceForModelDelete struct {
	space *types.Space
}

func (s *stubSpaceServiceForModelDelete) CreateSpace(context.Context, *types.Space) (*types.Space, error) {
	return nil, nil
}
func (s *stubSpaceServiceForModelDelete) GetSpaceByID(context.Context, uint64) (*types.Space, error) {
	return s.space, nil
}
func (s *stubSpaceServiceForModelDelete) GetSpacesByIDs(context.Context, []uint64) (map[uint64]*types.Space, error) {
	return nil, nil
}
func (s *stubSpaceServiceForModelDelete) ListSpaces(context.Context) ([]*types.Space, error) {
	return nil, nil
}
func (s *stubSpaceServiceForModelDelete) UpdateSpace(context.Context, *types.Space) (*types.Space, error) {
	return nil, nil
}
func (s *stubSpaceServiceForModelDelete) DeleteSpace(context.Context, uint64) error { return nil }
func (s *stubSpaceServiceForModelDelete) ListAllSpaces(context.Context) ([]*types.Space, error) {
	return nil, nil
}

func (s *stubSpaceServiceForModelDelete) SearchSpaces(context.Context, string, uint64, int, int) ([]*types.Space, int64, error) {
	return nil, 0, nil
}
func (s *stubSpaceServiceForModelDelete) GetSpaceByIDForUser(context.Context, uint64, string) (*types.Space, error) {
	return s.space, nil
}

func TestFormatModelInUseMessage(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		"model is used by 1 library(s); reconfigure or remove those references before deleting",
		formatModelInUseMessage(1),
	)
	assert.Equal(t,
		"model is used by 2 library(s); reconfigure or remove those references before deleting",
		formatModelInUseMessage(2),
	)
	assert.Equal(t,
		"model is used by 1 library(s); reconfigure or remove those references before deleting",
		formatModelInUseMessage(1),
	)

}

func (r *stubLibraryRepoForModelDelete) UpdateLibraryPin(context.Context, *types.Library) error {
	return nil
}

func (s *stubLibraryRepoForModelDelete) ListLibraries(context.Context, uint64) ([]*types.Library, error) {
	return nil, nil
}
