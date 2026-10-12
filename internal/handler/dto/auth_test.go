package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestAuthLoginResponse_OmitsActiveSpaceSecrets(t *testing.T) {
	space := sampleSecretSpace()
	resp := NewAuthLoginResponse(&types.LoginResponse{
		Success:     true,
		ActiveSpace: space,
	})
	body, err := json.Marshal(resp)
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, "space-api-key-123")
	assert.NotContains(t, s, "parser-secret-123")
}

func TestAuthLoginResponse_OmitsLegacyAPIKey(t *testing.T) {
	space := sampleSecretSpace()
	resp := NewAuthLoginResponse(&types.LoginResponse{
		Success:     true,
		ActiveSpace: space,
	})
	body, err := json.Marshal(resp)
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, `"api_key"`)
}
