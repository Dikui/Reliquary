package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"reliquary/internal/agent/approval"
	"reliquary/internal/types"
)

type knowledgeApplyAnomalyTool struct {
	BaseTool
	allowed map[string]uint64
	apply   func(context.Context, string, string) (string, error)
	gate    approval.RequiredApproval
}

func NewKnowledgeApplyAnomalyTool(targets types.SearchTargets, gate approval.RequiredApproval, apply func(context.Context, string, string) (string, error)) types.Tool {
	return &knowledgeApplyAnomalyTool{
		BaseTool: NewBaseTool(ToolKnowledgeApplyAnomaly,
			"Apply a specific pending message correction only when the knowledge base owner explicitly asks you to do so. This changes knowledge and requires owner rights.",
			json.RawMessage(`{"type":"object","properties":{"knowledge_base_id":{"type":"string"},"anomaly_id":{"type":"string"}},"required":["knowledge_base_id","anomaly_id"]}`)),
		allowed: targets.GetKBTenantMap(), gate: gate, apply: apply,
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
	meta, hasMeta := ToolExecFromContext(ctx)
	actorID, hasActor := types.UserIDFromContext(ctx)
	if !hasMeta || meta.EventBus == nil || meta.UserID == "" || !hasActor || meta.UserID != actorID || t.gate == nil {
		return &types.ToolResult{Success: false, Error: "owner approval is unavailable"}, nil
	}
	waitCtx := ctx
	if meta.ApprovalCtx != nil {
		waitCtx = meta.ApprovalCtx
	}
	decision, err := t.gate.RequireAndWait(waitCtx, approval.PendingRequest{
		TenantID: tenantID, UserID: actorID, SessionID: meta.SessionID,
		AssistantMessageID: meta.AssistantMessageID, RequestID: meta.RequestID,
		EventBus: meta.EventBus, ServiceID: "reliquary_knowledge", ServiceName: "Knowledge Base",
		MCPToolName: ToolKnowledgeApplyAnomaly, RegisteredToolName: ToolKnowledgeApplyAnomaly,
		Description: t.Description(), Args: args, ToolCallID: meta.ToolCallID,
	})
	if err != nil || !decision.Approved {
		return &types.ToolResult{Success: false, Error: "owner did not approve this correction"}, nil
	}
	if len(decision.ModifiedArgs) > 0 {
		if err := json.Unmarshal(decision.ModifiedArgs, &input); err != nil || input.AnomalyID == "" || t.allowed[input.KnowledgeBaseID] != tenantID {
			return &types.ToolResult{Success: false, Error: "invalid approved correction"}, nil
		}
	}
	if meta.ApprovalCtx != nil {
		timeout := meta.ExecTimeout
		if timeout <= 0 {
			timeout = time.Minute
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(meta.ApprovalCtx, timeout)
		defer cancel()
	}
	result, err := t.apply(ctx, input.KnowledgeBaseID, input.AnomalyID)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return &types.ToolResult{Success: true, Output: fmt.Sprintf("Applied anomaly %s: %s", input.AnomalyID, result)}, nil
}
