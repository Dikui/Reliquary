package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpacePayloadAcceptsBothKeysWithoutChangingPersistentWriters(t *testing.T) {
	for _, data := range []string{`{"tenant_id":1}`, `{"space_id":1}`, `{"tenant_id":1,"space_id":1}`} {
		payloads := []any{&DocumentProcessPayload{}, &ManualProcessPayload{}, &EntryListDeletePayload{}, &EntryListReparsePayload{}, &TopicBackfillPayload{}, &DataSourceSyncPayload{}, &EmbeddingRebuildPayload{}, &EntityNameEmbeddingRebuildPayload{}}
		for _, payload := range payloads {
			require.NoError(t, json.Unmarshal([]byte(data), payload))
			encoded, err := json.Marshal(payload)
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, json.Unmarshal(encoded, &fields))
			require.Equal(t, float64(1), fields["tenant_id"])
			require.NotContains(t, fields, "space_id")
		}
	}
	for _, data := range []string{`{"tenant_id":1,"space_id":2}`, `{"TENANT_ID":2,"tenant_id":1}`, `{"space_id":null}`, `{"tenant_id":"1"}`} {
		var payload DocumentProcessPayload
		require.Error(t, json.Unmarshal([]byte(data), &payload))
	}
}
