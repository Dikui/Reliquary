package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// fakeKBRepo is the smallest KnowledgeBaseRepository needed by
// CreateKnowledgeBase tests. It stores rows in a map
// keyed by ID. No tenant scoping is applied — the tested paths already
// enforce that explicitly.
type fakeKBRepo struct {
	rows      map[string]*types.KnowledgeBase
	createErr error
}

func newFakeKBRepo() *fakeKBRepo { return &fakeKBRepo{rows: map[string]*types.KnowledgeBase{}} }

func (r *fakeKBRepo) CreateKnowledgeBase(_ context.Context, kb *types.KnowledgeBase) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.rows[kb.ID] = kb
	return nil
}
func (r *fakeKBRepo) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	return r.rows[id], nil
}
func (r *fakeKBRepo) GetKnowledgeBaseByIDAndTenant(_ context.Context, id string, tenantID uint64) (*types.KnowledgeBase, error) {
	kb := r.rows[id]
	if kb == nil || kb.TenantID != tenantID {
		return nil, stderrors.New("not found")
	}
	return kb, nil
}
func (r *fakeKBRepo) GetKnowledgeBaseByIDs(_ context.Context, _ []string) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (r *fakeKBRepo) ListKnowledgeBases(_ context.Context) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (r *fakeKBRepo) ListKnowledgeBasesByTenantID(_ context.Context, tenantID uint64) ([]*types.KnowledgeBase, error) {
	rows := make([]*types.KnowledgeBase, 0, len(r.rows))
	for _, kb := range r.rows {
		if kb != nil && kb.TenantID == tenantID {
			rows = append(rows, kb)
		}
	}
	return rows, nil
}
func (r *fakeKBRepo) UpdateKnowledgeBase(_ context.Context, _ *types.KnowledgeBase) error {
	return nil
}
func (r *fakeKBRepo) DeleteKnowledgeBase(_ context.Context, _ string) error { return nil }
func (r *fakeKBRepo) TogglePinKnowledgeBase(_ context.Context, _ string, _ uint64) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (r *fakeKBRepo) CountByModelID(_ context.Context, _ uint64, _ string) (int64, error) {
	return 0, nil
}

func (r *fakeKBRepo) ListModelUsages(_ context.Context, _ uint64, _ string) ([]types.ModelUsageResource, error) {
	return []types.ModelUsageResource{}, nil
}

// Force compile-time conformance check so any future interface change
// surfaces as a build error here rather than at usage site.
var _ interfaces.KnowledgeBaseRepository = (*fakeKBRepo)(nil)

func newPR3KBService(repo *fakeKBRepo) *knowledgeBaseService {
	return &knowledgeBaseService{repo: repo}
}

func ctxWithTenant(tenantID uint64) context.Context {
	return context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
}

func TestDuplicateKnowledgeBase_CreatesSettingsOnlyDuplicate(t *testing.T) {
	repo := newFakeKBRepo()
	source := &types.KnowledgeBase{ID: "src",
		Name:        "Source KB",
		Type:        types.KnowledgeBaseTypeDocument,
		Description: "source description",
		TenantID:    1,
		CreatorID:   "source-owner",
		ChunkingConfig: types.ChunkingConfig{
			ChunkSize:       800,
			ChunkOverlap:    80,
			ChunkOverlapSet: true,
			Separators:      []string{"\n\n", "."},
			ParserEngineRules: []types.ParserEngineRule{{
				FileTypes: []string{"pdf"},
				Engine:    "anydoc",
			}},
		},
		ImageProcessingConfig: types.ImageProcessingConfig{ModelID: "vlm-image"},
		EmbeddingModelID:      "embed-1",
		SummaryModelID:        "summary-1",
		VLMConfig:             types.VLMConfig{Enabled: true, ModelID: "vlm-1"},

		QuestionGenerationConfig: &types.QuestionGenerationConfig{Enabled: true, QuestionCount: 5},
		IndexingStrategy: types.IndexingStrategy{
			VectorEnabled:  true,
			KeywordEnabled: true,
		},
		IsPinned:        true,
		KnowledgeCount:  9,
		ChunkCount:      27,
		IsProcessing:    true,
		ProcessingCount: 2,
		CreatorName:     "Source Owner",
	}
	repo.rows["src"] = source
	svc := newPR3KBService(repo)
	ctx := context.WithValue(ctxWithTenant(1), types.UserIDContextKey, "copy-user")

	target, err := svc.DuplicateKnowledgeBase(ctx, "src")
	require.NoError(t, err)

	require.NotEmpty(t, target.ID)
	assert.Equal(t, uint64(1), target.TenantID)
	assert.Equal(t, "copy-user", target.CreatorID)
	assert.Equal(t, "Source KB 副本", target.Name)
	assert.Equal(t, source.Description, target.Description)
	assert.Equal(t, source.ChunkingConfig, target.ChunkingConfig)
	assert.Equal(t, source.ImageProcessingConfig, target.ImageProcessingConfig)
	assert.Equal(t, source.EmbeddingModelID, target.EmbeddingModelID)
	assert.Equal(t, source.SummaryModelID, target.SummaryModelID)
	assert.Equal(t, source.VLMConfig, target.VLMConfig)
	require.NotNil(t, target.QuestionGenerationConfig)
	assert.Equal(t, 5, target.QuestionGenerationConfig.QuestionCount)
	assert.Equal(t, source.IndexingStrategy, target.IndexingStrategy)

	assert.False(t, target.IsPinned)
	assert.Nil(t, target.PinnedAt)
	assert.Zero(t, target.KnowledgeCount)
	assert.Zero(t, target.ChunkCount)
	assert.False(t, target.IsProcessing)
	assert.Zero(t, target.ProcessingCount)
	assert.Empty(t, target.CreatorName)
	require.Same(t, target, repo.rows[target.ID])
}

func TestDuplicateKnowledgeBase_UsesDistinctNameWhenDuplicateExists(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["src"] = &types.KnowledgeBase{ID: "src",
		Name:     "Source KB",
		Type:     types.KnowledgeBaseTypeDocument,
		TenantID: 1,
	}
	repo.rows["existing-copy"] = &types.KnowledgeBase{ID: "existing-copy",
		Name:     "Source KB 副本",
		Type:     types.KnowledgeBaseTypeDocument,
		TenantID: 1,
	}
	svc := newPR3KBService(repo)

	target, err := svc.DuplicateKnowledgeBase(ctxWithTenant(1), "src")
	require.NoError(t, err)

	assert.Equal(t, "Source KB 副本 2", target.Name)
}

func TestDuplicateKnowledgeBase_UsesLocalizedEnglishSuffix(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["src"] = &types.KnowledgeBase{ID: "src",
		Name:     "Source KB",
		Type:     types.KnowledgeBaseTypeDocument,
		TenantID: 1,
	}
	svc := newPR3KBService(repo)
	ctx := context.WithValue(ctxWithTenant(1), types.LanguageContextKey, "en-US")

	target, err := svc.DuplicateKnowledgeBase(ctx, "src")
	require.NoError(t, err)

	assert.Equal(t, "Source KB Copy", target.Name)
}

func ctxWithTenantInfo(tenantID uint64) context.Context {
	return context.WithValue(ctxWithTenant(tenantID), types.TenantInfoContextKey, &types.Tenant{ID: tenantID})
}

func (r *fakeKBRepo) UpdateKnowledgeBasePin(context.Context, *types.KnowledgeBase) error {
	return nil
}
