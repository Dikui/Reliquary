package router

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestRemovedIngestRoutesReturn404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterEntryRoutes(r.Group("/api/v1"), &handler.EntryHandler{}, &accessGuards{})
	RegisterSystemRoutes(r.Group("/api/v1"), &handler.SystemHandler{}, &accessGuards{})
	for _, item := range []struct{ method, path string }{
		{"POST", "/api/v1/entries/id/processing-review"},
		{"GET", "/api/v1/entries/id/download"},
		{"GET", "/api/v1/entries/id/preview"},
		{"GET", "/api/v1/system/parser-engines"},
		{"POST", "/api/v1/system/parser-engines/check"},
	} {
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest(item.method, item.path, nil))
		require.Equal(t, http.StatusNotFound, response.Code, item.path)
	}
}

func TestMarkdownOnlyHTTPAndMCP(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	client := f.connect(t, "2025-11-25")
	r := gin.New()
	r.Use(middleware.ErrorHandler(), middleware.Auth(f.params.SpaceService, f.params.UserService, f.params.APIKeyService))
	entries := f.articles.(interfaces.EntryService)
	RegisterEntryRoutes(r.Group("/api/v1"), handler.NewEntryHandler(nil, entries, f.params.LibraryService, f.queue, nil), newAccessGuards(f.params.LibraryService, entries, nil))
	send := func(protocol string, item map[string]any) types.IngestResult {
		t.Helper()
		var result types.IngestBatchResult
		payload := map[string]any{"items": []any{item}}
		if protocol == "MCP" {
			mcpCall(t, client, "ingest", payload, &result)
		} else {
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			response := personalRequest(t, r, http.MethodPost, "/api/v1/ingest", string(body), f.key, false)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var envelope struct {
				Data types.IngestBatchResult `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
			result = envelope.Data
		}
		require.Len(t, result.Items, 1)
		return result.Items[0]
	}
	pending := f.seedPending(t, "Removed conversion action")
	response := personalRequest(t, r, http.MethodPost, "/api/v1/entries/"+pending.ID+"/review", `{"action":"convert_to_mine","confirmed":true}`, f.key, false)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	call, err := client.CallTool(f.ctx, &mcp.CallToolParams{Name: "approve_pending", Arguments: map[string]any{"id": pending.ID, "action": "convert_to_mine", "confirmed": true}})
	require.NoError(t, err)
	require.True(t, call.IsError)
	var unchanged types.Entry
	require.NoError(t, f.db.First(&unchanged, "id=?", pending.ID).Error)
	require.Equal(t, "article", unchanged.SourceKind)
	for _, protocol := range []string{"HTTP", "MCP"} {
		t.Run(protocol, func(t *testing.T) {
			for _, field := range []string{"file", "original_file", "original_data", "original_file_base64"} {
				result := send(protocol, map[string]any{"url": "https://example.org/rejected", "acquisition": "extracted", "content": "# Body", field: "cGRm"})
				require.Equal(t, "invalid", result.Status)
				require.Contains(t, result.Reason, field)
				require.Contains(t, result.Reason, "Markdown")
			}
			result := send(protocol, map[string]any{"url": "https://example.org/empty", "acquisition": "extracted"})
			require.Equal(t, "invalid", result.Status)
			require.Contains(t, result.Reason, "content is required")
			require.Contains(t, result.Reason, "Markdown")
			picture := image.NewRGBA(image.Rect(0, 0, 64, 64))
			picture.Set(5, 5, color.RGBA{200, 50, 0, 255})
			var data bytes.Buffer
			require.NoError(t, png.Encode(&data, picture))
			asset, err := f.articles.(interfaces.IngestService).UploadIngestAsset(f.ctx, "figure.png", data.Bytes())
			require.NoError(t, err)
			extra := "Marine observation records quantify salinity, wind and tide along the Pacific coast."
			if protocol == "MCP" {
				extra = "Software verification documents a compiler architecture, dependency graph and deterministic output format."
			}
			body := "# " + protocol + " supplied source\n\nComplete original Markdown for " + protocol + " with an uploaded picture. " + extra + "\n\n![Figure](https://example.org/" + protocol + ".png)"
			result = send(protocol, map[string]any{"url": "https://example.org/valid-" + protocol, "acquisition": "extracted", "publisher": "Fixture Journal", "content": body, "images": []any{map[string]any{"asset_id": asset.AssetID, "source_url": "https://example.org/" + protocol + ".png"}}})
			require.Equal(t, "accepted", result.Status, result.Reason)
			for _, task := range f.queue.tasks {
				if task.Type() == types.TypeManualProcess {
					require.NoError(t, entries.ProcessManualUpdate(f.ctx, task))
				}
			}
			f.queue.tasks = nil
			var article types.Entry
			require.NoError(t, f.db.Where("id=?", result.EntryID).First(&article).Error)
			require.Equal(t, types.ParseStatusCompleted, article.ParseStatus, article.ErrorMessage)
			saved, err := f.vault.ReadBody(&article)
			require.NoError(t, err)
			require.Contains(t, string(saved), "Complete original Markdown for "+protocol)
			require.Contains(t, string(saved), "![Figure]")
			require.NotContains(t, string(saved), "https://example.org/"+protocol+".png")
		})
	}
}
