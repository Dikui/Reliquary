package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/models/rerank"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type searchTargetKnowledgeService struct {
	interfaces.KnowledgeService
	knowledges []*types.Knowledge
}

func (s *searchTargetKnowledgeService) GetKnowledgeBatch(_ context.Context, _ uint64, ids []string) ([]*types.Knowledge, error) {
	allowed := make(map[string]bool, len(ids))
	for _, id := range ids {
		allowed[id] = true
	}
	var out []*types.Knowledge
	for _, knowledge := range s.knowledges {
		if allowed[knowledge.ID] {
			out = append(out, knowledge)
		}
	}
	return out, nil
}

// This retrieval assertion previously lived with the deleted session tests.
func TestBuildSearchTargets_ExplicitKnowledgeScopeDisablesRecallThresholds(t *testing.T) {
	svc := &knowledgeSearchService{knowledgeService: &searchTargetKnowledgeService{
		knowledges: []*types.Knowledge{{ID: "doc-1", TenantID: 100, KnowledgeBaseID: "doc-kb"}},
	}}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(100))
	targets, err := svc.buildSearchTargets(ctx, 100, "doc-kb", []string{"doc-1"})

	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, types.SearchTargetTypeKnowledge, targets[0].Type)
	assert.Equal(t, []string{"doc-1"}, targets[0].KnowledgeIDs)
	assert.True(t, targets[0].DisableRecallThresholds)
}

func TestBuildSearchTargetsUsesSoleKnowledgeBaseAndRejectsOtherArticleIdentity(t *testing.T) {
	svc := &knowledgeSearchService{knowledgeService: &searchTargetKnowledgeService{knowledges: []*types.Knowledge{
		{ID: "article", TenantID: 1, KnowledgeBaseID: "only-kb"},
		{ID: "orphan", TenantID: 1, KnowledgeBaseID: "missing-kb"},
	}}}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	targets, err := svc.buildSearchTargets(ctx, 1, "only-kb", nil)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, "only-kb", targets[0].KnowledgeBaseID)
	targets, err = svc.buildSearchTargets(ctx, 1, "only-kb", []string{"article", "orphan"})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, []string{"article"}, targets[0].KnowledgeIDs)
}

// These stubs exercise model selection through the actual retrieval and rerank stages.
type soleRerankKBService struct {
	interfaces.KnowledgeBaseService
	kb    *types.KnowledgeBase
	empty bool
}

func (s *soleRerankKBService) GetKnowledgeBase(context.Context) (*types.KnowledgeBase, error) {
	return s.kb, nil
}
func (s *soleRerankKBService) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return []*types.KnowledgeBase{s.kb}, nil
}
func (s *soleRerankKBService) ResolveEmbeddingModelKeys(context.Context, []*types.KnowledgeBase) map[string]string {
	return map[string]string{}
}
func (s *soleRerankKBService) HybridSearch(context.Context, string, types.SearchParams) ([]*types.SearchResult, error) {
	if s.empty {
		return nil, nil
	}
	return []*types.SearchResult{{ID: "chunk", KnowledgeID: "article", Content: "Complete article body used for model routing", Score: 0.8}}, nil
}

type soleRerankTenantService struct {
	interfaces.TenantService
	config *types.RetrievalConfig
}

func (s *soleRerankTenantService) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: 1, RetrievalConfig: s.config}, nil
}

type soleRerankModelService struct {
	interfaces.ModelService
	selected    string
	listCalls   int
	unavailable error
}

func (s *soleRerankModelService) GetRerankModel(_ context.Context, id string) (rerank.Reranker, error) {
	s.selected = id
	return nil, s.unavailable
}
func (s *soleRerankModelService) ListModels(context.Context) ([]*types.Model, error) {
	s.listCalls++
	return nil, errors.New("search must use its configured rerank tier")
}

func TestKnowledgeSearchUsesOnlySoleKnowledgeBaseRerankTier(t *testing.T) {
	var retrieval types.RetrievalConfig
	require.NoError(t, json.Unmarshal([]byte(`{"rerank_model_id":"removed-global-rerank","rerank_top_k":7,"rerank_threshold":0.4}`), &retrieval))
	encoded, err := json.Marshal(retrieval)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "rerank_model_id")
	kb := &types.KnowledgeBase{ID: "only-kb", TenantID: 1, ModelTiers: types.ModelTierConfig{Rerank: "selected-rerank"}}
	bases := &soleRerankKBService{kb: kb}
	sentinel := errors.New("selected model temporarily unavailable")
	models := &soleRerankModelService{unavailable: sentinel}
	svc := NewKnowledgeSearchService(bases, nil, &soleRerankTenantService{config: &retrieval}, models, nil, nil, nil)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	for _, id := range []string{"selected-rerank", "replacement-rerank"} {
		kb.ModelTiers.Rerank = id
		_, err = svc.SearchKnowledge(ctx, nil, "article")
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, id, models.selected)
		require.Zero(t, models.listCalls)
	}
	kb.ModelTiers.Rerank = ""
	bases.empty = true
	models.selected = ""
	results, err := svc.SearchKnowledge(ctx, nil, "article")
	require.NoError(t, err)
	require.Empty(t, results)
	require.Empty(t, models.selected)
	require.Zero(t, models.listCalls, "an unconfigured tier must not select a different model automatically")
}
