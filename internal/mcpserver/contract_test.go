package mcpserver

import (
	"encoding/json"
	"fmt"
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
	require.JSONEq(t, `{"topics":[{"id":"topic","tenant_id":9007199254740993,"name":"","description":"","library_ids":null,"created_at":"0001-01-01T00:00:00Z","updated_at":"0001-01-01T00:00:00Z","entry_count":0}],"metadata":{"space_id":"user metadata","tenant_id":"original key"},"payload":{"space_id":1}}`, string(data))
}

func TestSearchAndFollowFeedContractsIncludeIncompleteBoolean(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		article := types.EntrySearchResult{EntryID: "article", Source: "article", Incomplete: incomplete,
			Snippets: []types.EntrySearchSnippet{}, SimilarReports: []*types.EntrySearchResult{
				{EntryID: "similar", Source: "article", Incomplete: !incomplete, Snippets: []types.EntrySearchSnippet{}},
			}}
		for name, response := range map[string]any{
			"search": types.EntrySearchResponse{Results: []*types.EntrySearchResult{&article}},
			"follow_feed": types.FollowFeedResponse{Results: []*types.FollowFeedArticle{{EntrySearchResult: article,
				SimilarReports: []*types.FollowFeedArticle{{EntrySearchResult: *article.SimilarReports[0]}}}}},
		} {
			t.Run(fmt.Sprintf("%s/incomplete=%t", name, incomplete), func(t *testing.T) {
				output, err := contractOutput(response)
				require.NoError(t, err)
				result := output.(map[string]any)["results"].([]any)[0].(map[string]any)
				require.Equal(t, incomplete, result["incomplete"])
				require.Equal(t, "article", result["entry_id"])
				member := result["similar_reports"].([]any)[0].(map[string]any)
				require.Equal(t, !incomplete, member["incomplete"])
				require.Equal(t, "similar", member["entry_id"])
			})
		}
	}
}

func TestSearchContractPreservesLibraryLocationsAndFlags(t *testing.T) {
	response := types.EntrySearchResponse{VectorUnavailable: true, RerankStatus: "unavailable", Results: []*types.EntrySearchResult{{EntryID: "entry", LibraryID: "library", LibraryName: "我的资料", LibraryDirectory: "mine", SourceUnverified: true, HasUpdate: true, LatestEntryID: "new", Snippets: []types.EntrySearchSnippet{{Content: "😀", Locations: []types.SourceLocation{{Start: 3, End: 4, Paragraph: 2, HeadingPath: []string{"标题"}, BodyHash: "hash", Mapping: "exact"}}}}}}}
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
