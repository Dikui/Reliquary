package router

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
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
			map[string]any{"library": "mine", "content": "My original " + protocol, "acquisition": "verbatim", "source_ref": protocol, "graph": false},
			map[string]any{"library": ids["mine"], "content": "My original " + protocol, "acquisition": "verbatim", "source_ref": protocol, "graph": false},
			map[string]any{"library": "institutional", "content": "My original", "acquisition": "verbatim", "source_ref": "wrong"},
			map[string]any{"library": "mine", "content": "My original", "acquisition": "verbatim", "identifier": "doi:wrong"},
			map[string]any{"library": "references", "content": "Research from " + protocol, "acquisition": "verbatim", "identifier": " DOI: 10.1234/ABC ", "url": "https://example.org/" + protocol, "publisher": "Journal"},
			map[string]any{"library": "ai-reports", "title": "Agent findings " + protocol, "content": "# Analysis\n\nGenerated observations.", "acquisition": "model_written"},
		}
		result := send(protocol, items)
		require.Len(t, result.Items, len(items))
		for _, i := range []int{0, 1, 4, 5} {
			require.Equal(t, "invalid", result.Items[i].Status)
			require.NotEmpty(t, result.Items[i].Reason)
		}
		require.Contains(t, result.Items[0].Reason, "list_libraries")
		require.Contains(t, result.Items[1].Reason, "list_libraries")
		require.Equal(t, "accepted", result.Items[2].Status)
		require.Equal(t, "exists", result.Items[3].Status)
		require.Equal(t, result.Items[2].EntryID, result.Items[3].EntryID)
		require.NotNil(t, result.Items[2].Graph)
		require.False(t, *result.Items[2].Graph)
		require.Equal(t, "doi:10.1234/abc", result.Items[6].Identifier)
		require.Equal(t, ids["ai-reports"], result.Items[7].LibraryID)
	}
	var report map[string]any
	mcpCall(t, client, "write_report", map[string]any{"library": ids["ai-reports"], "title": "Explicit AI report", "markdown": "# Generated\n\nReport body."}, &report)
	require.Equal(t, ids["ai-reports"], report["library_id"])
	rejected, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "write_report", Arguments: map[string]any{"library": "mine", "title": "Wrong library", "markdown": "Full report"}})
	require.NoError(t, err)
	require.True(t, rejected.IsError)
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
