package types

import (
	"context"
	"testing"
)

func TestTaskInitiatorRoundTrip(t *testing.T) {
	requestCtx := context.WithValue(context.Background(), UserIDContextKey, "user-1")

	initiator := TaskInitiatorFromContext(requestCtx)
	if initiator.UserID != "user-1" {
		t.Fatalf("TaskInitiatorFromContext() = %#v", initiator)
	}

	workerCtx := initiator.Apply(context.Background())
	if userID, ok := UserIDFromContext(workerCtx); !ok || userID != "user-1" {
		t.Fatalf("restored user = %q, %v", userID, ok)
	}
}

func TestTaskInitiatorOmitsEmptyActor(t *testing.T) {
	legacyCtx := (TaskInitiator{}).Apply(context.Background())
	if userID, ok := UserIDFromContext(legacyCtx); ok || userID != "" {
		t.Fatalf("legacy user = %q, %v", userID, ok)
	}
}

func TestTaskInitiatorPreservesAPIKeyWithoutWebUser(t *testing.T) {
	principal := Principal{Type: PrincipalAPIKey, ID: "17"}
	ctx := WithPrincipal(context.Background(), principal)
	got, ok := PrincipalFromContext(TaskInitiatorFromContext(ctx).Apply(context.Background()))
	if !ok || got != principal {
		t.Fatalf("restored API key = %#v, %v", got, ok)
	}
}
