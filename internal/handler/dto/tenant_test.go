package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestTenantResponse_UserOmitsLegacyTenantAPIKey(t *testing.T) {
	tenant := sampleSecretTenant()
	body, err := json.Marshal(NewTenantResponse(userContext(), tenant))
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, `"api_key"`)
	assert.NotContains(t, s, "parser-secret-123")
}

func TestTenantResponse_UserGetsRedactedIntegrationConfigs(t *testing.T) {
	tenant := sampleSecretTenant()
	resp := NewTenantResponse(userContext(), tenant)
	require.NotNil(t, resp.ParserEngineConfig)
	assert.Equal(t, types.RedactedSecretPlaceholder, resp.ParserEngineConfig.MinerUAPIKey)
	require.NotNil(t, resp.Credentials)
	require.NotNil(t, resp.Credentials.WeKnoraCloud)
	assert.Equal(t, types.RedactedSecretPlaceholder, resp.Credentials.WeKnoraCloud.AppSecret)
	assert.Equal(t, "wk-app-secret-def", tenant.Credentials.WeKnoraCloud.AppSecret)
}

func sampleSecretTenant() *types.Tenant {
	return &types.Tenant{
		ID:   42,
		Name: "tenant",
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
