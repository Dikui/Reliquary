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

type stubKBRepoForModelDelete struct {
	usages   []types.ModelUsageResource
	count    *int64
	usageErr error
}

func (s *stubKBRepoForModelDelete) CreateKnowledgeBase(context.Context, *types.KnowledgeBase) error {
	return nil
}
func (s *stubKBRepoForModelDelete) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *stubKBRepoForModelDelete) GetKnowledgeBaseByIDAndTenant(context.Context, string, uint64) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *stubKBRepoForModelDelete) GetKnowledgeBaseByIDs(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *stubKBRepoForModelDelete) ListKnowledgeBases(context.Context) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *stubKBRepoForModelDelete) ListKnowledgeBasesByTenantID(context.Context, uint64) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *stubKBRepoForModelDelete) UpdateKnowledgeBase(context.Context, *types.KnowledgeBase) error {
	return nil
}
func (s *stubKBRepoForModelDelete) DeleteKnowledgeBase(context.Context, string) error { return nil }
func (s *stubKBRepoForModelDelete) CountByModelID(context.Context, uint64, string) (int64, error) {
	if s.count != nil {
		return *s.count, s.usageErr
	}
	return int64(len(s.usages)), s.usageErr
}

func (s *stubKBRepoForModelDelete) ListModelUsages(
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
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	modelID := "model-in-use"

	svc := NewModelService(
		&stubModelRepoForDelete{model: &types.Model{ID: modelID, TenantID: 1}},
		&stubKBRepoForModelDelete{usages: []types.ModelUsageResource{
			{ID: "kb-1", Name: "Product docs", Bindings: []types.ModelUsageBinding{types.ModelUsageBindingVLMModel}},
			{ID: "kb-2", Name: "Engineering", Bindings: []types.ModelUsageBinding{types.ModelUsageBindingVLMModel}},
		}},
		nil, nil, nil,
	)

	err := svc.DeleteModel(ctx, modelID)
	require.Error(t, err)
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrModelInUse, appErr.Code)
	assert.Contains(t, appErr.Message, "2 knowledge base(s)")
	details, ok := appErr.Details.(types.ModelUsageDetails)
	require.True(t, ok)
	require.Len(t, details.KnowledgeBases, 2)
	assert.Equal(t, int64(2), details.KnowledgeBaseTotal)
	assert.Equal(t, "Product docs", details.KnowledgeBases[0].Name)
	assert.Equal(t, []types.ModelUsageBinding{types.ModelUsageBindingVLMModel}, details.KnowledgeBases[0].Bindings)
	assert.Equal(t, "Engineering", details.KnowledgeBases[1].Name)
	assert.Equal(t, []types.ModelUsageBinding{types.ModelUsageBindingVLMModel}, details.KnowledgeBases[1].Bindings)
}

func TestDeleteModel_SucceedsWhenUnreferenced(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	modelID := "free-model"
	deleted := false

	svc := NewModelService(
		&stubModelRepoForDelete{
			model: &types.Model{ID: modelID, TenantID: 1},
			delete: func(id string) error {
				assert.Equal(t, modelID, id)
				deleted = true
				return nil
			},
		},
		&stubKBRepoForModelDelete{},
		nil, nil, nil,
	)

	require.NoError(t, svc.DeleteModel(ctx, modelID))
	assert.True(t, deleted)
}

func TestGetModelUsageDetails_NormalizesEmptyCollections(t *testing.T) {
	svc := NewModelService(
		&stubModelRepoForDelete{},
		&stubKBRepoForModelDelete{},
		nil, nil, nil,
	)

	details, err := svc.(*modelService).getModelUsageDetails(context.Background(), 1, "unused-model")
	require.NoError(t, err)
	assert.NotNil(t, details.KnowledgeBases)
	assert.Equal(t, int64(0), details.KnowledgeBaseTotal)
}

func TestDeleteModel_DoesNotDeleteWhenUsageLookupFails(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	modelID := "lookup-failure"
	wantErr := errors.New("usage lookup failed")
	deleted := false

	svc := NewModelService(
		&stubModelRepoForDelete{
			model: &types.Model{ID: modelID, TenantID: 1},
			delete: func(string) error {
				deleted = true
				return nil
			},
		},
		&stubKBRepoForModelDelete{usageErr: wantErr},
		nil, nil, nil,
	)

	err := svc.DeleteModel(ctx, modelID)
	require.ErrorIs(t, err, wantErr)
	assert.False(t, deleted)
}

func TestDeleteModel_ReportsUntruncatedTotalsWhenListsAreCapped(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	modelID := "popular-embedding"
	kbTotal := int64(80)

	svc := NewModelService(
		&stubModelRepoForDelete{model: &types.Model{ID: modelID, TenantID: 1}},
		&stubKBRepoForModelDelete{
			count: &kbTotal,
			usages: []types.ModelUsageResource{
				{ID: "kb-1", Name: "Alpha", Bindings: []types.ModelUsageBinding{types.ModelUsageBindingEmbeddingModel}},
				{ID: "kb-2", Name: "Beta", Bindings: []types.ModelUsageBinding{types.ModelUsageBindingEmbeddingModel}},
			},
		},

		nil, nil, nil,
	)

	err := svc.DeleteModel(ctx, modelID)
	require.Error(t, err)
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok)
	assert.Contains(t, appErr.Message, "80 knowledge base(s)")
	details, ok := appErr.Details.(types.ModelUsageDetails)
	require.True(t, ok)
	assert.Equal(t, int64(80), details.KnowledgeBaseTotal)
	assert.Len(t, details.KnowledgeBases, 2)
}

type stubTenantServiceForModelDelete struct {
	tenant *types.Tenant
}

func (s *stubTenantServiceForModelDelete) CreateTenant(context.Context, *types.Tenant) (*types.Tenant, error) {
	return nil, nil
}
func (s *stubTenantServiceForModelDelete) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return s.tenant, nil
}
func (s *stubTenantServiceForModelDelete) GetTenantsByIDs(context.Context, []uint64) (map[uint64]*types.Tenant, error) {
	return nil, nil
}
func (s *stubTenantServiceForModelDelete) ListTenants(context.Context) ([]*types.Tenant, error) {
	return nil, nil
}
func (s *stubTenantServiceForModelDelete) UpdateTenant(context.Context, *types.Tenant) (*types.Tenant, error) {
	return nil, nil
}
func (s *stubTenantServiceForModelDelete) DeleteTenant(context.Context, uint64) error { return nil }
func (s *stubTenantServiceForModelDelete) ListAllTenants(context.Context) ([]*types.Tenant, error) {
	return nil, nil
}

func (s *stubTenantServiceForModelDelete) SearchTenants(context.Context, string, uint64, int, int) ([]*types.Tenant, int64, error) {
	return nil, 0, nil
}
func (s *stubTenantServiceForModelDelete) GetTenantByIDForUser(context.Context, uint64, string) (*types.Tenant, error) {
	return s.tenant, nil
}

func TestFormatModelInUseMessage(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		"model is used by 1 knowledge base(s); reconfigure or remove those references before deleting",
		formatModelInUseMessage(1),
	)
	assert.Equal(t,
		"model is used by 2 knowledge base(s); reconfigure or remove those references before deleting",
		formatModelInUseMessage(2),
	)
	assert.Equal(t,
		"model is used by 1 knowledge base(s); reconfigure or remove those references before deleting",
		formatModelInUseMessage(1),
	)

}

func (r *stubKBRepoForModelDelete) UpdateKnowledgeBasePin(context.Context, *types.KnowledgeBase) error {
	return nil
}
