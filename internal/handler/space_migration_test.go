package handler

import (
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"testing"
)

func TestAPIKeyMigrationStatus(t *testing.T) {
	key := &types.APIKey{ID: 15, KeyHash: "disabled-15"}
	require.Equal(t, "migration_invalidated", apiKeyForResponse(key).Status)
	key.KeyHash = "disabled-16"
	require.Equal(t, "active", apiKeyForResponse(key).Status)
	key.KeyHash = "disabled-15"
	key.APIKey = "new-key"
	require.Equal(t, "active", apiKeyForResponse(key).Status)
}
