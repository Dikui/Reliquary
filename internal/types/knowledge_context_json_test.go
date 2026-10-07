package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKnowledgeContextResponsesEncodeEmptyListsAsArrays(t *testing.T) {
	for _, test := range []struct {
		name   string
		value  any
		fields []string
	}{
		{"result", KnowledgeContextResult{}, []string{"entities", "facts", "events", "evidence", "attention"}},
		{"entity", KnowledgeEntity{}, []string{"aliases"}},
		{"event", KnowledgeEvent{}, []string{"sources", "entity_ids", "evidence_ids", "relations"}},
		{"evidence", KnowledgeEvidence{}, []string{"image_refs", "model_calls"}},
		{"legacy evidence", KnowledgeEvidence{ModelCalls: JSON(" null ")}, []string{"image_refs", "model_calls"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			require.NoError(t, err)
			var payload map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &payload))
			for _, field := range test.fields {
				require.JSONEq(t, "[]", string(payload[field]), field)
			}
		})
	}
}

func TestKnowledgeContextResponsePreservesPopulatedLists(t *testing.T) {
	result := KnowledgeContextResult{
		Entities: []KnowledgeEntity{{ID: "object", Aliases: []string{"Alias"}}},
		Events: []KnowledgeEvent{{ID: "event", EntityIDs: StringArray{"object"}, EvidenceIDs: StringArray{"source"},
			Sources: []KnowledgeContextSource{{KnowledgeBaseID: "kb"}}, Relations: []KnowledgeEventRelation{{RelatedEventID: "next"}}}},
		Evidence: []KnowledgeEvidence{{ID: "source", ImageRefs: StringArray{"image"}, ModelCalls: JSON(`[{"stage":"extract","calls":1}]`)}},
	}
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	var decoded KnowledgeContextResult
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, result.Entities, decoded.Entities)
	require.Equal(t, result.Events, decoded.Events)
	require.Equal(t, result.Evidence, decoded.Evidence)
	require.Nil(t, result.Facts, "serializing must not mutate the stored record")
}
