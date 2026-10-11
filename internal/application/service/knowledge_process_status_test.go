package service

import (
	"testing"
	"time"

	"reliquary/internal/types"
)

func TestFinalizeIndexedKnowledgeState(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	knowledge := &types.Knowledge{ParseStatus: types.ParseStatusProcessing, ErrorMessage: "previous failure", Description: "summary"}
	finalizeIndexedKnowledgeState(knowledge, 4096, now)
	if knowledge.ParseStatus != types.ParseStatusCompleted || knowledge.SummaryStatus != types.SummaryStatusCompleted || knowledge.ErrorMessage != "" || knowledge.Description != "summary" {
		t.Fatalf("unexpected completion: %+v", knowledge)
	}
	if knowledge.ProcessedAt == nil || !knowledge.ProcessedAt.Equal(now) || knowledge.StorageSize != 4096 || knowledge.EnableStatus != "enabled" {
		t.Fatalf("invalid indexed state: %+v", knowledge)
	}
}

// TestMarkKnowledgeProcessingClearsPreviousAttemptError covers the transition
// every worker performs before it starts a new attempt. A row that failed
// earlier still carries that attempt's error_message, and leaving it in place
// makes the UI report a failure on a document it is simultaneously showing as
// processing.
func TestMarkKnowledgeProcessingClearsPreviousAttemptError(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	knowledge := &types.Knowledge{
		ParseStatus:  types.ParseStatusFailed,
		ErrorMessage: "Task interrupted due to application restart",
	}

	markKnowledgeProcessing(knowledge, now)

	if knowledge.ParseStatus != types.ParseStatusProcessing {
		t.Fatalf("ParseStatus = %q, want %q", knowledge.ParseStatus, types.ParseStatusProcessing)
	}
	if knowledge.ErrorMessage != "" {
		t.Fatalf("ErrorMessage = %q, want empty", knowledge.ErrorMessage)
	}
	if !knowledge.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %v, want %v", knowledge.UpdatedAt, now)
	}
}
