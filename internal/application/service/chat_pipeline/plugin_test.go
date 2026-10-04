package chatpipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reliquary/internal/types"
)

// --- IntoChatMessage tests ---

func TestIntoChatMessage_NoKBRetrieval(t *testing.T) {
	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query: "hello world",
		},
		PipelineState: types.PipelineState{
			Intent: types.IntentChitchat,
		},
	}
	plugin := &PluginIntoChatMessage{messageService: nil}
	nextCalled := false
	err := plugin.OnEvent(context.Background(), types.INTO_CHAT_MESSAGE, cm, func() *PluginError {
		nextCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !nextCalled {
		t.Fatal("next() was not called")
	}
	if cm.UserContent != "hello world" {
		t.Errorf("UserContent: got %q, want %q", cm.UserContent, "hello world")
	}
}

func TestIntoChatMessage_WithMergeResults(t *testing.T) {
	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query: "test query",
			SummaryConfig: types.SummaryConfig{
				ContextTemplate: "Question: {{query}}\n\nReferences:\n{{contexts}}",
			},
		},
		PipelineState: types.PipelineState{
			MergeResult: []*types.SearchResult{
				{Content: "chunk A content"},
				{Content: "chunk B content"},
			},
		},
	}
	plugin := &PluginIntoChatMessage{messageService: nil}
	nextCalled := false
	err := plugin.OnEvent(context.Background(), types.INTO_CHAT_MESSAGE, cm, func() *PluginError {
		nextCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !nextCalled {
		t.Fatal("next() was not called")
	}
	if cm.UserContent == "" {
		t.Fatal("expected UserContent to be populated")
	}
	if !contains(cm.UserContent, "test query") {
		t.Errorf("UserContent should contain query, got: %s", cm.UserContent)
	}
	if !contains(cm.UserContent, "chunk A content") {
		t.Errorf("UserContent should contain chunk A, got: %s", cm.UserContent)
	}
}

func TestIntoChatMessage_ImageDescriptionAppended(t *testing.T) {
	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:                   "what is this?",
			ChatModelSupportsVision: false,
		},
		PipelineState: types.PipelineState{
			Intent:           types.IntentChitchat,
			ImageDescription: "a cat sitting on a mat",
		},
	}
	plugin := &PluginIntoChatMessage{messageService: nil}
	_ = plugin.OnEvent(context.Background(), types.INTO_CHAT_MESSAGE, cm, func() *PluginError {
		return nil
	})
	if !contains(cm.UserContent, "a cat sitting on a mat") {
		t.Errorf("UserContent should contain image description, got: %s", cm.UserContent)
	}
}

// --- PipelineBuilder tests ---

func TestPipelineBuilder_Basic(t *testing.T) {
	pipeline := types.NewPipelineBuilder().
		Add(types.LOAD_HISTORY).
		Add(types.CHAT_COMPLETION_STREAM).
		Build()

	if len(pipeline) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(pipeline))
	}
	if pipeline[0] != types.LOAD_HISTORY {
		t.Errorf("stage 0: got %v, want %v", pipeline[0], types.LOAD_HISTORY)
	}
}

func TestPipelineBuilder_AddIf(t *testing.T) {
	pipeline := types.NewPipelineBuilder().
		Add(types.LOAD_HISTORY).
		AddIf(false, types.QUERY_UNDERSTAND).
		AddIf(true, types.CHAT_COMPLETION_STREAM).
		Build()

	if len(pipeline) != 2 {
		t.Fatalf("expected 2 stages (QUERY_UNDERSTAND skipped), got %d", len(pipeline))
	}
	if pipeline[1] != types.CHAT_COMPLETION_STREAM {
		t.Errorf("stage 1: got %v, want %v", pipeline[1], types.CHAT_COMPLETION_STREAM)
	}
}

func TestPipelineBuilder_Empty(t *testing.T) {
	pipeline := types.NewPipelineBuilder().Build()
	if len(pipeline) != 0 {
		t.Fatalf("expected 0 stages, got %d", len(pipeline))
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s, substr))
}

func containsString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

type contextPipelineReader func(context.Context, string, types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error)

func (r contextPipelineReader) Query(ctx context.Context, id string, q types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
	return r(ctx, id, q)
}

func TestStructuredContextComplementsScopedRAGAndFallsBack(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "Acme revenue", SummaryConfig: types.SummaryConfig{ContextTemplate: "{{query}}\n{{contexts}}"}, SearchTargets: types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", TenantID: 9, KnowledgeIDs: []string{"allowed"}}}}, PipelineState: types.PipelineState{KnowledgeQueries: []types.KnowledgeContextQuery{{Entity: "Acme"}}, SearchResult: []*types.SearchResult{{KnowledgeBaseID: "kb", KnowledgeID: "allowed", Content: "original revenue evidence"}}}}
	calls := 0
	p := &PluginSearchParallel{contextReader: contextPipelineReader(func(scoped context.Context, id string, q types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
		calls++
		if types.MustTenantIDFromContext(scoped) != 9 || id != "kb" || len(q.KnowledgeIDs) != 1 || q.KnowledgeIDs[0] != "allowed" {
			t.Fatalf("scope was widened: %#v", q)
		}
		return &types.KnowledgeContextResult{Facts: []types.KnowledgeFact{{ID: "fact", Predicate: "revenue", Quote: "quoted revenue", EvidenceID: "proof"}}, Evidence: []types.KnowledgeEvidence{{ID: "proof", KnowledgeID: "allowed", Body: "large original snapshot"}}}, nil
	})}
	p.searchStructuredContext(ctx, cm)
	if calls != 2 || !strings.Contains(cm.KnowledgeContext, "quoted revenue") || strings.Contains(cm.KnowledgeContext, "large original snapshot") {
		t.Fatalf("invalid structured context: %s", cm.KnowledgeContext)
	}
	cm.MergeResult = cm.SearchResult
	if err := (&PluginIntoChatMessage{}).OnEvent(ctx, types.INTO_CHAT_MESSAGE, cm, func() *PluginError { return nil }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cm.UserContent, "original revenue evidence") || !strings.Contains(cm.UserContent, "quoted revenue") {
		t.Fatalf("hybrid context missing: %s", cm.UserContent)
	}
	p.contextReader = contextPipelineReader(func(context.Context, string, types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
		return nil, errors.New("unavailable")
	})
	p.searchStructuredContext(ctx, cm)
	if cm.KnowledgeContext != "" || len(cm.SearchResult) != 1 {
		t.Fatal("context failure changed original RAG")
	}
}
