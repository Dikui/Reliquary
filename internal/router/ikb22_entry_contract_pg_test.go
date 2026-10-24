package router

import (
	"context"
	"encoding/json"
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// Inspect wire keys directly, including nested/folded records. User metadata
// and historical audit values remain opaque and are not contract keys.
func requireEntryWireKeys(t *testing.T, value any) {
	t.Helper()
	switch v := value.(type) {
	case []any:
		for _, child := range v {
			requireEntryWireKeys(t, child)
		}
	case map[string]any:
		for key, child := range v {
			switch key {
			case "metadata", "custom_metadata", "before_value", "after_value", "payload", "headers":
				continue
			}
			for _, retired := range []string{"incomplete", "missing_materials", "external_completion_attempted", "completeness", "article_id", "article_ids", "latest_article_id", "related_article_id", "related_article_ids", "knowledge_id", "knowledge_ids", "knowledge", "article_count", "recent_article_count", "failed_articles"} {
				require.NotEqual(t, retired, key, "retired wire key")
			}
			requireEntryWireKeys(t, child)
		}
	}
}

func TestIKB22HTTPAndMCPEntryContractsAndRejectedAliases(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	entries := f.articles.(interfaces.EntryService)
	entryHandler := handler.NewEntryHandler(nil, entries, f.params.LibraryService, f.queue, nil)
	f.router.POST("/contract/ingest", entryHandler.Ingest)
	f.router.GET("/contract/entries/:id", entryHandler.GetEntry)
	f.router.GET("/contract/entries/:id/body", entryHandler.ReadEntryBody)
	search := service.NewSearchService(evaluationHTTPBases{f.params.LibraryService}, f.params.SpaceService, nil, f.db)
	f.router.POST("/contract/search", handler.NewSearchHandler(search, nil).SearchEntry)
	entitiesService := service.NewEntityService(f.db, nil)
	f.router.GET("/contract/entities/:entity_id", handler.NewEntityHandler(entitiesService).EntityDetail)
	follows := service.NewFollowService(f.db)
	f.router.POST("/contract/feed", handler.NewFollowHandler(follows).Feed)
	handler.NewEvaluationHandler(service.NewEvaluationService(f.db, search, nil), service.NewSearchRecorder(f.db)).Register(f.router.Group("/api/v1"))
	client := f.connect(t, "2025-11-25")
	call := func(name string, input map[string]any) map[string]any {
		var out map[string]any
		mcpCall(t, client, name, input, &out)
		requireEntryWireKeys(t, out)
		return out
	}
	httpCall := func(method, path string, input any) map[string]any {
		raw := ""
		if input != nil {
			encoded, err := json.Marshal(input)
			require.NoError(t, err)
			raw = string(encoded)
		}
		response := personalRequest(t, f.router, method, path, raw, f.key, true)
		require.Contains(t, []int{200, 201}, response.Code, response.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &out))
		requireEntryWireKeys(t, out)
		return out
	}
	for _, transport := range []string{"http", "mcp"} {
		t.Run(transport, func(t *testing.T) {
			input := map[string]any{"items": []any{map[string]any{"library": "personal", "source_ref": "contract/" + transport, "title": "Contract " + transport, "content": "# Contract\n\nComplete evidence for " + transport, "acquisition": "verbatim"}}}
			var out map[string]any
			if transport == "http" {
				out = httpCall(http.MethodPost, "/contract/ingest", input)["data"].(map[string]any)
			} else {
				out = call("ingest", input)
			}
			item := out["items"].([]any)[0].(map[string]any)
			require.Equal(t, "accepted", item["status"])
			id := item["entry_id"].(string)
			require.NotEmpty(t, id)
			// Retired image-completion requests must be rejected before any mutation.
			supplement := map[string]any{"items": []any{map[string]any{"library": "personal", "entry_id": id, "images": []any{map[string]any{"source_url": "https://example.test/image.png", "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aX1sAAAAASUVORK5CYII="}}}}}
			if transport == "http" {
				out = httpCall("POST", "/contract/ingest", supplement)["data"].(map[string]any)
			} else {
				out = call("ingest", supplement)
			}
			item = out["items"].([]any)[0].(map[string]any)
			require.Equal(t, "invalid", item["status"])
			require.Contains(t, item["reason"], "entry_id")
			for _, old := range []string{"article_id", "knowledge_id"} {
				bad := map[string]any{"items": []any{map[string]any{"library": "personal", old: id, "images": []any{}}}}
				if transport == "http" {
					out = httpCall("POST", "/contract/ingest", bad)["data"].(map[string]any)
				} else {
					out = call("ingest", bad)
				}
				require.Equal(t, "invalid", out["items"].([]any)[0].(map[string]any)["status"])
			}
		})
	}
	source := f.seedArticle(t, "Entrycontract searchfixture", "", "# Evidence\n\nsearchfixture original text")
	entity := types.Entity{ID: uuid.NewString(), SpaceID: 1, CanonicalName: "Entrycontract", NormalizedName: "entrycontract", EntityType: "company"}
	require.NoError(t, f.db.Create(&entity).Error)
	require.NoError(t, f.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: source.ID, EntityID: entity.ID}).Error)
	_, err := follows.Follow(f.ctx, types.FollowTargetEntity, entity.ID)
	require.NoError(t, err)
	query := map[string]any{"query": "searchfixture", "libraries": []string{f.library.ID}}
	for _, out := range []map[string]any{httpCall("POST", "/contract/search", query)["data"].(map[string]any), call("search", query)} {
		require.Equal(t, source.ID, out["results"].([]any)[0].(map[string]any)["entry_id"])
	}
	for _, out := range []map[string]any{httpCall("GET", "/contract/entities/"+entity.ID, nil)["data"].(map[string]any), call("get_entity", map[string]any{"id": entity.ID})} {
		require.NotEmpty(t, out["entries"])
	}
	feedInput := map[string]any{}
	httpFeed := httpCall("POST", "/contract/feed", feedInput)["data"].(map[string]any)
	mcpFeed := call("follow_feed", feedInput)
	require.NotEmpty(t, httpFeed["results"])
	require.NotEmpty(t, mcpFeed["results"])
	require.Contains(t, httpCall("GET", "/contract/entries/"+source.ID, nil)["data"], "id")
	require.NotEmpty(t, call("get_entry", map[string]any{"id": source.ID})["header"])
	_, err = client.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_article", Arguments: map[string]any{"id": source.ID}})
	require.Error(t, err)
	records, err := service.NewSearchRecorder(f.db).List(f.ctx, service.SearchRecordFilter{})
	require.NoError(t, err)
	require.NotEmpty(t, records)
	set := httpCall("POST", "/api/v1/evaluation/sets", map[string]any{"name": "Entry contract"})["data"].(map[string]any)
	path := "/api/v1/evaluation/sets/" + set["id"].(string) + "/from-record"
	converted := httpCall("POST", path, map[string]any{"record_id": records[0].ID, "answers": []any{map[string]any{"entry_id": source.ID, "grade": 1}}})["data"].(map[string]any)
	require.Equal(t, source.ID, converted["answers"].([]any)[0].(map[string]any)["entry_id"])
	for _, old := range []string{"article_id", "knowledge_id"} {
		raw, _ := json.Marshal(map[string]any{"record_id": records[0].ID, "answers": []any{map[string]any{old: source.ID, "grade": 1}}})
		response := personalRequest(t, f.router, "POST", path, string(raw), f.key, true)
		require.Equal(t, 400, response.Code, response.Body.String())
	}
}
