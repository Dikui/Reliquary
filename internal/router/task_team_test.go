package router

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"

	"reliquary/internal/types"
)

func TestBackgroundTaskMiddlewareUsesSingleTeam(t *testing.T) {
	for _, payload := range []string{`{"tenant_id":1}`, `{"session_id":"s-1"}`, `{}`} {
		t.Run(payload, func(t *testing.T) {
			called := false
			handler := backgroundTaskMiddleware()(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
				called = true
				tenantID, ok := types.TenantIDFromContext(ctx)
				if !ok || tenantID != types.SingleTenantID {
					t.Fatalf("tenant = %d, present = %v", tenantID, ok)
				}
				if !types.IsBackgroundTask(ctx) {
					t.Fatal("background task marker missing")
				}
				if string(task.Payload()) != payload {
					t.Fatal("task payload changed")
				}
				return nil
			}))
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(42))
			if err := handler.ProcessTask(ctx, asynq.NewTask("team:test", []byte(payload))); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("valid task was not dispatched")
			}
		})
	}
}

func TestBackgroundTaskMiddlewareRejectsOtherTenant(t *testing.T) {
	for _, payload := range []string{
		`{"tenant_id":2}`, `{"tenant_id":0}`, `{"tenant_id":null}`,
		`{"tenant_id":"1"}`, `{"TENANT_ID":2}`, `{"tenant_id":1,"TENANT_ID":2}`,
	} {
		t.Run(payload, func(t *testing.T) {
			handler := backgroundTaskMiddleware()(asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
				t.Fatal("task outside the team reached its handler")
				return nil
			}))
			err := handler.ProcessTask(context.Background(), asynq.NewTask("team:test", []byte(payload)))
			if !errors.Is(err, asynq.SkipRetry) {
				t.Fatalf("error = %v, want SkipRetry", err)
			}
		})
	}
}
