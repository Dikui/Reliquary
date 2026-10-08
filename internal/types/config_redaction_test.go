package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeParserEngineConfigForUpdate_PreservesRedactedSecrets(t *testing.T) {
	existing := &ParserEngineConfig{
		MinerUAPIKey:   "mineru-secret",
		MinerUEndpoint: "http://mineru",
	}
	incoming := &ParserEngineConfig{
		MinerUAPIKey:   RedactedSecretPlaceholder,
		MinerUEndpoint: "http://mineru-new",
	}
	merged := MergeParserEngineConfigForUpdate(incoming, existing)
	require.NotNil(t, merged)
	assert.Equal(t, "mineru-secret", merged.MinerUAPIKey)
	assert.Equal(t, "http://mineru-new", merged.MinerUEndpoint)
}

func TestMergeParserEngineConfigForUpdate_PreservesLegacyChatParserRules(t *testing.T) {
	existing := &ParserEngineConfig{
		ChatParserEngineRules: []ParserEngineRule{
			{FileTypes: []string{"pdf"}, Engine: "mineru"},
		},
	}
	incoming := &ParserEngineConfig{
		MinerUEndpoint: "http://mineru-new",
	}
	merged := MergeParserEngineConfigForUpdate(incoming, existing)
	require.NotNil(t, merged)
	require.Len(t, merged.ChatParserEngineRules, 1)
	assert.Equal(t, "mineru", merged.ChatParserEngineRules[0].Engine)
}

func TestParserEngineConfigForResponse_NilSafe(t *testing.T) {
	assert.Nil(t, ParserEngineConfigForResponse(nil, true))
}
