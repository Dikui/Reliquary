package router

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"reliquary/internal/mcpserver"
	"reliquary/internal/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCPContentChecksMatchHTTP(t *testing.T) {
	f := newMCPFixture(t)
	text := "# 规格\n\n支持十六个连接 🙂。\n"
	entry := f.seedArticle(t, "自检证据", "full", text)
	quote := "支持十六个连接 🙂。"
	chunk := &types.Chunk{Content: quote, StartAt: 6, EndAt: 6 + len([]rune(quote))}
	types.AssignSourceLocations(text, []*types.Chunk{chunk})
	require.Len(t, chunk.SourceLocations, 1)
	now := time.Now().UTC().Truncate(time.Microsecond)
	run := types.ContentCheckRun{
		ID: uuid.NewString(), SpaceID: types.SingleSpaceID, ModelID: "mcp-chat", Status: "completed",
		WindowFrom: now.Add(-time.Hour), WindowTo: now, CreatedAt: now, FinishedAt: &now,
		Targets: []string{entry.ID}, Completed: 1,
		Results: []types.ContentCheckResult{{EntryID: entry.ID, Findings: []types.ContentCheckFinding{{
			Kind: "result_error", Description: "摘要中的连接数量与原文不符。",
			Evidence: []types.ContentCheckEvidence{{
				EntryID: entry.ID, Title: entry.Title, LibraryID: entry.LibraryID,
				Quote: quote, CurrentResult: "支持八个连接。", Location: chunk.SourceLocations[0],
			}},
		}}}},
	}
	require.NoError(t, f.db.Create(&run).Error)
	for i := 1; i <= 20; i++ {
		other := types.ContentCheckRun{ID: uuid.NewString(), SpaceID: types.SingleSpaceID,
			Status: "completed", CreatedAt: now.Add(time.Duration(i) * time.Second),
			Targets: []string{}, Results: []types.ContentCheckResult{}}
		require.NoError(t, f.db.Create(&other).Error)
	}
	var before types.ContentCheckRun
	require.NoError(t, f.db.First(&before, "id=?", run.ID).Error)
	client := f.connect(t, "2025-11-25")
	for _, test := range []struct {
		name  string
		args  map[string]any
		query string
		size  int
	}{
		{"default", map[string]any{}, "", 20},
		{"first_page", map[string]any{"page": 1}, "?page=1", 20},
		{"second_page", map[string]any{"page": 2}, "?page=2", 1},
		{"empty_page", map[string]any{"page": 3}, "?page=3", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := personalRequest(t, f.router, http.MethodGet, "/api/v1/diagnostics/content-checks"+test.query, "", f.key, true)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var body struct {
				Data json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			var result json.RawMessage
			mcpCall(t, client, "list_content_checks", test.args, &result)
			require.JSONEq(t, string(body.Data), string(result))
			var page types.ContentCheckPage
			require.NoError(t, json.Unmarshal(result, &page))
			require.EqualValues(t, 21, page.Total)
			require.Len(t, page.Items, test.size)
			for _, item := range page.Items {
				require.Empty(t, item.Results, "evidence is fetched through the detail tool")
			}
			if test.name == "second_page" {
				require.Equal(t, run.ID, page.Items[0].ID)
				require.Equal(t, 1, page.Items[0].Total)
			}
		})
	}
	response := personalRequest(t, f.router, http.MethodGet, "/api/v1/diagnostics/content-checks/"+run.ID, "", f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	var detail json.RawMessage
	mcpCall(t, client, "get_content_check", map[string]any{"id": run.ID}, &detail)
	require.JSONEq(t, string(body.Data), string(detail))
	var got types.ContentCheckRun
	require.NoError(t, json.Unmarshal(detail, &got))
	require.Equal(t, 1, got.Total)
	require.Equal(t, run.Results, got.Results)
	for _, page := range []int{0, -1, 1000001} {
		response := personalRequest(t, f.router, http.MethodGet, "/api/v1/diagnostics/content-checks?page="+strconv.Itoa(page), "", f.key, true)
		require.Equal(t, http.StatusBadRequest, response.Code)
		result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_content_checks", Arguments: map[string]any{"page": page}})
		require.NoError(t, err)
		require.True(t, result.IsError, "invalid page %d", page)
	}
	missing := uuid.NewString()
	response = personalRequest(t, f.router, http.MethodGet, "/api/v1/diagnostics/content-checks/"+missing, "", f.key, true)
	require.Equal(t, http.StatusNotFound, response.Code)
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_content_check", Arguments: map[string]any{"id": missing}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Contains(t, result.Content[0].(*mcp.TextContent).Text, "check run not found")
	result, err = client.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_content_check", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	var after types.ContentCheckRun
	require.NoError(t, f.db.First(&after, "id=?", run.ID).Error)
	require.Equal(t, before, after)
	var count int64
	require.NoError(t, f.db.Model(&types.ContentCheckRun{}).Count(&count).Error)
	require.EqualValues(t, 21, count)
	require.Empty(t, f.queue.tasks)
}

func TestMCPContentChecksNotRegisteredWithoutService(t *testing.T) {
	f := newMCPFixture(t)
	adapter := mcpserver.New(mcpserver.Services{Entries: f.articles})
	f.router.Any("/mcp-without-content-checks", gin.WrapH(adapter))
	client := mcp.NewClient(&mcp.Implementation{Name: "reliquary-test-agent", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: f.server.URL + "/mcp-without-content-checks", MaxRetries: -1, DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: mcpBearerTransport{key: f.key, base: http.DefaultTransport}, Timeout: 10 * time.Second}}
	session, err := client.Connect(context.Background(), transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	tools, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		require.NotContains(t, tool.Name, "content_check")
	}
	for _, name := range []string{"list_content_checks", "get_content_check"} {
		_, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		require.Error(t, err)
	}
}
