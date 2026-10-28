package router

import (
	"encoding/json"
	"net/http"
	"reliquary/internal/application/service"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestLibraryCatalogHTTPAndMCP(t *testing.T) {
	f := newMCPFixture(t)
	response := personalRequest(t, f.router, http.MethodGet, "/api/v1/libraries", "", f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Len(t, body.Data, 5)
	rules := map[string]string{}
	for _, library := range body.Data {
		for _, field := range []string{"id", "directory", "name", "template", "description", "collect_content", "exclude_content", "review_criteria", "default_review_criteria", "processing_model_tiers", "model_tiers", "deduplication_rule", "content_types", "relation_types", "feed_enabled", "system_role", "template_rules"} {
			require.Contains(t, library, field)
		}
		rules[library["template"].(string)] = library["deduplication_rule"].(string)
		single := personalRequest(t, f.router, http.MethodGet, "/api/v1/libraries/"+library["id"].(string), "", f.key, true)
		require.Equal(t, http.StatusOK, single.Code, single.Body.String())
	}
	require.Len(t, rules, 4)
	texts := map[string]bool{}
	for _, rule := range rules {
		texts[rule] = true
	}
	require.Len(t, texts, 1, "all templates share the admission rule")
	client := f.connect(t, "2025-03-26")
	var result struct {
		Libraries []map[string]any `json:"libraries"`
	}
	mcpCall(t, client, "list_libraries", map[string]any{}, &result)
	require.Len(t, result.Libraries, 5)
	for i, library := range result.Libraries {
		for _, field := range []string{"id", "directory", "name", "template", "description", "collect_content", "exclude_content", "review_criteria", "deduplication_rule", "content_types", "feed_enabled"} {
			require.Equal(t, body.Data[i][field], library[field], field)
		}
	}
	created := personalRequest(t, f.router, http.MethodPost, "/api/v1/libraries", `{"template":"own","directory":"custom-docs","name":"Notes"}`, f.key, true)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var custom struct {
		Data types.Library `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &custom))
	require.Equal(t, custom.Data.DefaultReviewCriteria(), custom.Data.ReviewCriteria)
	model := &types.Model{ID: "catalog-review", SpaceID: 1, Name: "Catalog review", Type: types.ModelTypeGeneration, Status: types.ModelStatusActive}
	require.NoError(t, f.db.Create(model).Error)
	settings, err := json.Marshal(types.LibraryPatch{ModelTiers: &types.ModelTierConfig{Generation: model.ID, Embedding: custom.Data.ModelTiers.Embedding}, ChunkingConfig: &types.ChunkingConfig{ChunkSize: 256, ChunkOverlap: 20}})
	require.NoError(t, err)
	configured := personalRequest(t, f.router, http.MethodPatch, "/api/v1/libraries/"+custom.Data.ID, string(settings), f.key, true)
	require.Equal(t, http.StatusOK, configured.Code, configured.Body.String())
	var configuredBody struct {
		Data types.Library `json:"data"`
	}
	require.NoError(t, json.Unmarshal(configured.Body.Bytes(), &configuredBody))
	require.Equal(t, model.ID, configuredBody.Data.ModelTiers.Generation)
	require.Equal(t, 256, configuredBody.Data.ChunkingConfig.ChunkSize)
	var other types.Library
	require.NoError(t, f.db.First(&other, "id=?", f.library.ID).Error)
	require.NotEqual(t, model.ID, other.ModelTiers.Generation)
	settings, err = json.Marshal(types.LibraryPatch{ModelTiers: &types.ModelTierConfig{Generation: "missing-model", Embedding: custom.Data.ModelTiers.Embedding}})
	require.NoError(t, err)
	invalid := personalRequest(t, f.router, http.MethodPatch, "/api/v1/libraries/"+custom.Data.ID, string(settings), f.key, true)
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	require.Contains(t, invalid.Body.String(), "generation model is unavailable")
	for _, patch := range []string{`{"template":"generated"}`, `{"directory":"new"}`, `{"system_role":"personal"}`, `{"review_criteria":"No review"}`, `{"source_evidence_rule":"link_required"}`, `{"template_rules":{"review":false}}`} {
		response := personalRequest(t, f.router, http.MethodPatch, "/api/v1/libraries/"+custom.Data.ID, patch, f.key, true)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
	changed := personalRequest(t, f.router, http.MethodPatch, "/api/v1/libraries/"+custom.Data.ID, `{"name":"Renamed","description":"Collect notes","feed_enabled":true,"content_types":[{"name":"Note","enabled":true,"extract_graph":false}],"relation_types":[{"name":"uses","enabled":true}]}`, f.key, true)
	require.Equal(t, http.StatusOK, changed.Code, changed.Body.String())
	require.NoError(t, json.Unmarshal(changed.Body.Bytes(), &configuredBody))
	require.Equal(t, custom.Data.ReviewCriteria, configuredBody.Data.ReviewCriteria, "historical criteria remain readable")
	require.Equal(t, custom.Data.SourceEvidenceRule, configuredBody.Data.SourceEvidenceRule)
	require.Equal(t, http.StatusOK, personalRequest(t, f.router, http.MethodDelete, "/api/v1/libraries/"+f.library.ID, "", f.key, true).Code)
	require.Equal(t, http.StatusOK, personalRequest(t, f.router, http.MethodDelete, "/api/v1/libraries/"+custom.Data.ID, "", f.key, true).Code)
}

func TestExplicitIngestSelectsSystemLibrary(t *testing.T) {
	f := newMCPFixture(t)
	for _, kind := range []string{"official", "personal"} {
		request := &types.IngestRequest{Library: kind, Title: kind, Content: "# Original\n\nFull source content for the transition.", Acquisition: "verbatim", URL: "https://example.org/" + kind}
		if kind == "personal" {
			request.URL = ""
		}
		result, err := f.articles.Ingest(f.ctx, request)
		require.NoError(t, err)
		var entry types.Entry
		require.NoError(t, f.db.First(&entry, "id=?", result.EntryID).Error)
		var library types.Library
		require.NoError(t, f.db.First(&library, "id=?", entry.LibraryID).Error)
		require.Equal(t, kind, library.SystemRole)
	}
	report, err := f.documents.CreateReport(f.ctx, "Generated report", "# Report\n\nGenerated findings.", nil)
	require.NoError(t, err)
	var library types.Library
	require.NoError(t, f.db.First(&library, "id=?", report.LibraryID).Error)
	require.Equal(t, "ai-output", library.SystemRole)
}

func TestMultipleLibraryReadSearchAndFeed(t *testing.T) {
	f := newMCPFixture(t)
	require.NoError(t, f.db.Model(&types.Library{}).Where("space_id=?", types.SingleSpaceID).Update("indexing_strategy", types.IndexingStrategy{KeywordEnabled: true}).Error)
	var libraries []types.Library
	require.NoError(t, f.db.Find(&libraries).Error)
	follows := service.NewFollowService(f.db)
	for _, library := range libraries {
		entity := types.Entity{ID: "entity-" + library.SystemRole, SpaceID: 1, LibraryID: library.ID, CanonicalName: "Multi entity", NormalizedName: "multi entity", EntityType: "company"}
		require.NoError(t, f.db.Create(&entity).Error)
		_, err := follows.Follow(f.ctx, "entity", entity.ID)
		require.NoError(t, err)
		entry := types.Entry{ID: library.SystemRole + "-entry", SpaceID: 1, LibraryID: library.ID, Title: "catalogneedle shared", Type: "manual", SourceKind: map[string]string{"personal": "mine", "ai-output": "report", "official": "article", "media": "article", "references": "article"}[library.SystemRole], ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", VaultBody: "Full original body"}
		require.NoError(t, f.db.Create(&entry).Error)
		require.NoError(t, f.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: entry.ID, EntityID: entity.ID, CreatedAt: time.Now().Add(-time.Second)}).Error)
		detail := personalRequest(t, f.router, http.MethodGet, "/api/v1/entries/"+entry.ID, "", f.key, true)
		require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
		require.Contains(t, detail.Body.String(), library.ID)
		scoped := personalRequest(t, f.router, http.MethodGet, "/api/v1/libraries/"+library.ID+"/entries", "", f.key, true)
		require.Equal(t, http.StatusOK, scoped.Code, scoped.Body.String())
		var page struct {
			Total int `json:"total"`
		}
		require.NoError(t, json.Unmarshal(scoped.Body.Bytes(), &page))
		require.Equal(t, 1, page.Total)
	}
	all := personalRequest(t, f.router, http.MethodGet, "/api/v1/entries", "", f.key, true)
	require.Equal(t, http.StatusOK, all.Code, all.Body.String())
	var page struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(all.Body.Bytes(), &page))
	require.Equal(t, 5, page.Total)
	noCollapse := false
	search, err := service.NewSearchService(f.params.LibraryService, f.params.SpaceService, nil, f.db).Search(f.ctx, types.EntrySearchRequest{Libraries: []string{"personal", "ai-output", "official", "media", "references"}, Query: "catalogneedle", Limit: 10, CollapseSimilar: &noCollapse})
	require.NoError(t, err)
	require.Len(t, search.Results, 5)
	feed, err := follows.Feed(f.ctx, types.FollowFeedRequest{})
	require.NoError(t, err)
	require.Len(t, feed.Results, 2)
	require.NoError(t, f.db.Model(&types.Library{}).Where("system_role='personal'").Update("feed_enabled", true).Error)
	feed, err = follows.Feed(f.ctx, types.FollowFeedRequest{})
	require.NoError(t, err)
	require.Len(t, feed.Results, 3)
}
