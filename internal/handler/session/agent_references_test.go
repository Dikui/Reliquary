package session

import (
	"context"
	"testing"

	"reliquary/internal/event"
	"reliquary/internal/types"
)

func TestAgentReferenceSnapshotsReplacePreviousRound(t *testing.T) {
	ctx := context.Background()
	h := &AgentStreamHandler{
		ctx:              ctx,
		assistantMessage: &types.Message{},
		streamManager:    &stubStreamManager{},
	}
	first := &types.SearchResult{KnowledgeID: "message-1"}
	second := &types.SearchResult{KnowledgeID: "message-2"}
	for _, refs := range [][]*types.SearchResult{{first}, {first, second}} {
		if err := h.handleReferences(ctx, event.Event{Data: event.AgentReferencesData{References: refs}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.assistantMessage.KnowledgeReferences) != 2 || h.assistantMessage.KnowledgeReferences[0].KnowledgeID != "message-1" || h.assistantMessage.KnowledgeReferences[1].KnowledgeID != "message-2" {
		t.Fatalf("expected one copy of each message after two rounds, got %+v", h.assistantMessage.KnowledgeReferences)
	}
}
