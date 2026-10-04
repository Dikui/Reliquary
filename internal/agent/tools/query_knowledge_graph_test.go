package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type stubKnowledgeBaseService struct {
	kb      *types.KnowledgeBase
	results []*types.SearchResult
}

func (s *stubKnowledgeBaseService) CreateKnowledgeBase(context.Context, *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func (s *stubKnowledgeBaseService) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func (s *stubKnowledgeBaseService) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) FillKnowledgeBaseCounts(context.Context, *types.KnowledgeBase) error {
	return nil
}

func (s *stubKnowledgeBaseService) ListKnowledgeBases(context.Context) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) ListKnowledgeBasesByTenantID(context.Context, uint64) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) UpdateKnowledgeBase(
	context.Context,
	string,
	string,
	string,
	*types.KnowledgeBaseConfig,
) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) DeleteKnowledgeBase(context.Context, string) error {
	return nil
}

func (s *stubKnowledgeBaseService) TogglePinKnowledgeBase(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) HybridSearch(context.Context, string, types.SearchParams) ([]*types.SearchResult, error) {
	return s.results, nil
}

func (s *stubKnowledgeBaseService) GetQueryEmbedding(context.Context, string, string) ([]float32, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) ResolveEmbeddingModelKeys(context.Context, []*types.KnowledgeBase) map[string]string {
	return nil
}

func (s *stubKnowledgeBaseService) CopyKnowledgeBase(
	context.Context,
	string,
	string,
) (*types.KnowledgeBase, *types.KnowledgeBase, error) {
	return nil, nil, nil
}

func (s *stubKnowledgeBaseService) DuplicateKnowledgeBase(
	context.Context,
	string,
) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) GetRepository() interfaces.KnowledgeBaseRepository {
	return nil
}

func (s *stubKnowledgeBaseService) ProcessKBDelete(context.Context, *asynq.Task) error {
	return nil
}

func TestQueryKnowledgeGraphReturnsSourcedFactsAndRealRelations(t *testing.T) {
	tool := NewQueryKnowledgeGraphTool(&stubKnowledgeBaseService{kb: &types.KnowledgeBase{ID: "kb"}}).
		WithContextQuery(func(ctx context.Context, kbID string, q types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
			require.Equal(t, "kb", kbID)
			require.Equal(t, "Model A", q.Entity)
			return &types.KnowledgeContextResult{
				Entities: []types.KnowledgeEntity{{ID: "model", CanonicalName: "Model A"}, {ID: "vendor", CanonicalName: "Vendor"}},
				Facts:    []types.KnowledgeFact{{ID: "fact", Kind: types.KnowledgeFactRelation, SubjectEntityID: "model", Predicate: "released_by", ObjectEntityID: "vendor", EvidenceID: "evidence", Quote: "Vendor released Model A."}},
				Evidence: []types.KnowledgeEvidence{{ID: "evidence", KnowledgeID: "message", Body: "Vendor released Model A.", OriginalURL: "https://example.com/announcement"}},
			}, nil
		})
	args, _ := json.Marshal(QueryKnowledgeGraphInput{KnowledgeBaseIDs: []string{"kb"}, Query: "Model A"})
	result, err := tool.Execute(context.Background(), args)
	require.NoError(t, err)
	require.True(t, result.Success)
	assert.Contains(t, result.Output, "released_by")
	assert.Contains(t, result.Output, "https://example.com/announcement")
	graph := result.Data["graph_data"].(map[string]any)
	require.Len(t, graph["edges"], 1)
	edge := graph["edges"].([]map[string]any)[0]
	assert.Equal(t, "model", edge["source"])
	assert.Equal(t, "vendor", edge["target"])
	assert.Equal(t, "evidence", edge["evidence_id"])
	assert.NotContains(t, result.Output, "Chunk 1")
}

func TestQueryKnowledgeGraphFailsClosedOutsideScope(t *testing.T) {
	called := false
	tool := NewQueryKnowledgeGraphTool(nil, types.SearchTargets{&types.SearchTarget{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "allowed"}}).
		WithContextQuery(func(context.Context, string, types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
			called = true
			return nil, nil
		})
	args, _ := json.Marshal(QueryKnowledgeGraphInput{KnowledgeBaseIDs: []string{"forbidden"}, Query: "Model A"})
	result, err := tool.Execute(context.Background(), args)
	require.Error(t, err)
	require.False(t, result.Success)
	require.False(t, called)
}
