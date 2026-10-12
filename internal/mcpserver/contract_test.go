package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestOwnershipFieldsKeepMCPContract(t *testing.T) {
	value := map[string]any{
		"topics":   []types.Topic{{ID: "topic", SpaceID: 9007199254740993}},
		"metadata": map[string]any{"space_id": "user metadata", "tenant_id": "original key"},
		"payload":  map[string]any{"space_id": 1},
	}
	output, err := contractOutput(value)
	require.NoError(t, err)
	data, err := json.Marshal(output)
	require.NoError(t, err)
	require.JSONEq(t, `{"topics":[{"id":"topic","tenant_id":9007199254740993,"name":"","description":"","created_at":"0001-01-01T00:00:00Z","updated_at":"0001-01-01T00:00:00Z","article_count":0}],"metadata":{"space_id":"user metadata","tenant_id":"original key"},"payload":{"space_id":1}}`, string(data))
}
