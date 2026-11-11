package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// This substitutes an unavailable remote embedding endpoint, so keyword-only
// contracts do not depend on the fixture's identical vectors for every text.
type shelfSearchUnavailableVectors struct{ interfaces.LibraryService }

func (shelfSearchUnavailableVectors) GetQueryEmbedding(context.Context, string, string) ([]float32, error) {
	return nil, fmt.Errorf("synthetic embedding endpoint unavailable")
}

func newShelfSearchFixture(t *testing.T) (*managedNamesFixture, types.Library, types.ShelfChangeset) {
	t.Helper()
	f, library, nodes := shelfModelSetup(t)
	// The existing fixture replaces remote generation and embedding models.
	f.seedProcessingModels(t)
	search := service.NewSearchService(shelfSearchUnavailableVectors{f.params.LibraryService}, f.params.SpaceService, nil, f.db)
	f.router.POST("/api/v1/search", handler.NewSearchHandler(search, nil).SearchEntry)
	return f, library, nodes
}

func shelfSearchText(t *testing.T, f *managedNamesFixture, libraryID, title, content string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"items": []any{map[string]any{"library": libraryID, "title": title, "content": content, "acquisition": "verbatim"}}})
	require.NoError(t, err)
	response := personalRequest(t, ingestAuthHTTPRouter(f.mcpFixture), http.MethodPost, "/api/v1/ingest", string(body), f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var ingested struct{ Data types.IngestBatchResult }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &ingested))
	require.Len(t, ingested.Data.Items, 1)
	entryID := ingested.Data.Items[0].EntryID
	require.NotEmpty(t, entryID)
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	return entryID
}

func shelfSearchVersion(t *testing.T, f *managedNamesFixture, libraryID, ownerID, fileID, entryID, label string) string {
	t.Helper()
	op := map[string]any{"id": "document", "type": "add_document_version", "document": map[string]any{
		"owner_id": ownerID, "name": "File " + label, "entry_id": entryID, "cloud_url": "https://example.org/" + label, "version_label": label, "stage": "final",
	}}
	if fileID != "" {
		op["entity_id"] = fileID
	}
	var saved types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+libraryID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{op}}, http.StatusCreated, &saved)
	return saved.Items[0].EntityID
}

// Medium spec, task 10 / ADR 0024: old text is excluded before recall even
// without a node scope; ordinary text and the current version remain searchable.
func TestShelfSearchDefaultsToCurrentVersions(t *testing.T) {
	f, library, nodes := newShelfSearchFixture(t)
	old := shelfSearchText(t, f, library.ID, "Original manual", "retiredneedle describes the previous specification.")
	fileID := shelfSearchVersion(t, f, library.ID, shelfNode(nodes, "model"), "", old, "first")
	current := shelfSearchText(t, f, library.ID, "Current manual", "currentneedle describes the replacement specification.")
	shelfSearchVersion(t, f, library.ID, shelfNode(nodes, "model"), fileID, current, "second")
	ordinary := shelfSearchText(t, f, library.ID, "Independent material", "independentneedle remains searchable without an owner.")
	for _, tc := range []struct{ query, expected string }{{"retiredneedle", ""}, {"currentneedle", current}, {"independentneedle", ordinary}} {
		var result types.EntrySearchResponse
		f.request(http.MethodPost, "/search", map[string]any{"libraries": []string{library.ID}, "query": tc.query}, http.StatusOK, &result)
		if tc.expected == "" {
			require.Empty(t, result.Results, tc.query)
		} else {
			require.Len(t, result.Results, 1, tc.query)
			require.Equal(t, tc.expected, result.Results[0].EntryID)
		}
	}
}

