package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// Medium spec, product shelf task 02 / ADR 0023: enabling a shelf installs the
// fixed vocabulary on the server, replacing the unused legacy configuration.
func TestProductShelfEnablingInstallsFixedStructure(t *testing.T) {
	f := &managedNamesFixture{mcpFixture: newMCPFixture(t), t: t}
	var library types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Synthetic shelf", "directory": "synthetic-shelf", "entity_mode": "managed"}, http.StatusCreated, &library)
	path := "/libraries/" + library.ID
	f.request(http.MethodPut, path, map[string]any{"entity_schema": types.EntitySchema{Kinds: []types.EntityKind{{Key: "verification_scope", Name: "Legacy"}}}, "relation_types": []types.LibraryRelationType{{Name: "candidate_parent", Enabled: true}}}, http.StatusOK, nil)
	f.request(http.MethodPut, path, map[string]any{"shelf_enabled": true}, http.StatusOK, &library)
	require.True(t, library.ShelfEnabled)
	var keys []string
	for _, kind := range library.EntitySchema.Kinds {
		keys = append(keys, kind.Key)
	}
	require.Equal(t, []string{"product_line", "platform", "product", "model", "part", "document_requirement", "certification", "document_file"}, keys)
	rules := map[string]types.LibraryRelationType{}
	for _, rule := range library.RelationTypes {
		rules[rule.Name] = rule
	}
	require.True(t, rules["belongs_to"].Hierarchical)
	require.Equal(t, []string{"model"}, rules["composed_of"].SubjectKinds)
	require.Equal(t, []string{"part"}, rules["composed_of"].ObjectKinds)
	require.Contains(t, rules, "applies_to")
	require.Contains(t, rules, "has_certification")
	require.Contains(t, rules, "has_document")
	require.NotContains(t, rules, "candidate_parent")
	f.request(http.MethodGet, path, nil, http.StatusOK, &library)
	require.True(t, library.ShelfEnabled)
}

// Medium spec, task 02 / ADR 0023: incompatible legacy records are left for
// their submitter to resolve, without reclassifying or deleting them.
func TestProductShelfEnablingPreservesIncompatibleLegacyData(t *testing.T) {
	f := &managedNamesFixture{mcpFixture: newMCPFixture(t), t: t}
	var library types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Legacy catalog", "directory": "legacy-catalog", "entity_mode": "managed", "entity_schema": types.EntitySchema{Kinds: []types.EntityKind{{Key: "verification_scope", Name: "Legacy scope"}}}}, http.StatusCreated, &library)
	var legacy types.Entity
	path := "/libraries/" + library.ID
	f.request(http.MethodPost, path+"/entities", map[string]any{"kind": "verification_scope", "name": "Synthetic legacy record"}, http.StatusCreated, &legacy)
	f.request(http.MethodPut, path, map[string]any{"shelf_enabled": true}, http.StatusBadRequest, nil)
	f.request(http.MethodGet, path, nil, http.StatusOK, &library)
	require.False(t, library.ShelfEnabled)
	require.Equal(t, "verification_scope", library.EntitySchema.Kinds[0].Key)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+legacy.ID, nil, http.StatusOK, &detail)
	require.Equal(t, "verification_scope", detail.Entity.Kind)
	require.Equal(t, "Synthetic legacy record", detail.Entity.CanonicalName)
}

func newProductShelfFixture(t *testing.T) (*managedNamesFixture, types.Library) {
	t.Helper()
	f := &managedNamesFixture{mcpFixture: newMCPFixture(t), t: t}
	var library types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Synthetic shelf", "directory": "synthetic-shelf", "entity_mode": "managed", "shelf_enabled": true}, http.StatusCreated, &library)
	f.libraryID = library.ID
	return f, library
}

// Medium spec, task 02 / ADR 0023: stable keys survive append, rename and
// reorder, while names keep the caller's original spelling.
func TestProductShelfListsPersistOrderedRenames(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID
	config := map[string]any{}
	for _, list := range []string{"module_types", "regions", "stages"} {
		config[list] = []map[string]string{{"key": "first", "name": " First value "}, {"key": "second", "name": "Second value"}}
	}
	f.request(http.MethodPut, path, map[string]any{"shelf_config": config}, http.StatusOK, nil)
	for _, list := range []string{"module_types", "regions", "stages"} {
		config[list] = []map[string]string{{"key": "second", "name": " Renamed value "}, {"key": "first", "name": " First value "}, {"key": "third", "name": "Third value"}}
	}
	f.request(http.MethodPut, path, map[string]any{"shelf_config": config}, http.StatusOK, nil)
	var saved map[string]any
	f.request(http.MethodGet, path, nil, http.StatusOK, &saved)
	encoded, err := json.Marshal(config)
	require.NoError(t, err)
	var expected any
	require.NoError(t, json.Unmarshal(encoded, &expected))
	require.Equal(t, expected, saved["shelf_config"])
	// An invalid update cannot partially rename another list.
	config["stages"] = []map[string]string{{"key": "first", "name": "Repeated"}, {"key": "second", "name": "Repeated"}, {"key": "third", "name": "Third value"}}
	f.request(http.MethodPut, path, map[string]any{"shelf_config": config}, http.StatusBadRequest, nil)
	f.request(http.MethodGet, path, nil, http.StatusOK, &saved)
	require.Equal(t, expected, saved["shelf_config"])
	config["stages"] = []map[string]string{{"key": "first", "name": "Only remaining"}}
	f.request(http.MethodPut, path, map[string]any{"shelf_config": config}, http.StatusBadRequest, nil)
	var other map[string]any
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Other shelf", "directory": "other-shelf", "entity_mode": "managed", "shelf_enabled": true}, http.StatusCreated, &other)
	require.Empty(t, other["shelf_config"])
}

