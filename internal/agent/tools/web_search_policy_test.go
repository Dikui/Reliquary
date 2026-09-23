package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type policySearch struct {
	interfaces.WebSearchService
	query string
}

func (s *policySearch) Search(_ context.Context, _ string, _ *types.WebSearchConfig, query string) ([]*types.WebSearchResult, error) {
	s.query = query
	return nil, nil
}

type policyChat struct{}

func (policyChat) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	var request map[string]string
	if err := json.Unmarshal([]byte(messages[1].Content), &request); err != nil {
		return nil, err
	}
	if strings.Contains(request["policy"], "primary") {
		return &types.ChatResponse{Content: `{"query":"official ` + request["query"] + `"}`}, nil
	}
	return &types.ChatResponse{Content: `{"query":"` + request["query"] + `"}`}, nil
}
func (policyChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("not used")
}
func (policyChat) GetModelName() string { return "policy" }
func (policyChat) GetModelID() string   { return "policy" }

func TestWebSearchAppliesActiveResearchPolicyToActualQuery(t *testing.T) {
	search := &policySearch{}
	tool := NewWebSearchTool(search, nil, nil, nil, "session", 5, "").WithResearchPolicy(policyChat{}, "prefer primary sources")
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	result, err := tool.Execute(ctx, json.RawMessage(`{"query":"AI release"}`))
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "official AI release", search.query)
	tool = NewWebSearchTool(search, nil, nil, nil, "session", 5, "").WithResearchPolicy(policyChat{}, "")
	_, err = tool.Execute(ctx, json.RawMessage(`{"query":"AI release"}`))
	require.NoError(t, err)
	require.Equal(t, "AI release", search.query)
}
