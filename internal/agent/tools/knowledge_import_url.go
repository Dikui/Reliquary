package tools

import (
	"context"
	"encoding/json"
	"strings"

	"reliquary/internal/types"
)

type KnowledgeImportURLInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id"`
	URL             string `json:"url"`
}

type knowledgeImportURLTool struct {
	BaseTool
	allowed   map[string]uint64
	importURL func(context.Context, KnowledgeImportURLInput) error
}

// NewKnowledgeImportURLTool queues an original page for the existing intake review.
// The callback must enforce write permission; the tool also limits KB scope.
func NewKnowledgeImportURLTool(targets types.SearchTargets, importURL func(context.Context, KnowledgeImportURLInput) error) types.Tool {
	return &knowledgeImportURLTool{
		BaseTool: NewBaseTool(ToolKnowledgeImportURL,
			"Fetch a discovered original article URL into the knowledge base's normal completeness, source and quality review. The result is queued, not approved. Use only for a user-requested research task; never submit search snippets or X/Twitter posts. Source metadata must be extracted from the page, not guessed.",
			json.RawMessage(`{"type":"object","properties":{"knowledge_base_id":{"type":"string"},"url":{"type":"string"}},"required":["knowledge_base_id","url"]}`)),
		allowed: targets.GetKBTenantMap(), importURL: importURL,
	}
}

func (t *knowledgeImportURLTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input KnowledgeImportURLInput
	if err := json.Unmarshal(args, &input); err != nil || strings.TrimSpace(input.URL) == "" {
		return &types.ToolResult{Success: false, Error: "valid URL required"}, nil
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 || t.allowed[input.KnowledgeBaseID] != tenantID {
		return &types.ToolResult{Success: false, Error: "knowledge base is outside this workspace scope"}, nil
	}
	err := t.importURL(ctx, input)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return &types.ToolResult{Success: true, Output: "Original-page intake queued. It is not formal evidence until review completes."}, nil
}