// Medium spec, task 10 / ADR 0024: history is opt-in and matches are grouped
// by file, retaining version identity and snippets instead of mixing citations.
func TestShelfSearchHistoryMergesMatchingVersions(t *testing.T) {
	f, library, nodes := newShelfSearchFixture(t)
	owner := shelfNode(nodes, "model")
	old := shelfSearchText(t, f, library.ID, "Early manual", "archivedneedle is only in the earliest specification.")
	fileID := shelfSearchVersion(t, f, library.ID, owner, "", old, "early")
	previous := shelfSearchText(t, f, library.ID, "Previous manual", "sharedneedle was recorded in the previous specification.")
	shelfSearchVersion(t, f, library.ID, owner, fileID, previous, "previous")
	current := shelfSearchText(t, f, library.ID, "Latest manual", "sharedneedle is recorded in the current specification.")
	shelfSearchVersion(t, f, library.ID, owner, fileID, current, "latest")
	var response map[string]any
	f.request(http.MethodPost, "/search", map[string]any{"libraries": []string{library.ID}, "query": "sharedneedle", "include_history": true, "collapse_similar": false, "limit": 1}, http.StatusOK, &response)
	results := response["results"].([]any)
	require.Len(t, results, 1)
	result := results[0].(map[string]any)
	require.Equal(t, current, result["entry_id"])
	require.Equal(t, true, result["historical_match"])
	document := result["shelf_document"].(map[string]any)
	require.Equal(t, fileID, document["file_id"])
	require.Equal(t, "latest", document["version_label"])
	require.Equal(t, true, document["is_current"])
	others := result["other_version_matches"].([]any)
	require.Len(t, others, 1)
	previousResult := others[0].(map[string]any)
	require.Equal(t, previous, previousResult["entry_id"])
	require.Equal(t, false, previousResult["shelf_document"].(map[string]any)["is_current"])
	require.Contains(t, previousResult["snippets"].([]any)[0].(map[string]any)["content"], "sharedneedle")
	f.request(http.MethodPost, "/search", map[string]any{"libraries": []string{library.ID}, "query": "archivedneedle", "include_history": true}, http.StatusOK, &response)
	results = response["results"].([]any)
	require.Len(t, results, 1)
	result = results[0].(map[string]any)
	require.Equal(t, old, result["entry_id"])
	require.Equal(t, true, result["historical_match"])
	require.Equal(t, false, result["shelf_document"].(map[string]any)["is_current"])
}

// Medium spec, task 10 / ADR 0023 and 0024: a scope follows downward shelf
// structure and model composition, without leaking siblings or unowned text.
func TestShelfSearchScopeFollowsDescendantsAndParts(t *testing.T) {
	f, library, nodes := newShelfSearchFixture(t)
	entries := map[string]string{}
	for _, owner := range []string{"platform", "product", "model", "single", "a", "b", "fixed"} {
		entries[owner] = shelfSearchText(t, f, library.ID, "Scoped "+owner, "scopedneedle documents the synthetic assembly.")
		shelfSearchVersion(t, f, library.ID, shelfNode(nodes, owner), "", entries[owner], owner)
	}
	shelfSearchText(t, f, library.ID, "Unassigned file", "scopedneedle mentions Synthetic model without an explicit classification.")
	var outsider types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "model", "name": "Unrelated model"}, http.StatusCreated, &outsider)
	outside := shelfSearchText(t, f, library.ID, "Unrelated file", "scopedneedle belongs to another model.")
	shelfSearchVersion(t, f, library.ID, outsider.ID, "", outside, "unrelated")
	for _, tc := range []struct {
		roots, owners []string
	}{
		{[]string{"platform"}, []string{"platform", "product", "model", "single", "a", "b", "fixed"}},
		{[]string{"model"}, []string{"model", "a", "b", "fixed"}},
		{[]string{"a"}, []string{"a"}},
		{[]string{"a", "single"}, []string{"a", "single", "fixed"}},
	} {
		ids, expected := []string{}, []string{}
		for _, root := range tc.roots {
			ids = append(ids, shelfNode(nodes, root))
		}
		for _, owner := range tc.owners {
			expected = append(expected, entries[owner])
		}
		var response types.EntrySearchResponse
		f.request(http.MethodPost, "/search", map[string]any{"libraries": []string{library.Directory}, "query": "scopedneedle", "shelf_node_ids": ids, "collapse_similar": false, "limit": 100}, http.StatusOK, &response)
		actual := []string{}
		for _, result := range response.Results {
			actual = append(actual, result.EntryID)
		}
		require.ElementsMatch(t, expected, actual, tc.roots)
	}
}

