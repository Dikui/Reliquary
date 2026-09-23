package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestKnowledgeReportAnomalyToolRestrictsWorkspaceScope(t *testing.T) {
	called := false
	tool := NewKnowledgeReportAnomalyTool(types.SearchTargets{&types.SearchTarget{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", TenantID: 1, KnowledgeIDs: []string{"message"}}},
		func(_ context.Context, _ KnowledgeAnomalyReport) (string, error) { called = true; return "issue", nil })
	args := json.RawMessage(`{"knowledge_base_id":"kb","knowledge_ids":["message"],"kind":"suspected_error","description":"incorrect"}`)
	other := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(2))
	result, err := tool.Execute(other, args)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.False(t, called)
	owner := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	outOfScope := json.RawMessage(`{"knowledge_base_id":"kb","knowledge_ids":["other"],"kind":"suspected_error","description":"incorrect"}`)
	result, err = tool.Execute(owner, outOfScope)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.False(t, called)
	result, err = tool.Execute(owner, args)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, called)
}
