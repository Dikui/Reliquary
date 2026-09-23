package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"reliquary/internal/types"
)

// KnowledgeAnomalyReport is a suggestion only. Applying it requires the KB owner's decision.
type KnowledgeAnomalyReport struct {
	KnowledgeBaseID      string   `json:"knowledge_base_id"`
	KnowledgeIDs         []string `json:"knowledge_ids"`
	Kind                 string   `json:"kind"`
	Description          string   `json:"description"`
	ProposalType         string   `json:"proposal_type"`
	ProposedTagIDs       []string `json:"proposed_tag_ids"`
	ProposedRemoveTagIDs []string `json:"proposed_remove_tag_ids"`
	ProposedDeleteTagID  string   `json:"proposed_delete_tag_id"`
}

type knowledgeReportAnomalyTool struct {
	BaseTool
	allowed map[string]uint64
	targets types.SearchTargets
	report  func(context.Context, KnowledgeAnomalyReport) (string, error)
}

func NewKnowledgeReportAnomalyTool(targets types.SearchTargets, report func(context.Context, KnowledgeAnomalyReport) (string, error)) types.Tool {
	return &knowledgeReportAnomalyTool{
		BaseTool: NewBaseTool(ToolKnowledgeReportAnomaly,
			"Report a message issue or propose explicit tag additions/removals for the knowledge base owner to review. This never applies changes.",
			json.RawMessage(`{"type":"object","properties":{"knowledge_base_id":{"type":"string"},"knowledge_ids":{"type":"array","items":{"type":"string"}},"kind":{"type":"string","enum":["suspected_error","outdated","source_conflict","manual_conflict","tag_deletion"]},"description":{"type":"string"},"proposal_type":{"type":"string","enum":["none","change_tags","delete_tag"]},"proposed_tag_ids":{"type":"array","items":{"type":"string"}},"proposed_remove_tag_ids":{"type":"array","items":{"type":"string"}},"proposed_delete_tag_id":{"type":"string"}},"required":["knowledge_base_id","knowledge_ids","kind","description"]}`)),
		allowed: targets.GetKBTenantMap(), targets: targets, report: report,
	}
}

func (t *knowledgeReportAnomalyTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input KnowledgeAnomalyReport
	if err := json.Unmarshal(args, &input); err != nil {
		return &types.ToolResult{Success: false, Error: "invalid parameters"}, nil
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 || t.allowed[input.KnowledgeBaseID] != tenantID {
		return &types.ToolResult{Success: false, Error: "knowledge base is outside this workspace scope"}, nil
	}
	for _, id := range input.KnowledgeIDs {
		inScope := false
		for _, target := range t.targets {
			if target == nil || target.KnowledgeBaseID != input.KnowledgeBaseID || target.TenantID != tenantID {
				continue
			}
			if target.Type == types.SearchTargetTypeKnowledgeBase && len(target.KnowledgeIDs) == 0 {
				inScope = true
				break
			}
			for _, scopedID := range target.KnowledgeIDs {
				if id == scopedID {
					inScope = true
					break
				}
			}
		}
		if !inScope {
			return &types.ToolResult{Success: false, Error: "message is outside this knowledge scope"}, nil
		}
	}
	id, err := t.report(ctx, input)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	return &types.ToolResult{Success: true, Output: fmt.Sprintf("Reported message anomaly %s for owner review; no correction was applied.", id)}, nil
}
