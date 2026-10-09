package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
)

func TestLegacyContextToolHintsAndHistoryRemainRecognized(t *testing.T) {
	require.Equal(t, "查询知识上下文", formatToolHint("query_knowledge_graph", nil))
	messages := []chat.Message{{Role: "tool", Name: "query_knowledge_graph", ToolCallID: "old-call", Content: "stale source evidence"}}
	redacted := redactHistoryKBResults(messages)
	require.Len(t, redacted, 1)
	require.NotContains(t, redacted[0].Content, "stale source evidence")
	require.Equal(t, "old-call", redacted[0].ToolCallID)
	require.Equal(t, messages[0].Name, redacted[0].Name)
}
