package handler

import (
	"context"
	"net/http"
	"testing"

	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func TestRequireTaskProgressSpace_RejectsCrossSpace(t *testing.T) {
	taskID := utils.GenerateTaskID("document_process", 999, "kb-victim")
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))

	err := requireTaskProgressSpace(ctx, taskID)
	if err == nil {
		t.Fatal("expected cross-space task to be rejected")
	}
}

func TestRequireTaskProgressSpace_AllowsOwnSpace(t *testing.T) {
	taskID := utils.GenerateTaskID("entry_move", 1, "kb-source")
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))

	if err := requireTaskProgressSpace(ctx, taskID); err != nil {
		t.Fatalf("expected own-space task to pass, got %v", err)
	}
}

func TestRequireTaskProgressSpace_InvalidTaskID(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	err := requireTaskProgressSpace(ctx, "not-a-task")
	if err == nil {
		t.Fatal("expected invalid task ID to fail")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected error message")
	}
	_ = http.StatusBadRequest
}
