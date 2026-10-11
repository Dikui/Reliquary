package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestSpaceResponse_UserOmitsLegacyAPIKey(t *testing.T) {
	space := sampleSecretSpace()
	body, err := json.Marshal(NewSpaceResponse(userContext(), space))
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, `"api_key"`)
	assert.NotContains(t, s, "parser-secret-123")
}

func TestSpaceResponse_UserGetsRedactedIntegrationConfigs(t *testing.T) {
	space := sampleSecretSpace()
	resp := NewSpaceResponse(userContext(), space)
	require.NotNil(t, resp.ParserEngineConfig)
	assert.Equal(t, types.RedactedSecretPlaceholder, resp.ParserEngineConfig.MinerUAPIKey)
	require.NotNil(t, resp.Credentials)
	require.NotNil(t, resp.Credentials.WeKnoraCloud)
	assert.Equal(t, types.RedactedSecretPlaceholder, resp.Credentials.WeKnoraCloud.AppSecret)
	assert.Equal(t, "wk-app-secret-def", space.Credentials.WeKnoraCloud.AppSecret)
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
		ParserEngineConfig: &types.ParserEngineConfig{
			MinerUAPIKey: "parser-secret-123",
		},
	}
}
