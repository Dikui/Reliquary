package modelcontext

import (
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

func TestRegistryProtocolOwnsResourceHandleRules(t *testing.T) {
	prompt := NewRegistry(true).ProtocolPrompt()
	require.Contains(t, prompt, "Source handling protocol")
	require.Contains(t, prompt, "Resource handle protocol")
	require.Contains(t, prompt, "res://NNNN")
}

func TestRegistryAuditsUnresolvedAndPartiallyResolvedToolHandles(t *testing.T) {
	registry := NewRegistry(true)
	registry.RegisterKnowledgeBase("kb-real")

	calls := []types.LLMToolCall{{Function: types.FunctionCall{
		Name:      "knowledge_search",
		Arguments: `{"knowledge_base_ids":["b1","b99"],"query":"b98","asset":"res://0099"}`,
	}}}
	registry.DecodeToolCalls(calls)

	require.JSONEq(t,
		`{"knowledge_base_ids":["kb-real","b99"],"query":"b98","asset":"res://0099"}`,
		calls[0].Function.Arguments,
	)
	require.Equal(t, ArgumentResolutionPartiallyResolved, calls[0].ArgumentResolution)
	require.Equal(t, []string{"b99", "res://0099"}, calls[0].UnresolvedHandles)
	// Alias-shaped free text is not an ID and must not be reported as a source
	// resolution failure.
	require.NotContains(t, calls[0].UnresolvedHandles, "b98")
}

func TestRegistryAuditsUnchangedJSONFormattingAsUnchanged(t *testing.T) {
	registry := NewRegistry(true)
	calls := []types.LLMToolCall{{Function: types.FunctionCall{Arguments: `{ "query": "hello" }`}}}
	registry.DecodeToolCalls(calls)
	require.Equal(t, ArgumentResolutionUnchanged, calls[0].ArgumentResolution)
}

func TestRegistryDecodesKnownHandlesEmbeddedInBuiltInSQL(t *testing.T) {
	registry := NewRegistry(true)
	registry.RegisterDocument("doc-real")
	registry.RegisterKnowledgeBase("kb-real")
	calls := []types.LLMToolCall{{
		Function: types.FunctionCall{
			Name:      "data_analysis",
			Arguments: `{"sql":"SELECT * FROM knowledges WHERE id = 'd1' AND knowledge_base_id = 'b1'"}`,
		},
	}}
	registry.DecodeToolCalls(calls)
	require.JSONEq(t,
		`{"sql":"SELECT * FROM knowledges WHERE id = 'doc-real' AND knowledge_base_id = 'kb-real'"}`,
		calls[0].Function.Arguments,
	)
	require.Equal(t, ArgumentResolutionResolved, calls[0].ArgumentResolution)
}

func TestRegistryOnlyDecodesQuotedSQLHandlesAndRejectsUnknownOnes(t *testing.T) {
	registry := NewRegistry(true)
	registry.RegisterDocument("doc-real")

	calls := []types.LLMToolCall{{
		Function: types.FunctionCall{
			Name:      "data_analysis",
			Arguments: `{"sql":"SELECT d1.id FROM documents d1 WHERE d1.id = 'd1' AND d1.parent_id = 'd99'"}`,
		},
	}}
	registry.DecodeToolCalls(calls)
	require.JSONEq(t,
		`{"sql":"SELECT d1.id FROM documents d1 WHERE d1.id = 'doc-real' AND d1.parent_id = 'd99'"}`,
		calls[0].Function.Arguments,
	)
	require.Equal(t, []string{"d99"}, calls[0].UnresolvedHandles)
	require.Equal(t, ArgumentResolutionPartiallyResolved, calls[0].ArgumentResolution)
}

func TestRegistryReplaysStructuredHandlesWithoutAliasDrift(t *testing.T) {
	registry := NewRegistry(true)
	registry.RegisterDocument("doc-real")
	registry.RegisterKnowledgeBase("kb-real")
	messages := []chat.Message{
		{Role: "assistant", ToolCalls: []chat.ToolCall{{Function: chat.FunctionCall{Name: "data_analysis", Arguments: `{"sql":"SELECT * FROM knowledges WHERE id = 'doc-real' AND knowledge_base_id = 'kb-real'"}`}}}},
		{Role: "tool", Name: "get_document_info", Content: `{"knowledge_base_id":"kb-real","knowledge_id":"doc-real"}`},
	}
	first := registry.EncodeMessages(messages)
	require.JSONEq(t, `{"sql":"SELECT * FROM knowledges WHERE id = 'd1' AND knowledge_base_id = 'b1'"}`, first[0].ToolCalls[0].Function.Arguments)
	require.JSONEq(t, `{"knowledge_base_id":"b1","knowledge_id":"d1"}`, first[1].Content)
	second := registry.EncodeMessages(first)
	require.JSONEq(t, first[0].ToolCalls[0].Function.Arguments, second[0].ToolCalls[0].Function.Arguments)
	require.JSONEq(t, first[1].Content, second[1].Content)
}

func TestRegistryDoesNotApplyBuiltInFieldPoliciesToDynamicTools(t *testing.T) {
	registry := NewRegistry(true)
	registry.RegisterDocument("doc-real")
	registry.RegisterWeb("https://example.com", "Example")
	calls := []types.LLMToolCall{{Function: types.FunctionCall{
		Name:      "dynamic_mcp_tool",
		Arguments: `{"issue_id":"i1","knowledge_id":"d1","url":"w1","sql":"SELECT 'd1'"}`,
	}}}
	registry.DecodeToolCalls(calls)
	require.JSONEq(t, `{"issue_id":"i1","knowledge_id":"d1","url":"w1","sql":"SELECT 'd1'"}`, calls[0].Function.Arguments)
	require.Equal(t, ArgumentResolutionUnchanged, calls[0].ArgumentResolution)
	require.Empty(t, calls[0].UnresolvedHandles)

	messages := registry.EncodeMessages([]chat.Message{
		{
			Role: "assistant",
			ToolCalls: []chat.ToolCall{{Function: chat.FunctionCall{
				Name:      "dynamic_mcp_tool",
				Arguments: `{"knowledge_id":"doc-real"}`,
			}}},
		},
		{
			Role:    "tool",
			Name:    "dynamic_mcp_tool",
			Content: `<knowledge_id>mcp-owned-id</knowledge_id> doc-real`,
		},
	})
	require.JSONEq(t, `{"knowledge_id":"doc-real"}`, messages[0].ToolCalls[0].Function.Arguments)
	require.Equal(t, `<knowledge_id>mcp-owned-id</knowledge_id> doc-real`, messages[1].Content)

	modelOutput := registry.ModelToolResultForTool("dynamic_mcp_tool", &types.ToolResult{
		Success: true,
		Output:  `<knowledge_id>mcp-owned-id</knowledge_id> doc-real`,
	})
	require.Equal(t, `<knowledge_id>mcp-owned-id</knowledge_id> doc-real`, modelOutput)
}

func TestRegistryCompactsKnownIDsInBuiltInValidationErrors(t *testing.T) {
	registry := NewRegistry(true)
	registry.RegisterDocument("doc-real")
	registry.RegisterKnowledgeBase("kb-real")

	got := registry.ModelToolResultForTool("knowledge_report_anomaly", &types.ToolResult{
		Success: false,
		Error:   "document doc-real belongs to knowledge base kb-real",
	})
	require.Equal(t, "Error: document d1 belongs to knowledge base b1", got)
}

func TestRegistryDecodesCanonicalArgumentsForEveryBuiltInReferenceTool(t *testing.T) {
	registry := NewRegistry(true)
	registry.RegisterDocument("doc-real")
	registry.RegisterKnowledgeBase("kb-real")
	registry.RegisterChunk(ChunkReference{ChunkID: "chunk-real", KnowledgeID: "doc-real", KnowledgeBaseID: "kb-real"})
	registry.RegisterWeb("https://example.com/page", "Example")

	tests := []struct {
		name string
		tool string
		raw  string
		want string
	}{
		{"knowledge search KB", "knowledge_search", `{"queries":["d1"],"knowledge_base_ids":["b1"]}`, `{"queries":["d1"],"knowledge_base_ids":["kb-real"]}`},
		{"list document", "list_knowledge_chunks", `{"knowledge_id":"d1"}`, `{"knowledge_id":"doc-real"}`},
		{"list chunk", "list_knowledge_chunks", `{"chunk_id":"c1"}`, `{"chunk_id":"chunk-real"}`},
		{"document info", "get_document_info", `{"knowledge_ids":["d1"]}`, `{"knowledge_ids":["doc-real"]}`},
		{"data analysis SQL", "data_analysis", `{"knowledge_id":"d1","sql":"SELECT * FROM 'd1'"}`, `{"knowledge_id":"doc-real","sql":"SELECT * FROM 'doc-real'"}`},
		{"data schema", "data_schema", `{"knowledge_id":"d1"}`, `{"knowledge_id":"doc-real"}`},
		{"web fetch", "web_fetch", `{"items":[{"url":"w1","prompt":"read"}]}`, `{"items":[{"url":"https://example.com/page","prompt":"read"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := []types.LLMToolCall{{Function: types.FunctionCall{Name: test.tool, Arguments: test.raw}}}
			registry.DecodeToolCalls(calls)
			require.JSONEq(t, test.want, calls[0].Function.Arguments)
			require.NotEqual(t, ArgumentResolutionUnresolved, calls[0].ArgumentResolution)
		})
	}
}

func TestRegistryStreamDecoderRestoresSplitResourceAndCitationHandles(t *testing.T) {
	registry := NewRegistry(true)
	registry.EncodeMessages([]chat.Message{{Role: "user", Content: "resource://AbCdEfGhIjKlMnOpQrStUv"}})
	registry.RegisterChunk(ChunkReference{
		ChunkID:         "chunk-real",
		KnowledgeID:     "doc-real",
		KnowledgeBaseID: "kb-real",
		DocumentTitle:   "Doc",
	})

	decoder := registry.StreamDecoder()
	got := decoder.Feed("image res://0") +
		decoder.Feed("001 claim <ref id=\"c") +
		decoder.Feed("1\"/>") + decoder.Flush()
	require.Contains(t, got, "resource://AbCdEfGhIjKlMnOpQrStUv")
	require.Contains(t, got, `chunk_id="chunk-real"`)
}

func TestRegistryDropsUnknownResourceHandlesFromCompleteAndStreamOutput(t *testing.T) {
	registry := NewRegistry(true)
	require.Equal(t, "broken ", registry.DecodeOutputText("broken res://9999"))

	decoder := registry.StreamDecoder()
	got := decoder.Feed("broken res:/") + decoder.Feed("/99") + decoder.Feed("99 end") + decoder.Flush()
	require.Equal(t, "broken  end", got)
}
