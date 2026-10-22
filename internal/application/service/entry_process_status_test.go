package service

import (
	"testing"
	"time"

	"reliquary/internal/types"
)

func TestFinalizeIndexedEntryState(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	entry := &types.Entry{ParseStatus: types.ParseStatusProcessing, ErrorMessage: "previous failure", Description: "summary", SummaryStatus: types.SummaryStatusPending}
	finalizeIndexedEntryState(entry, 4096, now)
	if entry.ParseStatus != types.ParseStatusCompleted || entry.SummaryStatus != types.SummaryStatusPending || entry.ErrorMessage != "" || entry.Description != "summary" {
		t.Fatalf("unexpected completion: %+v", entry)
	}
	if entry.ProcessedAt == nil || !entry.ProcessedAt.Equal(now) || entry.StorageSize != 4096 || entry.EnableStatus != "enabled" {
		t.Fatalf("invalid indexed state: %+v", entry)
	}
}

// TestMarkEntryProcessingClearsPreviousAttemptError covers the transition
// every worker performs before it starts a new attempt. A row that failed
// earlier still carries that attempt's error_message, and leaving it in place
// makes the UI report a failure on a document it is simultaneously showing as
// processing.
func TestMarkEntryProcessingClearsPreviousAttemptError(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	entry := &types.Entry{
		ParseStatus:  types.ParseStatusFailed,
		ErrorMessage: "Task interrupted due to application restart",
	}

	markEntryProcessing(entry, now)

	if entry.ParseStatus != types.ParseStatusProcessing {
		t.Fatalf("ParseStatus = %q, want %q", entry.ParseStatus, types.ParseStatusProcessing)
	}
	if entry.ErrorMessage != "" {
		t.Fatalf("ErrorMessage = %q, want empty", entry.ErrorMessage)
	}
	if !entry.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %v, want %v", entry.UpdatedAt, now)
	}
}
