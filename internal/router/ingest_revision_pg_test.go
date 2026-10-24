package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reliquary/internal/types"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func ingestRevisionHTTP(t *testing.T, f *mcpFixture, items ...map[string]any) []map[string]any {
	t.Helper()
	data, err := json.Marshal(map[string]any{"items": items})
	require.NoError(t, err)
	response := personalRequest(t, ingestAuthHTTPRouter(f), http.MethodPost,
		"/api/v1/ingest", string(data), f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result struct {
		Data struct{ Items []map[string]any }
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Len(t, result.Data.Items, len(items))
	return result.Data.Items
}

func TestIngestSourceRevisionHTTPAndMCP(t *testing.T) {
	f := newMCPFixture(t)
	router := ingestAuthHTTPRouter(f)
	client := f.connect(t, "2025-11-25")
	for _, transport := range []string{"http", "mcp"} {
		t.Run(transport, func(t *testing.T) {
			send := func(library, sourceID, body string, revision int64) map[string]any {
				t.Helper()
				input := map[string]any{"items": []any{map[string]any{
					"library": library, "source_id": sourceID, "source_revision": revision, "content": body,
				}}}
				var result struct{ Items []map[string]any }
				if transport == "mcp" {
					mcpCall(t, client, "ingest", input, &result)
				} else {
					data, err := json.Marshal(input)
					require.NoError(t, err)
					response := personalRequest(t, router, http.MethodPost, "/api/v1/ingest", string(data), f.key, true)
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
					var envelope struct{ Data json.RawMessage }
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
					require.NoError(t, json.Unmarshal(envelope.Data, &result))
				}
				require.Len(t, result.Items, 1)
				return result.Items[0]
			}
			sourceID := "eyeandear/" + transport
			first := send("personal", " "+sourceID+" ", "Latest measurements.", 5)
			require.Equal(t, "accepted", first["status"], first)
			id, ok := first["entry_id"].(string)
			require.True(t, ok)
			require.NotEmpty(t, id)
			require.Equal(t, sourceID, first["source_id"])
			require.Equal(t, float64(5), first["source_revision"])
			for _, revision := range []int64{4, 5} {
				stale := send("personal", sourceID, "Stale measurements must never be saved.", revision)
				require.Equal(t, "stale_revision", stale["status"], stale)
				require.Equal(t, "source_revision_not_newer", stale["reason"])
				require.Equal(t, id, stale["entry_id"])
				require.Equal(t, float64(5), stale["source_revision"])
			}
			body := personalRequest(t, router, http.MethodGet, "/api/v1/entries/"+id+"/body", "", f.key, true)
			require.Equal(t, http.StatusOK, body.Code)
			require.Equal(t, "Latest measurements.\n", body.Body.String())
			newer := send("personal", sourceID, "Corrected measurements.", 6)
			require.Equal(t, "accepted", newer["status"], newer)
			require.Equal(t, id, newer["entry_id"])
			var read map[string]any
			mcpCall(t, client, "get_entry", map[string]any{"id": id}, &read)
			require.Equal(t, "Corrected measurements.\n", read["body"])
			require.Equal(t, sourceID, read["source_id"])
			require.Equal(t, float64(6), read["source_revision"])
			other := send("references", sourceID, "An independent library revision.", 0)
			require.Equal(t, "accepted", other["status"], other)
			require.NotEqual(t, id, other["entry_id"])
		})
	}
}

func TestIngestSourceRevisionAdvancesWithoutBodyChangeAndPreservesLegacyIdentity(t *testing.T) {
	f := newMCPFixture(t)
	item := map[string]any{
		"library": "personal", "source_ref": "notes/original", "content": "Original measurements.",
	}
	legacy := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "accepted", legacy["status"])
	id := legacy["entry_id"]
	item["source_id"], item["source_revision"] = "stable-source", 0
	bound := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "exists", bound["status"], bound)
	require.Equal(t, id, bound["entry_id"])
	require.Equal(t, float64(0), bound["source_revision"])
	item["source_revision"] = 9
	advanced := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "exists", advanced["status"], advanced)
	require.Equal(t, float64(9), advanced["source_revision"])
	item["source_revision"], item["content"] = 8, "Stale changed content."
	require.Equal(t, "stale_revision", ingestRevisionHTTP(t, f, item)[0]["status"])
	delete(item, "source_id")
	delete(item, "source_revision")
	item["content"] = "Legacy source_ref update."
	updated := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "accepted", updated["status"], updated)
	require.Equal(t, id, updated["entry_id"])
	require.Equal(t, float64(9), updated["source_revision"])
	require.Equal(t, "exists", ingestRevisionHTTP(t, f, item)[0]["status"])
	item["source_id"], item["source_revision"] = "stable-source", 9
	require.Equal(t, "stale_revision", ingestRevisionHTTP(t, f, item)[0]["status"])
	item["source_id"] = "different-source"
	conflict := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "invalid", conflict["status"])
	require.Contains(t, conflict["reason"], "source_id")
}

