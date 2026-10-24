package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOperationValuesKeepPersistedOwnershipKey(t *testing.T) {
	value := map[string]any{"space_id": uint64(9007199254740993), "tenant_id": "user JSON"}
	data, err := marshalOperationValue(value)
	require.NoError(t, err)
	expected, err := json.Marshal(value)
	require.NoError(t, err)
	require.Equal(t, expected, data)
}
