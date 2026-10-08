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
	assert.NotContains(t, s, "legacy-search-secret-999")
	assert.NotContains(t, s, "parser-secret-123")
	assert.Contains(t, s, "web_search_config")
}

func TestTenantResponse_UserGetsRedactedIntegrationConfigs(t *testing.T) {
	tenant := sampleSecretTenant()
	resp := NewTenantResponse(userContext(), tenant)
	require.NotNil(t, resp.WebSearchConfig)
	assert.Equal(t, types.RedactedSecretPlaceholder, resp.WebSearchConfig.ProxyURL)
	assert.Empty(t, resp.WebSearchConfig.APIKey)
	require.NotNil(t, resp.ParserEngineConfig)
	assert.Equal(t, types.RedactedSecretPlaceholder, resp.ParserEngineConfig.MinerUAPIKey)
	require.NotNil(t, resp.StorageEngineConfig.MinIO)
	assert.Equal(t, types.RedactedSecretPlaceholder, resp.StorageEngineConfig.MinIO.SecretAccessKey)
	require.NotNil(t, resp.Credentials)
	require.NotNil(t, resp.Credentials.WeKnoraCloud)
	assert.Equal(t, types.RedactedSecretPlaceholder, resp.Credentials.WeKnoraCloud.AppSecret)
	assert.Equal(t, "wk-app-secret-def", tenant.Credentials.WeKnoraCloud.AppSecret)
}

func sampleSecretTenant() *types.Tenant {
	return &types.Tenant{
		ID:   42,
		Name: "tenant",
		WebSearchConfig: &types.WebSearchConfig{
			APIKey:   "legacy-search-secret-999",
			ProxyURL: "http://proxy.internal:8080",
		},
		Credentials: &types.CredentialsConfig{
			WeKnoraCloud: &types.WeKnoraCloudCredentials{
				AppID:     "wk-app-id-abc",
				AppSecret: "wk-app-secret-def",
			},
		},
		ParserEngineConfig: &types.ParserEngineConfig{
			MinerUAPIKey:          "parser-secret-123",
			PaddleOCRVLCloudToken: "paddle-secret-456",
		},
		StorageEngineConfig: &types.StorageEngineConfig{
			DefaultProvider: "minio",
			MinIO: &types.MinIOEngineConfig{
				AccessKeyID:     "minio-access-id",
				SecretAccessKey: "minio-secret-789",
			},
		},
	}
}