// Medium spec, task 10: MCP and HTTP share scope/version behavior; the MCP
// fixture also exercises vector and header recall alongside body keyword recall.
func TestShelfSearchMCPUsesScopeAndHistory(t *testing.T) {
	f, library, nodes := newShelfSearchFixture(t)
	owner := shelfNode(nodes, "model")
	old := shelfSearchText(t, f, library.ID, "Transport manual", "transportneedle is the original version.")
	fileID := shelfSearchVersion(t, f, library.ID, owner, "", old, "original")
	current := shelfSearchText(t, f, library.ID, "Transport replacement", "transportneedle is the replacement version.")
	shelfSearchVersion(t, f, library.ID, owner, fileID, current, "replacement")
	shelfSearchText(t, f, library.ID, "Unassigned transport file", "transportneedle is unrelated text.")
	client := f.connect(t, "2025-11-25")
	for _, history := range []bool{false, true} {
		input := map[string]any{"libraries": []string{library.ID}, "query": "transportneedle", "shelf_node_ids": []string{owner}, "include_history": history}
		var mcpResult, httpResult types.EntrySearchResponse
		mcpCall(t, client, "search", input, &mcpResult)
		f.request(http.MethodPost, "/search", input, http.StatusOK, &httpResult)
		for _, result := range []*types.EntrySearchResponse{&mcpResult, &httpResult} {
			var snapshot struct{ Request types.EntrySearchRequest }
			require.NoError(t, json.Unmarshal(result.Snapshot, &snapshot))
			require.Equal(t, []string{owner}, snapshot.Request.ShelfNodeIDs)
			require.Equal(t, history, snapshot.Request.IncludeHistory)
			require.Len(t, result.Results, 1)
			hit := result.Results[0]
			require.Equal(t, current, hit.EntryID)
			require.Equal(t, fileID, hit.ShelfDocument.FileID)
			require.Equal(t, history, hit.HistoricalMatch)
			if history {
				require.Len(t, hit.OtherVersionMatches, 1)
				require.Equal(t, old, hit.OtherVersionMatches[0].EntryID)
			} else {
				require.Empty(t, hit.OtherVersionMatches)
			}
		}
	}
}

// Medium spec, task 10: invalid scope IDs never broaden into a library search.
func TestShelfSearchRejectsInvalidScope(t *testing.T) {
	f, library, nodes := newShelfSearchFixture(t)
	var other types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Other shelf", "directory": "other-search-shelf", "entity_mode": "managed", "shelf_enabled": true}, http.StatusCreated, &other)
	var requirement types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "document_requirement", "name": "Search requirement", "attributes": shelfFileRequirementAttributes(f, library)}, http.StatusCreated, &requirement)
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = shelfNode(nodes, "model")
	}
	for _, tc := range []struct {
		libraries, roots []string
	}{
		{[]string{library.ID}, []string{"missing"}},
		{[]string{library.ID}, []string{" "}},
		{[]string{library.ID}, []string{requirement.ID}},
		{[]string{library.ID}, tooMany},
		{[]string{other.ID}, []string{shelfNode(nodes, "model")}},
	} {
		f.request(http.MethodPost, "/search", map[string]any{"libraries": tc.libraries, "query": "scope", "shelf_node_ids": tc.roots}, http.StatusBadRequest, nil)
	}
	var result types.EntrySearchResponse
	f.request(http.MethodPost, "/search", map[string]any{"libraries": []string{library.ID}, "query": "scope", "shelf_node_ids": []string{shelfNode(nodes, "model")}}, http.StatusOK, &result)
	require.Empty(t, result.Results)
}

// Medium spec, task 10: including historical file versions does not restore
// a composition relation that has ended in the current shelf structure.
func TestShelfSearchScopeUsesEffectiveComposition(t *testing.T) {
	f, library, nodes := newShelfSearchFixture(t)
	entry := shelfSearchText(t, f, library.ID, "Component history", "endedneedle documents a previously used component.")
	shelfSearchVersion(t, f, library.ID, shelfNode(nodes, "b"), "", entry, "ended")
	search := func(root string) []string {
		var result types.EntrySearchResponse
		f.request(http.MethodPost, "/search", map[string]any{"libraries": []string{library.ID}, "query": "endedneedle", "shelf_node_ids": []string{root}, "include_history": true}, http.StatusOK, &result)
		ids := []string{}
		for _, hit := range result.Results {
			ids = append(ids, hit.EntryID)
		}
		return ids
	}
	require.Equal(t, []string{entry}, search(shelfNode(nodes, "model")))
	var groups []service.RelationGroup
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model")+"/relations", nil, http.StatusOK, &groups)
	ended := false
	for _, group := range groups {
		for _, relation := range group.Relations {
			if relation.RelationType == "composed_of" && relation.ObjectID == shelfNode(nodes, "b") {
				f.request(http.MethodDelete, "/relations/"+relation.ID+"?version="+fmt.Sprint(relation.Version), nil, http.StatusOK, nil)
				ended = true
			}
		}
	}
	require.True(t, ended)
	require.Empty(t, search(shelfNode(nodes, "model")))
	require.Equal(t, []string{entry}, search(shelfNode(nodes, "b")))
}

