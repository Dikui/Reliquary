package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLibraryTaskEnvelopeReadsHistoricalKeysAndWritesCurrentKeys(t *testing.T) {
	for _, key := range []string{"knowledge_base_id", "library_id"} {
		var payload ManualProcessPayload
		err := json.Unmarshal([]byte(`{"tenant_id":1,"knowledge_id":"entry","`+key+`":"library"}`), &payload)
		require.NoError(t, err)
		require.Equal(t, "library", payload.LibraryID)
		require.Equal(t, "entry", payload.EntryID)
		data, err := json.Marshal(payload)
		require.NoError(t, err)
		require.Contains(t, string(data), `"library_id":"library"`)
		require.NotContains(t, string(data), "knowledge_base_id")
	}
	for _, data := range []string{
		`{"knowledge_base_id":"old","library_id":"different"}`,
		`{"library_id":42}`,
	} {
		var payload ManualProcessPayload
		require.Error(t, json.Unmarshal([]byte(data), &payload), data)
	}
	var payload ManualProcessPayload
	require.NoError(t, json.Unmarshal([]byte(`{"knowledge_base_id":null}`), &payload))
	require.Empty(t, payload.LibraryID, "retain the original string-field null behavior")
}
