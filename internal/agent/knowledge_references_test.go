package agent

import (
	"strings"
	"testing"

	agenttools "reliquary/internal/agent/tools"
	"reliquary/internal/types"
)

func TestAppendKnowledgeRefsFromToolResults(t *testing.T) {
	state := &types.AgentState{}
	step := types.AgentStep{ToolCalls: []types.ToolCall{
		{
			Name: agenttools.ToolKnowledgeSearch,
			Result: &types.ToolResult{Success: true, Data: map[string]interface{}{
				"results": []map[string]interface{}{{
					"chunk_id": "chunk-1", "knowledge_id": "message-1", "knowledge_base_id": "kb-1",
					"knowledge_title": "Source message", "content": "Original evidence",
				}},
			}},
		},
		{
			Name: agenttools.ToolKnowledgeSearch,
			Result: &types.ToolResult{Success: true, Data: map[string]interface{}{
				"results": []map[string]interface{}{{"chunk_id": "chunk-other", "knowledge_id": "message-1"}},
			}},
		},
		{
			Name: agenttools.ToolListKnowledgeChunks,
			Result: &types.ToolResult{Success: true, Data: map[string]interface{}{
				"knowledge_id": "message-2", "knowledge_title": "Second message",
				"chunks": []map[string]interface{}{{
					"chunk_id": "chunk-2", "knowledge_base": "kb-1", "content": strings.Repeat("证", 600),
				}},
			}},
		},
		{
			Name: "web_search",
			Result: &types.ToolResult{Success: true, Data: map[string]interface{}{
				"results": []map[string]interface{}{{"knowledge_id": "not-a-message"}},
			}},
		},
	}}
	refs := appendKnowledgeRefs(state, step)
	if len(refs) != 2 || len(state.KnowledgeRefs) != 2 {
		t.Fatalf("expected two unique knowledge references, got %d new and %d total", len(refs), len(state.KnowledgeRefs))
	}
	if refs[0].KnowledgeID != "message-1" || refs[0].KnowledgeBaseID != "kb-1" || refs[0].Content != "Original evidence" {
		t.Fatalf("unexpected knowledge reference: %+v", refs[0])
	}
	if refs[1].KnowledgeID != "message-2" || refs[1].KnowledgeBaseID != "kb-1" || len([]rune(refs[1].Content)) > 301 {
		t.Fatalf("unexpected additional knowledge reference: %+v", refs[1])
	}
}