// Medium spec, task 10 / ADR 0024: reports owned by a certification stay
// discoverable through its owning node, without leaking sibling certifications.
func TestShelfSearchScopeIncludesCertificationFiles(t *testing.T) {
	f, library, nodes := newShelfSearchFixture(t)
	configureCertificationStandards(f, &library)
	entries := []string{}
	for _, owner := range []string{"model", "single"} {
		var saved types.ShelfChangeset
		f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{
			"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Search certification " + owner, "owner_id": shelfNode(nodes, owner), "standard": "standard_a", "certification_object": "Synthetic assembly", "region": "region_a", "number": "SYN-SEARCH-" + owner},
		}}}, http.StatusCreated, &saved)
		entry := shelfSearchText(t, f, library.ID, "Certificate "+owner, "certificateneedle records the synthetic test result.")
		f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{
			"id": "report", "type": "add_document_version", "document": map[string]any{"name": "Search report " + owner, "owner_id": shelfNode(saved, "cert"), "certificate_number": "SYN-SEARCH-" + owner, "entry_id": entry, "cloud_url": "https://example.org/report/" + owner, "stage": "final"},
		}}}, http.StatusCreated, nil)
		entries = append(entries, entry)
	}
	var result types.EntrySearchResponse
	f.request(http.MethodPost, "/search", map[string]any{"libraries": []string{library.ID}, "query": "certificateneedle", "shelf_node_ids": []string{shelfNode(nodes, "model")}}, http.StatusOK, &result)
	require.Len(t, result.Results, 1)
	require.Equal(t, entries[0], result.Results[0].EntryID)
}

// Medium spec, task 10 / ADR 0024: an unparsed replacement never silently
// falls back to the old text, which remains available only with history enabled.
func TestShelfSearchUnparsedCurrentDoesNotExposeOldText(t *testing.T) {
	f, library, nodes := newShelfSearchFixture(t)
	owner := shelfNode(nodes, "model")
	old := shelfSearchText(t, f, library.ID, "Parsed original", "unparsedneedle describes the old manual.")
	fileID := shelfSearchVersion(t, f, library.ID, owner, "", old, "parsed")
	shelfSearchVersion(t, f, library.ID, owner, fileID, "", "unparsed")
	for _, history := range []bool{false, true} {
		var response types.EntrySearchResponse
		f.request(http.MethodPost, "/search", map[string]any{"libraries": []string{library.ID}, "query": "unparsedneedle", "shelf_node_ids": []string{owner}, "include_history": history}, http.StatusOK, &response)
		if history {
			require.Len(t, response.Results, 1)
			require.Equal(t, old, response.Results[0].EntryID)
			require.True(t, response.Results[0].HistoricalMatch)
			require.False(t, response.Results[0].ShelfDocument.IsCurrent)
		} else {
			require.Empty(t, response.Results)
		}
	}
}

// Medium spec, task 10: the requested result limit counts files after merging,
// so multiple versions of one file do not consume the second file's result slot.
func TestShelfSearchLimitCountsFiles(t *testing.T) {
	f, library, nodes := newShelfSearchFixture(t)
	owner, fileID := shelfNode(nodes, "model"), ""
	for _, label := range []string{"first", "second", "third"} {
		entry := shelfSearchText(t, f, library.ID, "Limited "+label, "limitneedle records the same synthetic manual.")
		fileID = shelfSearchVersion(t, f, library.ID, owner, fileID, entry, label)
	}
	other := shelfSearchText(t, f, library.ID, "Separate limited file", "limitneedle records the same synthetic manual.")
	otherFile := shelfSearchVersion(t, f, library.ID, owner, "", other, "separate")
	var response types.EntrySearchResponse
	f.request(http.MethodPost, "/search", map[string]any{"libraries": []string{library.ID}, "query": "limitneedle", "include_history": true, "collapse_similar": false, "limit": 2}, http.StatusOK, &response)
	require.Len(t, response.Results, 2)
	require.ElementsMatch(t, []string{fileID, otherFile}, []string{response.Results[0].ShelfDocument.FileID, response.Results[1].ShelfDocument.FileID})
}
