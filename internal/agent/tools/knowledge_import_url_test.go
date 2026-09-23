package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestKnowledgeImportURLToolUsesOnlyScopedOriginalURL(t *testing.T) {
	var imported KnowledgeImportURLInput
	tool := NewKnowledgeImportURLTool(types.SearchTargets{&types.SearchTarget{
		Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb", TenantID: 1,
	}}, func(_ context.Context, input KnowledgeImportURLInput) error {
		imported = input
		return nil
	})
	args := json.RawMessage(`{"knowledge_base_id":"kb","url":"https://example.com/original","source_info":{"author":"invented"}}`)
	wrongTenant := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(2))
	result, err := tool.Execute(wrongTenant, args)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Empty(t, imported.URL)

	owner := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	result, err = tool.Execute(owner, args)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "https://example.com/original", imported.URL)
	require.NotContains(t, result.Output, "approved")
}
