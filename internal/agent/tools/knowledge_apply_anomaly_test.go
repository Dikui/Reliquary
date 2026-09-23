package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/agent/approval"
	"reliquary/internal/event"
	"reliquary/internal/types"
)

type anomalyApprovalStub struct{ approved bool }

func (s anomalyApprovalStub) RequireAndWait(_ context.Context, req approval.PendingRequest) (approval.Decision, error) {
	if req.UserID != "owner" || req.RegisteredToolName != ToolKnowledgeApplyAnomaly {
		return approval.Decision{}, nil
	}
	return approval.Decision{Approved: s.approved}, nil
}

func TestKnowledgeApplyAnomalyRequiresPerCallOwnerApproval(t *testing.T) {
	called := false
	apply := func(context.Context, string, string) (string, error) { called = true; return "tags_updated", nil }
	targets := types.SearchTargets{&types.SearchTarget{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb", TenantID: 1}}
	args := json.RawMessage(`{"knowledge_base_id":"kb","anomaly_id":"issue"}`)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "owner")
	ctx = WithToolExecContext(ctx, &ToolExecContext{UserID: "owner", SessionID: "session", EventBus: &event.EventBus{}})
	result, err := NewKnowledgeApplyAnomalyTool(targets, nil, apply).Execute(ctx, args)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.False(t, called)
	result, err = NewKnowledgeApplyAnomalyTool(targets, anomalyApprovalStub{approved: false}, apply).Execute(ctx, args)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.False(t, called)
	result, err = NewKnowledgeApplyAnomalyTool(targets, anomalyApprovalStub{approved: true}, apply).Execute(ctx, args)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, called)
}
