package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestArticleLibraryNamesHTTPAndMCP(t *testing.T) {
	f := newMCPFixture(t)
	client := f.connect(t, "2025-11-25")
	entity, topic := mcpFollowTargets(t, f)
	mcpCall(t, client, "follow", map[string]any{"target_type": "topic", "target_id": topic.ID}, nil)
	RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(service.NewFollowService(f.db)))
	systemLibrary := f.library
	customLibrary := &types.Library{ID: uuid.NewString(), SpaceID: types.SingleSpaceID, Name: "研究 / Custom library", Directory: "custom-library", SourceEvidenceRule: systemLibrary.SourceEvidenceRule, Template: systemLibrary.Template, Type: types.LibraryTypeDocument}
	require.NoError(t, f.db.Create(customLibrary).Error)

	request := func(method, path, body, language string) any {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+f.key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", language)
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var envelope struct {
			Data any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
		t.Logf("HTTP %s %s (%s): %s", method, path, language, rec.Body.String())
		return envelope.Data
	}
	expected := map[string]string{}
	var catalog []types.Library
	require.NoError(t, f.db.Find(&catalog).Error)
	for _, library := range catalog {
		expected[library.ID] = library.Name
	}
	var assertNames func(any) int
	assertNames = func(value any) int {
		t.Helper()
		count := 0
		switch v := value.(type) {
		case map[string]any:
			if id, ok := v["library_id"].(string); ok && id != "" {
				require.Contains(t, expected, id)
				require.Equal(t, expected[id], v["library_name"], "%+v", v)
				count++
			}
			for _, child := range v {
				count += assertNames(child)
			}
		case []any:
			for _, child := range v {
				count += assertNames(child)
			}
		}
		return count
	}
	var articles []*types.Entry
	for _, library := range []*types.Library{systemLibrary, customLibrary} {
		f.library = library
		article := mcpFollowArticle(t, f, "Librarynamefixture", time.Now().Add(-time.Hour), entity.ID, topic.ID)
		articles = append(articles, article)
		f.seedPending(t, "Librarynamefixture pending")
		require.NoError(t, f.db.Model(article).Update("incomplete", true).Error)
	}
	f.library = systemLibrary
	for _, language := range []string{"zh-CN", "en-US"} {
		t.Run(language, func(t *testing.T) {
			for _, article := range articles {
				require.Greater(t, assertNames(request(http.MethodGet, "/api/v1/entries/"+article.ID, "", language)), 0)
				require.Greater(t, assertNames(request(http.MethodGet, "/api/v1/libraries/"+article.LibraryID+"/entries?collapse_similar=false", "", language)), 0)
			}
			for _, path := range []string{
				"/api/v1/entries?collapse_similar=false",
				"/api/v1/entries/batch?ids=" + articles[0].ID + "&ids=" + articles[1].ID,
				"/api/v1/entries/pending",
				"/api/v1/entries/search?keyword=Librarynamefixture",
				"/api/v1/ingest/incomplete",
				"/api/v1/entities/" + entity.ID,
			} {
				require.Greater(t, assertNames(request(http.MethodGet, path, "", language)), 0, path)
			}
			require.Greater(t, assertNames(request(http.MethodPost, "/api/v1/follows/feed", `{"include_seen":true,"collapse_similar":false}`, language)), 0)
		})
	}
	// The catalog localizes a default system label separately from stored article metadata.
	require.Equal(t, "Institutional", systemLibrary.DisplayName("en-US"))
	require.Equal(t, customLibrary.Name, customLibrary.DisplayName("en-US"))
	for _, article := range articles {
		var read service.EntryReadResult
		mcpCall(t, client, "get_article", map[string]any{"id": article.ID}, &read)
		require.Equal(t, article.LibraryID, read.LibraryID)
		require.Equal(t, expected[article.LibraryID], read.LibraryName)
	}
	for _, tool := range []string{"list_pending", "list_incomplete", "follow_feed"} {
		var wire any
		args := map[string]any{}
		if tool == "follow_feed" {
			args["collapse_similar"] = false
		}
		mcpCall(t, client, tool, args, &wire)
		require.Greater(t, assertNames(wire), 0, tool)
	}
	var detail any
	mcpCall(t, client, "get_entity", map[string]any{"id": entity.ID}, &detail)
	require.Greater(t, assertNames(detail), 0)
	// Mutation responses reuse the same article metadata contract.
	require.Greater(t, assertNames(request(http.MethodPut, "/api/v1/entries/"+articles[0].ID+"/star", `{"starred":true}`, "en-US")), 0)
	require.Greater(t, assertNames(request(http.MethodPut, "/api/v1/entries/"+articles[0].ID+"/content-type", `{"content_type":"新闻"}`, "en-US")), 0)
	require.Greater(t, assertNames(request(http.MethodPost, "/api/v1/ingest/"+articles[0].ID+"/accept-incomplete", `{}`, "en-US")), 0)
	var starred any
	mcpCall(t, client, "set_star", map[string]any{"id": articles[1].ID, "starred": true}, &starred)
	require.Greater(t, assertNames(starred), 0)
	var ingested types.IngestBatchResult
	mcpCall(t, client, "ingest", map[string]any{"items": []any{map[string]any{
		"library": systemLibrary.ID, "url": "https://example.org/library-name-ingest", "content": "# Library name ingest\n\nOriginal source evidence.", "acquisition": "extracted", "publisher": "Fixture Journal",
	}}}, &ingested)
	require.Len(t, ingested.Items, 1)
	require.Equal(t, "accepted", ingested.Items[0].Status)
	require.Equal(t, systemLibrary.Name, ingested.Items[0].LibraryName)
	var reports types.Library
	require.NoError(t, f.db.Where("space_id=? AND system_role=?", types.SingleSpaceID, "ai-reports").First(&reports).Error)
	expected[reports.ID] = reports.Name
	var report any
	mcpCall(t, client, "write_report", map[string]any{"title": "Library name report", "markdown": "# Report\n\nDerived evidence."}, &report)
	require.Greater(t, assertNames(report), 0)
	// Renames must be reflected immediately, including custom names on a system library.
	expected[systemLibrary.ID] = "Renamed system library"
	require.NoError(t, f.db.Model(systemLibrary).Update("name", expected[systemLibrary.ID]).Error)
	require.Greater(t, assertNames(request(http.MethodGet, "/api/v1/entries/"+articles[0].ID, "", "en-US")), 0)
	var read service.EntryReadResult
	mcpCall(t, client, "get_article", map[string]any{"id": articles[0].ID}, &read)
	require.Equal(t, expected[systemLibrary.ID], read.LibraryName)
}
