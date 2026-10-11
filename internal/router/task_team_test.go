package router

import (
	"context"
	"errors"
	"testing"

	"reliquary/internal/queue"

	"reliquary/internal/types"
)

func TestBackgroundTaskMiddlewareUsesSingleTeam(t *testing.T) {
	for _, payload := range []string{`{"tenant_id":1}`, `{"knowledge_id":"article-1"}`, `{}`} {
		t.Run(payload, func(t *testing.T) {
			called := false
			handler := backgroundTaskMiddleware()(queue.HandlerFunc(func(ctx context.Context, task *queue.Task) error {
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
			if err := handler.ProcessTask(ctx, queue.NewTask("team:test", []byte(payload))); err != nil {
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
			handler := backgroundTaskMiddleware()(queue.HandlerFunc(func(context.Context, *queue.Task) error {
				t.Fatal("task outside the team reached its handler")
				return nil
			}))
			err := handler.ProcessTask(context.Background(), queue.NewTask("team:test", []byte(payload)))
			if !errors.Is(err, queue.SkipRetry) {
				t.Fatalf("error = %v, want SkipRetry", err)
			}
		})
	}
}
