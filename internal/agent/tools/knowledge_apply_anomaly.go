package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"reliquary/internal/types"
)

type knowledgeApplyAnomalyTool struct {
	BaseTool
	allowed map[string]uint64
	apply   func(context.Context, string, string) (string, error)
}

func NewKnowledgeApplyAnomalyTool(targets types.SearchTargets, apply func(context.Context, string, string) (string, error)) types.Tool {
	return &knowledgeApplyAnomalyTool{
		BaseTool: NewBaseTool(ToolKnowledgeApplyAnomaly,
			"Apply a specific pending message correction only when the knowledge base owner explicitly asks you to do so. This changes knowledge and requires owner rights.",
			json.RawMessage(`{"type":"object","properties":{"knowledge_base_id":{"type":"string"},"anomaly_id":{"type":"string"}},"required":["knowledge_base_id","anomaly_id"]}`)),
		allowed: targets.GetKBTenantMap(), apply: apply,
	}
}

func (t *knowledgeApplyAnomalyTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input struct {
		KnowledgeBaseID string `json:"knowledge_base_id"`
		AnomalyID       string `json:"anomaly_id"`
	}
	if err := json.Unmarshal(args, &input); err != nil || input.AnomalyID == "" {
		return &types.ToolResult{Success: false, Error: "invalid parameters"}, nil
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 || t.allowed[input.KnowledgeBaseID] != tenantID {
		return &types.ToolResult{Success: false, Error: "knowledge base is outside this workspace scope"}, nil
	}
	result, err := t.apply(ctx, input.KnowledgeBaseID, input.AnomalyID)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return &types.ToolResult{Success: true, Output: fmt.Sprintf("Applied anomaly %s: %s", input.AnomalyID, result)}, nil
}
