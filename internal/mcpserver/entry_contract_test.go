package mcpserver

import (
	"encoding/json"
	"os"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntryContractFieldSetsAndExample(t *testing.T) {
	value := map[string]any{
		"entry":         types.Entry{ID: "item", SpaceID: 1, LibraryID: "library", SourceKind: "mine", Metadata: types.JSON(`{"custom_id":"opaque","entry_id":"user value"}`)},
		"entity_detail": types.EntityDetail{Entries: []types.Entry{{ID: "item", SpaceID: 1}}},
		"search": types.EntrySearchResponse{
			Results: []*types.EntrySearchResult{
				{
					EntryID:  "item",
					Source:   "mine",
					Snippets: []types.EntrySearchSnippet{{ChunkID: "chunk", Content: "Evidence", Score: 0.75}},
				},
			},
		},
		"association": types.EntryEntity{SpaceID: 1, EntryID: "item", EntityID: "entity"},
		"read": service.EntryReadResult{
			Header:      service.VaultArticleHeader{ID: "item", Title: "Original header"},
			Body:        "Original body",
			Length:      13,
			TotalLength: 13,
		},
	}
	output, err := contractOutput(value)
	require.NoError(t, err)
	data, err := json.MarshalIndent(output, "", "  ")
	require.NoError(t, err)
	require.Contains(t, string(data), `"library_id"`)
	require.Contains(t, string(data), `"library_name"`)
	require.NotContains(t, string(data), "knowledge_base")
	if path := os.Getenv("NAMING_MCP_EVIDENCE"); path != "" {
		require.NoError(t, os.WriteFile(path, append(data, '\n'), 0o600))
		return
	}
	expected, err := os.ReadFile("testdata/entry-contract.json")
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(data))
}
