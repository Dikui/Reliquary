package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestOperationValuesKeepPersistedOwnershipKey(t *testing.T) {
	for _, value := range []any{
		types.Topic{SpaceID: 1, ID: "topic"},
		&types.Topic{SpaceID: 1, ID: "topic"},
		types.EntryTopic{SpaceID: 1, EntryID: "article", TopicID: "topic"},
		[]types.EntryTopic{{SpaceID: 1, EntryID: "article", TopicID: "topic"}},
	} {
		data, err := marshalOperationValue(value)
		require.NoError(t, err)
		require.Contains(t, string(data), `"tenant_id":1`)
		require.NotContains(t, string(data), `"space_id"`)
	}
	value := map[string]any{"space_id": uint64(9007199254740993), "tenant_id": "user JSON"}
	data, err := marshalOperationValue(value)
	require.NoError(t, err)
	expected, err := json.Marshal(value)
	require.NoError(t, err)
	require.Equal(t, expected, data)
}
