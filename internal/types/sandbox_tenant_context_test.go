package types

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every path that does not borrow another workspace's tenant must keep working
// without setting the key at all: there the request tenant already is the
// session owner. Session deletion relies on exactly this fallback.
func TestSandboxTenantIDFallsBackToRequestTenant(t *testing.T) {
	ctx := context.WithValue(context.Background(), TenantIDContextKey, uint64(7))

	got, ok := SandboxTenantIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, uint64(7), got)
}

// A zero tenant would silently poison the binding key, so it is ignored rather
// than recorded.
func TestWithSandboxTenantIDIgnoresZero(t *testing.T) {
	ctx := context.WithValue(context.Background(), TenantIDContextKey, uint64(7))
	ctx = WithSandboxTenantID(ctx, 0)

	got, ok := SandboxTenantIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, uint64(7), got)
}
