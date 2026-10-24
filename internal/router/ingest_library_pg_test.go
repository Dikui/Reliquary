package router

import (
	"encoding/json"
	"net/http"
	"reliquary/internal/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIngestLibraryHTTPAndMCPAgreement(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	router := ingestAuthHTTPRouter(f)
	client := f.connect(t, "2025-11-25")
	send := func(protocol string, items []any) types.IngestBatchResult {
		input := map[string]any{"items": items}
		var result types.IngestBatchResult
		if protocol == "mcp" {
			mcpCall(t, client, "ingest", input, &result)
		} else {
			data, err := json.Marshal(input)
			require.NoError(t, err)
			response := personalRequest(t, router, http.MethodPost, "/api/v1/ingest", string(data), f.key, true)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var envelope struct{ Data types.IngestBatchResult }
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
			result = envelope.Data
		}
		return result
	}
	var libraries []types.Library
	require.NoError(t, f.db.Find(&libraries).Error)
	ids := map[string]string{}
	for _, library := range libraries {
		ids[library.Directory] = library.ID
	}
	for _, protocol := range []string{"http", "mcp"} {
		items := []any{
			map[string]any{"content": "Missing library", "acquisition": "verbatim"},
			map[string]any{"library": "unknown", "content": "Unknown library", "acquisition": "verbatim"},
			map[string]any{
				"library":     "personal",
				"content":     "My original " + protocol,
				"acquisition": "verbatim",
				"source_ref":  protocol,
			},
			map[string]any{
				"library":     ids["personal"],
				"content":     "My original " + protocol,
				"acquisition": "verbatim",
				"source_ref":  protocol,
			},
			map[string]any{
				"library":     "official",
				"content":     "My original",
				"acquisition": "verbatim",
				"source_ref":  "wrong",
			},
			map[string]any{
				"library":     "personal",
				"content":     "My original",
				"acquisition": "verbatim",
				"identifier":  "doi:wrong",
			},
			map[string]any{
				"library":     "references",
				"content":     "Research from " + protocol,
				"acquisition": "verbatim",
				"identifier":  " DOI: 10.1234/" + protocol + " ",
				"url":         "https://example.org/" + protocol,
				"publisher":   "Journal",
			},
			map[string]any{
				"library":     "ai-output",
				"title":       "Agent findings " + protocol,
				"content":     "# Analysis\n\nGenerated observations.",
				"acquisition": "model_written",
			},
		}
		result := send(protocol, items)
		require.Len(t, result.Items, len(items))
		for _, i := range []int{0, 1} {
			require.Equal(t, "invalid", result.Items[i].Status)
			require.NotEmpty(t, result.Items[i].Reason)
		}
		require.Contains(t, result.Items[0].Reason, "list_libraries")
		require.Contains(t, result.Items[1].Reason, "list_libraries")
		require.Equal(t, "accepted", result.Items[2].Status)
		for _, i := range []int{4, 5} {
			require.Contains(t, []string{"accepted", "exists"}, result.Items[i].Status)
		}
		require.Equal(t, "exists", result.Items[3].Status)
		require.Equal(t, result.Items[2].EntryID, result.Items[3].EntryID)
		require.Equal(t, "doi:10.1234/"+protocol, result.Items[6].Identifier)
		require.Equal(t, ids["ai-output"], result.Items[7].LibraryID)
	}
	var checked types.IngestCheckResponse
	mcpCall(t, client, "check_urls", map[string]any{"urls": []string{"https://example.org/a", "https://example.org/b"}}, &checked)
	require.Len(t, checked.Items, 2)
	require.Equal(t, "invalid", checked.Items[0].Status)
	for _, library := range []string{"references", ids["references"]} {
		data, _ := json.Marshal(types.IngestCheckRequest{Library: library, URLs: []string{"https://example.org/http"}})
		response := personalRequest(t, router, http.MethodPost, "/api/v1/ingest/check", string(data), f.key, true)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var result struct{ Data types.IngestCheckResponse }
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.Equal(t, "in_library", result.Data.Items[0].Status)
	}

	// check_urls also returns one invalid result per input URL when no library is supplied.
	response := personalRequest(t, router, http.MethodPost, "/api/v1/ingest/check", `{"urls":["https://example.org/a","https://example.org/b"]}`, f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var checks struct{ Data types.IngestCheckResponse }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &checks))
	require.Len(t, checks.Data.Items, 2)
	require.Equal(t, "invalid", checks.Data.Items[0].Status)
}