func TestIngestSourceRevisionValidationIsPerItem(t *testing.T) {
	f := newMCPFixture(t)
	for _, fields := range []map[string]any{
		{"source_id": "source"},
		{"source_revision": 1},
		{"source_id": "source", "source_revision": -1},
		{"source_id": "source", "source_revision": 1.5},
		{"source_id": "source", "source_revision": "1"},
		{"source_id": "source", "source_revision": nil},
		{"source_id": nil},
		{"source_revision": nil},
		{"source_id": " \t ", "source_revision": 1},
		{"source_id": "invalid\x00source", "source_revision": 1},
		{"source_id": strings.Repeat("界", 2049), "source_revision": 1},
		{"source_id": "source", "source_revision": json.Number("9223372036854775808")},
	} {
		fields["library"], fields["content"] = "personal", "Source validation."
		results := ingestRevisionHTTP(t, f, fields,
			map[string]any{"library": "personal", "content": "Valid independent item."})
		require.Equal(t, "invalid", results[0]["status"], fields)
		require.NotEmpty(t, results[0]["reason"])
		require.Equal(t, "accepted", results[1]["status"])
	}
}

func TestIngestSourceRevisionConcurrentSubmissionsRetainNewest(t *testing.T) {
	f := newMCPFixture(t)
	router := ingestAuthHTTPRouter(f)
	responses := make(chan *httptest.ResponseRecorder, 8)
	for revision := 1; revision <= cap(responses); revision++ {
		data, err := json.Marshal(map[string]any{"items": []any{map[string]any{
			"library": "personal", "source_id": "concurrent-source", "source_revision": revision,
			"content": fmt.Sprintf("Revision %d.", revision),
		}}})
		require.NoError(t, err)
		go func() {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader(string(data)))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+f.key)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			responses <- response
		}()
	}
	id := ""
	for range cap(responses) {
		response := <-responses
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var result struct{ Data types.IngestBatchResult }
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.Len(t, result.Data.Items, 1)
		item := result.Data.Items[0]
		require.Contains(t, []string{"accepted", "stale_revision"}, item.Status)
		if id == "" {
			id = item.EntryID
		}
		require.Equal(t, id, item.EntryID)
	}
	body := personalRequest(t, router, http.MethodGet, "/api/v1/entries/"+id+"/body", "", f.key, true)
	require.Equal(t, http.StatusOK, body.Code)
	require.Equal(t, "Revision 8.\n", body.Body.String())
}

func TestIngestSourceRevisionInt64SurvivesMCPRead(t *testing.T) {
	f := newMCPFixture(t)
	client := f.connect(t, "2025-11-25")
	const revision int64 = 9223372036854775807
	sourceID := strings.Repeat("界", 2048)
	var result types.IngestBatchResult
	response, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ingest", Arguments: map[string]any{"items": []any{map[string]any{
			"library": "personal", "source_id": sourceID,
			"source_revision": revision, "content": "An exact integer revision.",
		}}},
	})
	require.NoError(t, err)
	require.False(t, response.IsError)
	// Decode protocol text directly: the SDK's StructuredContent uses float64.
	require.NoError(t, json.Unmarshal([]byte(response.Content[0].(*mcp.TextContent).Text), &result))
	require.Len(t, result.Items, 1)
	require.Equal(t, "accepted", result.Items[0].Status)
	require.Equal(t, revision, *result.Items[0].SourceRevision)
	var read struct {
		SourceID       string `json:"source_id"`
		SourceRevision int64  `json:"source_revision"`
	}
	response, err = client.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_entry", Arguments: map[string]any{"id": result.Items[0].EntryID},
	})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.NoError(t, json.Unmarshal([]byte(response.Content[0].(*mcp.TextContent).Text), &read))
	require.Equal(t, sourceID, read.SourceID)
	require.Equal(t, revision, read.SourceRevision)
}

func TestIngestSourceRevisionRollsBackWhenEnqueueFails(t *testing.T) {
	f := newMCPFixture(t)
	item := map[string]any{
		"library": "personal", "source_id": "rollback-source", "source_revision": 2,
		"content": "Committed measurements.",
	}
	first := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "accepted", first["status"], first)
	f.queue.enqueueErr = errors.New("fixture queue unavailable")
	item["source_revision"], item["content"] = 3, "Failed replacement."
	require.Equal(t, "invalid", ingestRevisionHTTP(t, f, item)[0]["status"])
	f.queue.enqueueErr = nil
	item["content"] = "Successful replacement."
	retry := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "accepted", retry["status"], retry)
	require.Equal(t, first["entry_id"], retry["entry_id"])
}
