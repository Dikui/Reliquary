package mcpserver

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
	"testing"
)

func TestEntryContractFieldSetsAndExample(t *testing.T) {
	value := map[string]any{
		"entry":         types.Entry{ID: "item", SpaceID: 1, KnowledgeBaseID: "kb", SourceKind: "mine", Metadata: types.JSON(`{"knowledge_id":"opaque","entry_id":"user value"}`)},
		"entity_detail": types.EntityDetail{Entries: []types.Entry{{ID: "item", SpaceID: 1}}},
		"search":        types.EntrySearchResponse{Results: []*types.EntrySearchResult{{EntryID: "item", Source: "mine", Snippets: []types.EntrySearchSnippet{{ChunkID: "chunk", Content: "Evidence", Score: 0.75}}, SimilarReports: []*types.EntrySearchResult{}}}},
		"association":   types.EntryTopic{SpaceID: 1, EntryID: "item", TopicID: "topic"},
		"read":          service.EntryReadResult{Header: service.VaultArticleHeader{ID: "item", Title: "Original header"}, Body: "Original body", Length: 13, TotalLength: 13},
	}
	output, err := contractOutput(value)
	require.NoError(t, err)
	data, err := json.MarshalIndent(output, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("NAMING_MCP_EVIDENCE"); path != "" {
		require.NoError(t, os.WriteFile(path, append(data, '\n'), 0600))
		return
	}
	expected, err := os.ReadFile("testdata/entry-contract.json")
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(data))
}
