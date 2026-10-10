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
	...string,
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

func TestQueryKnowledgeContextReturnsSourcedFactsAndRealRelations(t *testing.T) {
	tool := NewQueryKnowledgeContextTool(&stubKnowledgeBaseService{kb: &types.KnowledgeBase{ID: "kb"}}).
		WithContextQuery(func(ctx context.Context, kbID string, q types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
			require.Equal(t, "kb", kbID)
			require.Equal(t, "Model A", q.Entity)
			return &types.KnowledgeContextResult{
				Entities: []types.KnowledgeEntity{{ID: "model", CanonicalName: "Model A"}, {ID: "vendor", CanonicalName: "Vendor"}},
				Facts:    []types.KnowledgeFact{{ID: "fact", Kind: types.KnowledgeFactRelation, SubjectEntityID: "model", Predicate: "released_by", ObjectEntityID: "vendor", EvidenceID: "evidence", Quote: "Vendor released Model A."}},
				Evidence: []types.KnowledgeEvidence{{ID: "evidence", KnowledgeID: "message", Body: "Vendor released Model A.", OriginalURL: "https://example.com/announcement"}},
			}, nil
		})
	args, _ := json.Marshal(QueryKnowledgeContextInput{KnowledgeBaseIDs: []string{"kb"}, Query: "Model A"})
	result, err := tool.Execute(context.Background(), args)
	require.NoError(t, err)
	require.True(t, result.Success)
	assert.Contains(t, result.Output, "released_by")
	assert.Contains(t, result.Output, "https://example.com/announcement")
	contextData := result.Data["context_data"].(map[string]any)
	require.Len(t, contextData["edges"], 1)
	edge := contextData["edges"].([]map[string]any)[0]
	assert.Equal(t, "model", edge["source"])
	assert.Equal(t, "vendor", edge["target"])
	assert.Equal(t, "evidence", edge["evidence_id"])
	assert.NotContains(t, result.Output, "Chunk 1")
}

func TestQueryKnowledgeContextFailsClosedOutsideScope(t *testing.T) {
	called := false
	tool := NewQueryKnowledgeContextTool(nil, types.SearchTargets{&types.SearchTarget{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "allowed"}}).
		WithContextQuery(func(context.Context, string, types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
			called = true
			return nil, nil
		})
	args, _ := json.Marshal(QueryKnowledgeContextInput{KnowledgeBaseIDs: []string{"forbidden"}, Query: "Model A"})
	result, err := tool.Execute(context.Background(), args)
	require.Error(t, err)
	require.False(t, result.Success)
	require.False(t, called)
}

func TestQueryKnowledgeContextAllAndDomainScopes(t *testing.T) {
	for _, scope := range []string{"all", "domain"} {
		t.Run(scope, func(t *testing.T) {
			calls := 0
			tool := NewQueryKnowledgeContextTool(nil).WithContextQuery(func(ctx context.Context, kbID string, q types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
				calls++
				require.Empty(t, kbID)
				require.Equal(t, types.NormalizeKnowledgeContextScope(scope), q.Scope)
				require.Equal(t, "energy", q.DomainID)
				return &types.KnowledgeContextResult{Entities: []types.KnowledgeEntity{{ID: "company", CanonicalName: "Acme"}}, Facts: []types.KnowledgeFact{{KnowledgeBaseID: "one", KnowledgeBaseName: "First", Quote: "Acme"}, {KnowledgeBaseID: "two", KnowledgeBaseName: "Second", Quote: "Acme"}}}, nil
			})
			args, _ := json.Marshal(QueryKnowledgeContextInput{Scope: scope, DomainID: "energy", Query: "Acme"})
			result, err := tool.Execute(context.Background(), args)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, 1, calls)
			require.Contains(t, result.Output, "First")
			require.Contains(t, result.Output, "Second")
		})
	}
}

func TestQueryKnowledgeContextRejectsLegacyTeamScope(t *testing.T) {
	called := false
	tool := NewQueryKnowledgeContextTool(nil).WithContextQuery(func(context.Context, string, types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
		called = true
		return &types.KnowledgeContextResult{}, nil
	})
	args, err := json.Marshal(QueryKnowledgeContextInput{Scope: "team", Query: "Acme"})
	require.NoError(t, err)
	result, err := tool.Execute(context.Background(), args)
	require.ErrorContains(t, err, "scope must be knowledge_base, domain or all")
	require.False(t, result.Success)
	require.Contains(t, result.Error, "scope must be knowledge_base, domain or all")
	require.False(t, called)
}

func TestQueryKnowledgeContextAllScopePreservesTargetsAndDeduplicatesObjects(t *testing.T) {
	targets := types.SearchTargets{{KnowledgeBaseID: "one", Type: types.SearchTargetTypeKnowledgeBase}, {KnowledgeBaseID: "two", Type: types.SearchTargetTypeKnowledge, KnowledgeIDs: []string{"selected"}}}
	queried := map[string][]string{}
	tool := NewQueryKnowledgeContextTool(nil, targets).WithContextQuery(func(ctx context.Context, kbID string, q types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
		queried[kbID] = q.KnowledgeIDs
		require.Empty(t, q.Scope, "each authorized target keeps its narrower query scope")
		return &types.KnowledgeContextResult{Entities: []types.KnowledgeEntity{{ID: "company", CanonicalName: "Acme"}}}, nil
	})
	args, _ := json.Marshal(QueryKnowledgeContextInput{Scope: "all", Query: "Acme"})
	result, err := tool.Execute(context.Background(), args)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Len(t, queried, 2)
	require.Equal(t, []string{"selected"}, queried["two"])
	require.Equal(t, 1, result.Data["context_data"].(map[string]any)["total_nodes"])
}

func TestQueryKnowledgeContextRegistryInvokesCurrentAndLegacyNames(t *testing.T) {
	calls := 0
	tool := NewQueryKnowledgeContextTool(nil).WithContextQuery(func(context.Context, string, types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
		calls++
		return &types.KnowledgeContextResult{Entities: []types.KnowledgeEntity{{ID: "acme", CanonicalName: "Acme"}}}, nil
	})
	registry := NewToolRegistry()
	registry.RegisterTool(tool)
	args := json.RawMessage(`{"knowledge_base_ids":["kb"],"query":"Acme"}`)
	for _, name := range []string{ToolQueryKnowledgeContext, "query_knowledge_graph"} {
		result, err := registry.ExecuteTool(context.Background(), name, args)
		require.NoError(t, err)
		require.True(t, result.Success)
		require.Equal(t, "context_query_results", result.Data["display_type"])
		require.Contains(t, result.Data, "context_data")
	}
	require.Equal(t, 2, calls)
	require.Equal(t, []string{ToolQueryKnowledgeContext}, registry.ListTools())
}
