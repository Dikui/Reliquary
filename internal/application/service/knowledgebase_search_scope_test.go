package service

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"testing"
)

func TestPickPrimary(t *testing.T) {
	t.Parallel()
	kbs := []*types.KnowledgeBase{
		{ID: "kb-a"}, {ID: "kb-b"}, {ID: "kb-c"},
	}
	if pickPrimary(kbs, "kb-b").ID != "kb-b" {
		t.Errorf("hit case failed")
	}
	// Miss returns nil rather than falling back to kbs[0]; the helper's
	// godoc explains the rationale (surface caller bugs, avoid leaking
	// unintended KB metadata).
	if pickPrimary(kbs, "kb-missing") != nil {
		t.Errorf("miss should return nil")
	}
	if pickPrimary(nil, "any") != nil {
		t.Errorf("nil slice should return nil")
	}
}

func TestTotalHits(t *testing.T) {
	t.Parallel()
	got := totalHits([]*types.RetrieveResult{
		{Results: []*types.IndexWithScore{{}, {}}},
		{Results: []*types.IndexWithScore{{}}},
		{Results: nil},
	})
	if got != 3 {
		t.Errorf("want 3, got %d", got)
	}
	if totalHits(nil) != 0 {
		t.Errorf("nil → 0")
	}
}

type fakeModelSvcForKeys struct {
	byID map[string]*types.Model // modelID → model
	interfaces.ModelService
}

func (f *fakeModelSvcForKeys) GetModelByID(ctx context.Context, id string) (*types.Model, error) {
	if m, ok := f.byID[id]; ok {
		return m, nil
	}
	return nil, fmt.Errorf("model %s not found", id)
}

func buildModel(id, name, baseURL string) *types.Model {
	return &types.Model{
		ID:   id,
		Name: name,
		Parameters: types.ModelParameters{
			BaseURL: baseURL,
		},
	}
}

func newKBSvcForValidate(modelService interfaces.ModelService) *knowledgeBaseService {
	return &knowledgeBaseService{
		modelService: modelService,
	}
}

func TestValidateSameEmbeddingModel_SingleKB_NoOp(t *testing.T) {
	t.Parallel()
	svc := newKBSvcForValidate(&fakeModelSvcForKeys{})
	err := svc.validateSameEmbeddingModel(context.Background(), []*types.KnowledgeBase{
		{ID: "kb-1", EmbeddingModelID: "m-1", TenantID: 1},
	})
	assert.NoError(t, err)
}

func TestValidateSameEmbeddingModel_SameIdentity_OK(t *testing.T) {
	t.Parallel()
	models := map[string]*types.Model{
		"m-1": buildModel("m-1", "bge-m3", "http://embed-a"),
		"m-2": buildModel("m-2", "bge-m3", "http://embed-a"), // same identity key
	}
	svc := newKBSvcForValidate(&fakeModelSvcForKeys{byID: models})
	err := svc.validateSameEmbeddingModel(context.Background(), []*types.KnowledgeBase{
		{ID: "kb-1", EmbeddingModelID: "m-1", TenantID: 1},
		{ID: "kb-2", EmbeddingModelID: "m-2", TenantID: 2}, // cross-tenant
	})
	assert.NoError(t, err)
}

func TestValidateSameEmbeddingModel_DifferentIdentity_400(t *testing.T) {
	t.Parallel()
	models := map[string]*types.Model{
		"m-1": buildModel("m-1", "bge-m3", "http://embed-a"),
		"m-2": buildModel("m-2", "openai-3", "http://embed-b"), // different
	}
	svc := newKBSvcForValidate(&fakeModelSvcForKeys{byID: models})
	err := svc.validateSameEmbeddingModel(context.Background(), []*types.KnowledgeBase{
		{ID: "kb-1", EmbeddingModelID: "m-1", TenantID: 1},
		{ID: "kb-2", EmbeddingModelID: "m-2", TenantID: 1},
	})
	app, ok := apperrors.IsAppError(err)
	require.True(t, ok, "expected AppError, got %T", err)
	assert.Equal(t, apperrors.ErrBadRequest, app.Code)
}

func TestValidateSameEmbeddingModel_Unindexed_Tolerated(t *testing.T) {
	t.Parallel()
	models := map[string]*types.Model{
		"m-1": buildModel("m-1", "bge-m3", "http://embed-a"),
	}
	svc := newKBSvcForValidate(&fakeModelSvcForKeys{byID: models})
	// One vector KB + one unindexed (empty EmbeddingModelID) → tolerated.
	err := svc.validateSameEmbeddingModel(context.Background(), []*types.KnowledgeBase{
		{ID: "kb-vec", EmbeddingModelID: "m-1", TenantID: 1},
		{ID: "kb-unindexed", EmbeddingModelID: "", TenantID: 1},
	})
	assert.NoError(t, err)
}

func TestValidateSameEmbeddingModel_LogInjection_Sanitized(t *testing.T) {
	t.Parallel()
	// BaseURL with CRLF — SanitizeForLog should keep us safe.
	models := map[string]*types.Model{
		"m-1": buildModel("m-1", "bge-m3", "http://a\r\nfoo"),
		"m-2": buildModel("m-2", "openai", "http://b\r\nbar"),
	}
	svc := newKBSvcForValidate(&fakeModelSvcForKeys{byID: models})
	err := svc.validateSameEmbeddingModel(context.Background(), []*types.KnowledgeBase{
		{ID: "kb-1", EmbeddingModelID: "m-1", TenantID: 1},
		{ID: "kb-2", EmbeddingModelID: "m-2", TenantID: 1},
	})
	// We only verify the 400 still happens; sanitize is exercised in the
	// hot path of WarnWithFields and is unit-tested in internal/utils.
	app, ok := apperrors.IsAppError(err)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrBadRequest, app.Code)
}

func TestAuthorizeKBAccess_SameTenantAllPass(t *testing.T) {
	t.Parallel()
	s := &knowledgeBaseService{}
	kbs := []*types.KnowledgeBase{
		{ID: "kb-1", TenantID: 7},
		{ID: "kb-2", TenantID: 7},
	}
	err := s.authorizeKBAccess(ctxWithTenantForAuth(7), kbs, 7)
	require.NoError(t, err)
}

func TestAuthorizeKBAccess_ForeignTenantForbidden(t *testing.T) {
	t.Parallel()
	s := &knowledgeBaseService{}
	kbs := []*types.KnowledgeBase{
		{ID: "kb-foreign", TenantID: 99},
	}
	err := s.authorizeKBAccess(ctxWithTenantForAuth(7), kbs, 7)
	require.Error(t, err)
	app, ok := apperrors.IsAppError(err)
	require.True(t, ok, "expected typed AppError, got %T", err)
	assert.Equal(t, apperrors.ErrForbidden, app.Code,
		"foreign knowledge bases must be rejected")
}

func TestAuthorizeKBAccess_EmptyKBs_OK(t *testing.T) {
	t.Parallel()
	s := &knowledgeBaseService{}
	err := s.authorizeKBAccess(ctxWithTenantForAuth(7), nil, 7)
	require.NoError(t, err)
}

func ctxWithTenantForAuth(tenantID uint64) context.Context {
	return context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
}
