package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	agenttools "reliquary/internal/agent/tools"
	"reliquary/internal/types"
)

type documentQASearchTool struct{ agenttools.BaseTool }

func (t *documentQASearchTool) Execute(context.Context, json.RawMessage) (*types.ToolResult, error) {
	return &types.ToolResult{Success: true, Output: "Acme ships widgets.", Data: map[string]any{"display_type": "search_results", "results": []map[string]any{{"chunk_id": "article-chunk", "knowledge_id": "article", "knowledge_base_id": "kb", "knowledge_title": "Acme", "content": "Acme ships widgets."}}}}, nil
}

func TestAgentQACompletesWithRetrievedDocumentCitation(t *testing.T) {
	model := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{{Done: true, FinishReason: "tool_calls", ToolCalls: []types.LLMToolCall{{ID: "search", Function: types.FunctionCall{Name: agenttools.ToolKnowledgeSearch, Arguments: `{}`}}}}}},
		{chunks: []types.StreamResponse{{Content: `Acme ships widgets. <ref id="c1"/>`, Done: true}}},
	}}
	engine := newTestEngine(t, model)
	engine.toolRegistry = agenttools.NewToolRegistry()
	engine.toolRegistry.RegisterTool(&documentQASearchTool{BaseTool: agenttools.NewBaseTool(agenttools.ToolKnowledgeSearch, "Search documents", json.RawMessage(`{"type":"object"}`))})
	state, err := engine.Execute(context.Background(), "session", "message", "What does Acme ship?", nil)
	require.NoError(t, err)
	require.True(t, state.IsComplete)
	require.Contains(t, state.FinalAnswer, `<kb doc="Acme" chunk_id="article-chunk" kb_id="kb" />`)
	require.Len(t, state.KnowledgeRefs, 1)
	require.Equal(t, "article", state.KnowledgeRefs[0].KnowledgeID)
	require.Len(t, model.calls, 2)
}
