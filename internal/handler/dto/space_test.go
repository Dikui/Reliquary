package dto

import (
	"encoding/json"
	"testing"

	"reliquary/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpaceResponse_UserOmitsLegacyAPIKey(t *testing.T) {
	space := sampleSecretSpace()
	body, err := json.Marshal(NewSpaceResponse(userContext(), space))
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, `"api_key"`)
	assert.NotContains(t, s, "parser-secret-123")
}

func sampleSecretSpace() *types.Space {
	return &types.Space{
		ID:   42,
		Name: "space",
		Credentials: &types.CredentialsConfig{
			WeKnoraCloud: &types.WeKnoraCloudCredentials{
				AppID:     "wk-app-id-abc",
				AppSecret: "wk-app-secret-def",
			},
		},
	}
}
