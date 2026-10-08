package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestAuthLoginResponse_OmitsActiveTenantSecrets(t *testing.T) {
	tenant := sampleSecretTenant()
	resp := NewAuthLoginResponse(&types.LoginResponse{
		Success:      true,
		ActiveTenant: tenant,
	})
	body, err := json.Marshal(resp)
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, "tenant-api-key-123")
	assert.NotContains(t, s, "parser-secret-123")
}

func TestAuthLoginResponse_OmitsLegacyTenantAPIKey(t *testing.T) {
	tenant := sampleSecretTenant()
	resp := NewAuthLoginResponse(&types.LoginResponse{
		Success:      true,
		ActiveTenant: tenant,
	})
	body, err := json.Marshal(resp)
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, `"api_key"`)
}
