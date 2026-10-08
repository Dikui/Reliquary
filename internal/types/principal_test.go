package types

import (
	"context"
	"testing"
)

func TestPrincipalFromContextFallsBackToWebUser(t *testing.T) {
	ctx := context.WithValue(context.Background(), UserIDContextKey, "u1")

	p, ok := PrincipalFromContext(ctx)
	if !ok {
		t.Fatal("expected principal")
	}
	if p.Type != PrincipalWebUser || p.ID != "u1" {
		t.Fatalf("principal = %#v", p)
	}
}

func TestWithPrincipalRejectsBlankValues(t *testing.T) {
	ctx := WithPrincipal(context.Background(), Principal{Type: " ", ID: "x"})

	if _, ok := PrincipalFromContext(ctx); ok {
		t.Fatal("blank principal type should not be stored")
	}
}

func TestPrincipalStorageID(t *testing.T) {
	p := Principal{Type: PrincipalWebUser, ID: "u1"}

	if got := p.StorageID(); got != "web_user:u1" {
		t.Fatalf("StorageID() = %q", got)
	}
}

func TestSessionOwnerIDFromContextUsesAuthenticatedUser(t *testing.T) {
	ctx := WithPrincipal(context.Background(), Principal{
		Type: PrincipalWebUser,
		ID:   "user-7",
	})
	ctx = context.WithValue(ctx, UserIDContextKey, "user-7")

	if got := SessionOwnerIDFromContext(ctx); got != "user-7" {
		t.Fatalf("SessionOwnerIDFromContext() = %q", got)
	}
}

func TestSessionOwnerIDFromContextFallsBackToUserID(t *testing.T) {
	ctx := context.WithValue(context.Background(), UserIDContextKey, "user-7")

	if got := SessionOwnerIDFromContext(ctx); got != "user-7" {
		t.Fatalf("SessionOwnerIDFromContext() = %q", got)
	}
}
