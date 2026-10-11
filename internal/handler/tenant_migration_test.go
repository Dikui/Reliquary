package handler

import (
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"testing"
)

func TestTenantAPIKeyMigrationStatus(t *testing.T) {
	key := &types.TenantAPIKey{ID: 15, KeyHash: "disabled-15"}
	require.Equal(t, "migration_invalidated", tenantAPIKeyForResponse(key).Status)
	key.KeyHash = "disabled-16"
	require.Equal(t, "active", tenantAPIKeyForResponse(key).Status)
	key.KeyHash = "disabled-15"
	key.APIKey = "new-key"
	require.Equal(t, "active", tenantAPIKeyForResponse(key).Status)
}
