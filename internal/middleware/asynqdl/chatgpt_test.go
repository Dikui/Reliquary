package asynqdl

import (
	"context"
	"fmt"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"testing"
	"time"
)

func TestChatGPTDeferredTaskNeverEntersDeadLetter(t *testing.T) {
	repo := &fakeRepo{}
	called := false
	paused := &types.ChatGPTDeferredError{Reason: "daily_limit", RetryAt: time.Now().Add(time.Hour)}
	paused.Cause = &types.ChatGPTError{Code: "subscription_sharing_unsupported_capability", Message: "fallback failed"}
	err := fmt.Errorf("article call: %w", paused)
	wrapped := MiddlewareWithCallback(repo, func(context.Context, *asynq.Task, error) { called = true })(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return err }))
	got := wrapped.ProcessTask(context.Background(), asynq.NewTask("knowledge:document", []byte(`{"knowledge_id":"pending"}`)))
	require.ErrorIs(t, got, paused)
	require.Zero(t, repo.rowCount())
	require.False(t, called)
}
