package mcpserver

import (
	"encoding/json"
	"reliquary/internal/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOwnershipFieldsKeepMCPContract(t *testing.T) {
	value := map[string]any{
		"metadata": map[string]any{"space_id": "user metadata", "tenant_id": "original key"},
		"payload":  map[string]any{"space_id": 1},
	}
	output, err := contractOutput(value)
	require.NoError(t, err)
	data, err := json.Marshal(output)
	require.NoError(t, err)
	require.JSONEq(
		t,
		`{"metadata":{"space_id":"user metadata","tenant_id":"original key"},"payload":{"space_id":1}}`,
		string(data),
	)
}

func TestSearchContractPreservesLibraryLocationsAndFlags(t *testing.T) {
	response := types.EntrySearchResponse{VectorUnavailable: true, RerankStatus: "unavailable", Results: []*types.EntrySearchResult{{EntryID: "entry", LibraryID: "library", LibraryName: "我的资料", LibraryDirectory: "personal", SourceUnverified: true, HasUpdate: true, LatestEntryID: "new", Snippets: []types.EntrySearchSnippet{{Content: "😀", Locations: []types.SourceLocation{{Start: 3, End: 4, Paragraph: 2, HeadingPath: []string{"标题"}, BodyHash: "hash", Mapping: "exact"}}}}}}}
	output, err := contractOutput(response)
	require.NoError(t, err)
	data := output.(map[string]any)
	require.Equal(t, true, data["vector_unavailable"])
	require.Equal(t, "unavailable", data["rerank_status"])
	result := data["results"].([]any)[0].(map[string]any)
	require.Equal(t, "library", result["library_id"])
	require.Equal(t, true, result["source_unverified"])
	require.NotEmpty(t, result["snippets"].([]any)[0].(map[string]any)["locations"])
}
