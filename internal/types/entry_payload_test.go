package types

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEntryPayloadGenerationsAndPersistedWriterKeys(t *testing.T) {
	for _, key := range []string{"knowledge_id", "entry_id", "ENTRY_ID"} {
		var p DocumentProcessPayload
		require.NoError(t, json.Unmarshal([]byte(`{"space_id":1,"`+key+`":"item"}`), &p))
		require.Equal(t, "item", p.EntryID)
		encoded, err := json.Marshal(p)
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(encoded, &fields))
		require.Contains(t, fields, "knowledge_id")
		require.NotContains(t, fields, "entry_id")
	}
	for _, key := range []string{"knowledge_ids", "entry_ids"} {
		var p EntryListReparsePayload
		require.NoError(t, json.Unmarshal([]byte(`{"space_id":1,"`+key+`":["a","b"]}`), &p))
		require.Equal(t, []string{"a", "b"}, p.EntryIDs)
		data, err := json.Marshal(p)
		require.NoError(t, err)
		require.Contains(t, string(data), `"knowledge_ids"`)
	}
	for _, raw := range []string{`{"knowledge_id":"a","entry_id":"b"}`, `{"entry_id":12}`, `{"entry_id":null}`, `{"knowledge_ids":["a"],"entry_ids":["b"]}`, `{"entry_ids":[1]}`} {
		var p DocumentProcessPayload
		require.Error(t, json.Unmarshal([]byte(raw), &p), raw)
	}
	var p ManualProcessPayload
	require.NoError(t, json.Unmarshal([]byte(`{"knowledge_id":"a","entry_id":"a"}`), &p))
	require.Equal(t, "a", p.EntryID)
	require.Equal(t, "knowledge:list_delete", TypeEntryListDelete)
	require.Equal(t, "knowledge:list_reparse", TypeEntryListReparse)
	require.Equal(t, "knowledge:embedding_rebuild", TypeEmbeddingRebuild)
}
