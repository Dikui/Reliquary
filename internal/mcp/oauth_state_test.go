package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestOAuthAttemptCompletesOnlyAfterCallbackExchange(t *testing.T) {
	ctx := context.Background()
	store := newOAuthStateStore()
	principal := types.Principal{Type: types.PrincipalWebUser, ID: "user-1"}
	state := OAuthState{
		TenantID:  7,
		Principal: principal,
		ServiceID: "service-1",
	}

	require.NoError(t, store.Put(ctx, "attempt-1", state))

	attempt, err := store.Attempt(ctx, "attempt-1")
	require.NoError(t, err)
	require.False(t, attempt.Completed)

	// Consuming the OAuth state means the provider callback started. It must
	// not look successful until the code exchange and token persistence finish.
	_, err = store.Take(ctx, "attempt-1")
	require.NoError(t, err)
	attempt, err = store.Attempt(ctx, "attempt-1")
	require.NoError(t, err)
	require.False(t, attempt.Completed)

	require.NoError(t, store.CompleteAttempt(ctx, "attempt-1"))
	attempt, err = store.Attempt(ctx, "attempt-1")
	require.NoError(t, err)
	require.True(t, attempt.Completed)
	require.Equal(t, uint64(7), attempt.TenantID)
	require.Equal(t, principal.Normalize(), attempt.Principal)
	require.Equal(t, "service-1", attempt.ServiceID)
}

func TestOAuthStateSingleUseAndExpiration(t *testing.T) {
	ctx := context.Background()
	store := newOAuthStateStore()
	value := OAuthState{TenantID: 1, ServiceID: "service", CodeVerifier: "secret"}
	require.NoError(t, store.Put(ctx, "single-use", value))
	got, err := store.Take(ctx, "single-use")
	require.NoError(t, err)
	require.Equal(t, value, got)
	_, err = store.Take(ctx, "single-use")
	require.ErrorContains(t, err, "not found or expired")
	require.NoError(t, store.Put(ctx, "expired", value))
	store.mu.Lock()
	entry := store.mem["expired"]
	entry.expiresAt = time.Now().Add(-time.Second)
	store.mem["expired"] = entry
	attempt := store.attempts["expired"]
	attempt.expiresAt = entry.expiresAt
	store.attempts["expired"] = attempt
	store.mu.Unlock()
	_, err = store.Take(ctx, "expired")
	require.ErrorContains(t, err, "not found or expired")
	_, err = store.Attempt(ctx, "expired")
	require.ErrorContains(t, err, "not found or expired")
	require.ErrorContains(t, store.CompleteAttempt(ctx, "expired"), "not found or expired")
	store.mu.Lock()
	require.NotContains(t, store.mem, "expired")
	require.NotContains(t, store.attempts, "expired")
	store.mu.Unlock()
}

func TestAuthorizationAttemptStatusIsScopedToPrincipalAndService(t *testing.T) {
	ctx := context.Background()
	manager := &OAuthManager{states: newOAuthStateStore()}
	principal := types.Principal{Type: types.PrincipalWebUser, ID: "user-1"}

	require.NoError(t, manager.states.Put(ctx, "attempt-1", OAuthState{
		TenantID:  7,
		Principal: principal,
		ServiceID: "service-1",
	}))
	require.NoError(t, manager.states.CompleteAttempt(ctx, "attempt-1"))

	completed, err := manager.IsAuthorizationAttemptComplete(
		ctx, 7, principal, "service-1", "attempt-1",
	)
	require.NoError(t, err)
	require.True(t, completed)

	_, err = manager.IsAuthorizationAttemptComplete(
		ctx, 7, types.Principal{Type: types.PrincipalWebUser, ID: "user-2"}, "service-1", "attempt-1",
	)
	require.ErrorContains(t, err, "does not match")

	_, err = manager.IsAuthorizationAttemptComplete(
		ctx, 7, principal, "service-2", "attempt-1",
	)
	require.ErrorContains(t, err, "does not match")
}
