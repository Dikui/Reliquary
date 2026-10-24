package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func assertRetainedEntryFields(t *testing.T, row map[string]any) {
	t.Helper()
	for _, field := range []string{
		"content_type",
		"content_type_corrected",
		"content_type_hint",
		"topics",
		"similar_reports",
		"library_content_types",
	} {
		require.NotContains(t, row, field)
	}
}

func TestRetainedSearchFiltersHTTPAndMCP(t *testing.T) {
	f := newMCPFixture(t)
	require.NoError(
		t,
		f.db.Model(f.library).Update("indexing_strategy", types.IndexingStrategy{KeywordEnabled: true}).Error,
	)
	first := f.seedArticle(t, "retainedneedle first", "full", "# First\n\nSaved first body.")
	second := f.seedArticle(t, "retainedneedle second", "full", "# Second\n\nSaved second body.")
	other := &types.Library{
		ID:        "another",
		SpaceID:   types.SingleSpaceID,
		Name:      "Another",
		Directory: "another",
		Template:  types.TemplateOwn,
	}
	require.NoError(t, f.db.Create(other).Error)
	original := f.library
	f.library = other
	f.seedArticle(t, "retainedneedle outside", "full", "# Outside\n\nSaved outside body.")
	f.library = original
	require.NoError(t, f.db.Model(first).Update("starred", true).Error)
	search := service.NewSearchService(f.params.LibraryService, f.params.SpaceService, mcpFixtureModels{}, f.db)
	r := ingestAuthHTTPRouter(f)
	r.POST("/api/v1/search", handler.NewSearchHandler(search, f.files).SearchEntry)
	client := f.connect(t, "2025-11-25")
	for _, starred := range []*bool{nil, new(bool), func() *bool { v := true; return &v }()} {
		request := map[string]any{
			"libraries":      []string{f.library.ID},
			"query":          "retainedneedle",
			"published_from": "2026-10-01",
			"published_to":   "2026-10-01",
		}
		want := []string{first.ID, second.ID}
		if starred != nil {
			request["starred"] = *starred
			if *starred {
				want = []string{first.ID}
			} else {
				want = []string{second.ID}
			}
		}
		body, err := json.Marshal(request)
		require.NoError(t, err)
		response := personalRequest(t, r, http.MethodPost, "/api/v1/search", string(body), f.key, true)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var httpResult struct {
			Data struct{ Results []map[string]any }
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &httpResult))
		var ids []string
		for _, row := range httpResult.Data.Results {
			assertRetainedEntryFields(t, row)
			ids = append(ids, row["entry_id"].(string))
		}
		require.ElementsMatch(t, want, ids)
		var mcpResult struct {
			Results []map[string]any `json:"results"`
		}
		mcpCall(t, client, "search", request, &mcpResult)
		ids = nil
		for _, row := range mcpResult.Results {
			assertRetainedEntryFields(t, row)
			ids = append(ids, row["entry_id"].(string))
		}
		require.ElementsMatch(t, want, ids)
	}
	for _, field := range []string{"content_types", "topic_ids", "collapse_similar"} {
		value := any([]string{"retired"})
		if field == "collapse_similar" {
			value = true
		}
		body, err := json.Marshal(
			map[string]any{"libraries": []string{f.library.ID}, "query": "retainedneedle", field: value},
		)
		require.NoError(t, err)
		response := personalRequest(t, r, http.MethodPost, "/api/v1/search", string(body), f.key, true)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		result, err := client.CallTool(
			context.Background(),
			&mcp.CallToolParams{
				Name:      "search",
				Arguments: map[string]any{"libraries": []string{f.library.ID}, "query": "retainedneedle", field: value},
			},
		)
		require.NoError(t, err)
		require.True(t, result.IsError)
	}
	response := personalRequest(
		t,
		r,
		http.MethodGet,
		fmt.Sprintf(
			"/api/v1/entries?libraries=%s&published_from=2026-10-01&published_to=2026-10-01&starred=true",
			f.library.ID,
		),
		"",
		f.key,
		true,
	)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var page struct {
		Data  []map[string]any
		Total int
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Equal(t, 1, page.Total)
	require.Len(t, page.Data, 1)
	require.Equal(t, first.ID, page.Data[0]["id"])
	assertRetainedEntryFields(t, page.Data[0])
}

func TestProcessingRetainsBodyWithoutRetiredMetadata(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	r := ingestAuthHTTPRouter(f)
	response := personalRequest(
		t,
		r,
		http.MethodPost,
		"/api/v1/ingest",
		`{"items":[{"library":"personal",
"title":"Saved processing",
"acquisition":"verbatim",
"content":"# Saved body\n\nExact original measurements remain available."}]}`,
		f.key,
		true,
	)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result struct{ Data types.IngestBatchResult }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, "accepted", result.Data.Items[0].Status)
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	id := result.Data.Items[0].EntryID
	response = personalRequest(t, r, http.MethodGet, "/api/v1/entries/"+id, "", f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var entry struct{ Data map[string]any }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &entry))
	assertRetainedEntryFields(t, entry.Data)
	require.NotNil(t, entry.Data["structured_summary"])
	client := f.connect(t, "2025-11-25")
	var read struct {
		Body   string
		Header map[string]any
	}
	mcpCall(t, client, "get_entry", map[string]any{"id": id}, &read)
	require.Equal(t, "# Saved body\n\nExact original measurements remain available.\n", read.Body)
	assertRetainedEntryFields(t, read.Header)
}