// Medium spec, task 02 / ADR 0023: the fixed hierarchy enforces allowed
// levels, a single parent and no cycle through the public relation routes.
func TestProductShelfHierarchyRejectsMultipleParentsAndCycles(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID
	nodes := map[string]types.Entity{}
	for _, kind := range []string{"product_line", "platform", "product", "model"} {
		var node types.Entity
		f.request(http.MethodPost, path+"/entities", map[string]any{"name": "Synthetic " + kind, "kind": kind}, http.StatusCreated, &node)
		nodes[kind] = node
	}
	var second types.Entity
	f.request(http.MethodPost, path+"/entities", map[string]any{"name": "Second product", "kind": "product"}, http.StatusCreated, &second)
	edge := func(child, parent string, status int) {
		t.Helper()
		f.request(http.MethodPost, path+"/relations", map[string]any{"subject_id": child, "object_id": parent, "relation_type": "belongs_to", "attributes": map[string]any{}}, status, nil)
	}
	edge(nodes["platform"].ID, nodes["product_line"].ID, http.StatusCreated)
	edge(nodes["product"].ID, nodes["platform"].ID, http.StatusCreated)
	edge(nodes["model"].ID, nodes["product"].ID, http.StatusCreated)
	edge(nodes["model"].ID, second.ID, http.StatusBadRequest)
	edge(nodes["product_line"].ID, nodes["model"].ID, http.StatusBadRequest)
	edge(nodes["model"].ID, nodes["model"].ID, http.StatusBadRequest)
	f.request(http.MethodPut, path, map[string]any{"entity_schema": types.EntitySchema{Kinds: []types.EntityKind{{Key: "model", Name: "Model", ParentKinds: []string{"model"}}}}}, http.StatusBadRequest, nil)
	f.request(http.MethodPut, path, map[string]any{"relation_types": []types.LibraryRelationType{}}, http.StatusBadRequest, nil)
}

// Medium spec, task 02 / ADR 0024: a shelf cannot be a source or a target of
// automatic product matching, including enabling an already selected target.
func TestProductShelfRejectsAutomaticMatchingConfiguration(t *testing.T) {
	f, shelf := newProductShelfFixture(t)
	var products, papers types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Products", "directory": "plain-products", "entity_mode": "managed"}, http.StatusCreated, &products)
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Papers", "directory": "plain-papers", "product_library_ids": []string{products.ID}}, http.StatusCreated, &papers)
	f.request(http.MethodPut, "/libraries/"+shelf.ID, map[string]any{"product_library_ids": []string{products.ID}}, http.StatusBadRequest, nil)
	f.request(http.MethodPut, "/libraries/"+papers.ID, map[string]any{"product_library_ids": []string{shelf.ID}}, http.StatusBadRequest, nil)
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Rejected", "directory": "rejected-target", "product_library_ids": []string{shelf.ID}}, http.StatusBadRequest, nil)
	f.request(http.MethodPut, "/libraries/"+products.ID, map[string]any{"shelf_enabled": true}, http.StatusBadRequest, nil)
	f.request(http.MethodPut, "/libraries/"+papers.ID, map[string]any{"product_library_ids": []string{}}, http.StatusOK, nil)
	f.request(http.MethodPut, "/libraries/"+products.ID, map[string]any{"shelf_enabled": true}, http.StatusOK, nil)
}

// Medium spec, task 02 / ADR 0024: actual ingestion and background processing
// must not infer shelf links; explicit manual links survive reprocessing.
func TestProductShelfIngestionDoesNotMatchBodyNames(t *testing.T) {
	f, library := newProductShelfFixture(t)
	f.seedProcessingModels(t) // The fixture replaces remote models with deterministic responses.
	var product types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"name": "Synthetic Widget Pro", "kind": "model"}, http.StatusCreated, &product)
	input := map[string]any{"items": []any{map[string]any{"library": library.ID, "title": "Synthetic report", "content": "Synthetic Widget Pro provides measured output.", "acquisition": "verbatim"}}}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	router := ingestAuthHTTPRouter(f.mcpFixture)
	response := personalRequest(t, router, http.MethodPost, "/api/v1/ingest", string(body), f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var ingested struct{ Data types.IngestBatchResult }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &ingested))
	require.Len(t, ingested.Data.Items, 1)
	entryID := ingested.Data.Items[0].EntryID
	require.NotEmpty(t, entryID)
	processor := f.articles.(interfaces.EntryService)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	var links []service.ManagedEntityLink
	f.request(http.MethodGet, "/entries/"+entryID+"/entity-links", nil, http.StatusOK, &links)
	require.Empty(t, links)
	f.request(http.MethodPut, "/entries/"+entryID+"/entity-links/"+product.ID, map[string]any{"excluded": false, "relation_type": "references"}, http.StatusOK, nil)
	response = personalRequest(t, router, http.MethodPost, "/api/v1/entries/"+entryID+"/reprocess", "{}", f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	f.request(http.MethodGet, "/entries/"+entryID+"/entity-links", nil, http.StatusOK, &links)
	require.Len(t, links, 1)
	require.Equal(t, "manual", links[0].Origin)
}
