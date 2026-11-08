package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func shelfModelSetup(t *testing.T, processing ...mcpProcessingFixture) (*managedNamesFixture, types.Library, types.ShelfChangeset) {
	t.Helper()
	f, library := newProductShelfFixture(t, processing...)
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": map[string]any{
		"module_types": []any{map[string]any{"key": "module_a", "name": "Module A"}, map[string]any{"key": "module_b", "name": "Module B"}},
		"regions":      []any{map[string]any{"key": "region_a", "name": "Region A"}},
	}}, http.StatusOK, &library)
	operations := []any{}
	for _, node := range []struct{ id, kind string }{{"line", "product_line"}, {"platform", "platform"}, {"product", "product"}, {"model", "model"}, {"single", "model"}, {"a", "part"}, {"b", "part"}, {"fixed", "part"}} {
		operations = append(operations, map[string]any{"id": node.id, "type": "create_entity", "entity": map[string]any{"kind": node.kind, "name": "Synthetic " + node.id}})
	}
	for _, edge := range []struct{ child, parent, kind, module string }{{"platform", "line", "belongs_to", ""}, {"product", "platform", "belongs_to", ""}, {"model", "product", "belongs_to", ""}, {"single", "product", "belongs_to", ""}, {"model", "a", "composed_of", "module_a"}, {"model", "b", "composed_of", "module_a"}, {"model", "fixed", "composed_of", "module_b"}, {"single", "fixed", "composed_of", "module_b"}} {
		attrs := map[string]any{}
		if edge.module != "" {
			attrs["module_type"] = edge.module
		}
		operations = append(operations, map[string]any{"id": edge.child + "_" + edge.parent, "type": "save_relation", "relation": map[string]any{"subject_id": "$" + edge.child, "object_id": "$" + edge.parent, "relation_type": edge.kind, "attributes": attrs}})
	}
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": operations}, http.StatusCreated, &draft)
	return f, library, draft
}

func shelfNode(draft types.ShelfChangeset, id string) string {
	for _, item := range draft.Items {
		if item.ID == id {
			return item.EntityID
		}
	}
	return ""
}

// Medium spec, task 04 / ADR 0023: detail groups fixed and optional parts;
// a separately sold model stays a distinct model containing one part.
func TestShelfModelDetailReturnsComposition(t *testing.T) {
	f, _, draft := shelfModelSetup(t)
	var detail map[string]any
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "model"), nil, http.StatusOK, &detail)
	shelf, ok := detail["shelf"].(map[string]any)
	require.True(t, ok, "model detail must expose shelf composition")
	groups := shelf["composition"].([]any)
	require.Len(t, groups, 2)
	first := groups[0].(map[string]any)
	require.Equal(t, "module_a", first["module_type"])
	require.Equal(t, true, first["optional"])
	require.Len(t, first["parts"], 2)
	second := groups[1].(map[string]any)
	require.Equal(t, false, second["optional"])
	require.Len(t, second["parts"], 1)
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "single"), nil, http.StatusOK, &detail)
	require.Len(t, detail["shelf"].(map[string]any)["composition"], 1)
	require.NotEqual(t, shelfNode(draft, "single"), shelfNode(draft, "fixed"))
}

// Medium spec, task 04 / ADR 0023: statuses retain their exact source spelling
// and conjunctive conditions; selecting a configuration filters applicable facts.
func TestShelfModelConditionalStatuses(t *testing.T) {
	f, library, draft := shelfModelSetup(t)
	id := shelfNode(draft, "model")
	a, b, fixed := shelfNode(draft, "a"), shelfNode(draft, "b"), shelfNode(draft, "fixed")
	conditions := []any{map[string]any{"module_type": "module_a", "part_ids": []string{a}}, map[string]any{"module_type": "module_b", "part_ids": []string{fixed}}}
	var saved types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "statuses", "type": "set_sale_statuses", "entity_id": id, "revision": 1, "sale_statuses": []any{
		map[string]any{"region": "region_a", "status": "GA", "planned_eom": true, "notes": " Original note ", "raw": "GA (Original note)", "conditions": conditions},
		map[string]any{"region": "region_a", "status": "DEV", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{b}}}},
	}}}}, http.StatusCreated, &saved)
	var detail map[string]any
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &detail)
	statuses := detail["shelf"].(map[string]any)["sale_statuses"].([]any)
	require.Len(t, statuses, 2)
	require.Equal(t, "GA (Original note)", statuses[0].(map[string]any)["raw"])
	require.Equal(t, " Original note ", statuses[0].(map[string]any)["notes"])
	require.Equal(t, true, statuses[0].(map[string]any)["planned_eom"])
	require.Len(t, statuses[0].(map[string]any)["conditions"], 2)
	combination, _ := json.Marshal(map[string]string{"module_a": a, "module_b": fixed})
	f.request(http.MethodGet, "/entities/"+id+"?combination="+url.QueryEscape(string(combination)), nil, http.StatusOK, &detail)
	statuses = detail["shelf"].(map[string]any)["sale_statuses"].([]any)
	require.Len(t, statuses, 1)
	require.Equal(t, "GA", statuses[0].(map[string]any)["status"])
	combination, _ = json.Marshal(map[string]string{"module_a": b, "module_b": fixed})
	f.request(http.MethodGet, "/entities/"+id+"?combination="+url.QueryEscape(string(combination)), nil, http.StatusOK, &detail)
	statuses = detail["shelf"].(map[string]any)["sale_statuses"].([]any)
	require.Len(t, statuses, 1)
	require.Equal(t, "DEV", statuses[0].(map[string]any)["status"])
}

// Medium spec, task 04: parameters are defined for product lines, retain
// conditional scalar/range values and reject assignment outside their scope.
func TestShelfModelParametersRespectProductLineScope(t *testing.T) {
	f, library, draft := shelfModelSetup(t)
	path := "/libraries/" + library.ID
	var other types.Entity
	f.request(http.MethodPost, path+"/entities", map[string]any{"kind": "product_line", "name": "Other synthetic line"}, http.StatusCreated, &other)
	fields := append(library.EntitySchema.Fields, types.EntityFieldDefinition{Key: "energy", Name: "Energy", Type: "number", Unit: "kWh", Kinds: []string{"model"}, ProductLineIDs: []string{shelfNode(draft, "line")}})
	schema := types.EntitySchema{Kinds: library.EntitySchema.Kinds, Fields: fields}
	f.request(http.MethodPut, path, map[string]any{"entity_schema": schema}, http.StatusOK, &library)
	var saved types.ShelfChangeset
	values := map[string]any{"energy": []any{
		map[string]any{"value": map[string]any{"min": 9, "max": 54}, "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{shelfNode(draft, "a")}}}},
		map[string]any{"value": 27, "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{shelfNode(draft, "b")}}}},
	}}
	f.request(http.MethodPost, path+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "parameter", "type": "set_parameter_values", "entity_id": shelfNode(draft, "model"), "revision": 1, "parameter_values": values}}}, http.StatusCreated, &saved)
	var detail map[string]any
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "model"), nil, http.StatusOK, &detail)
	parameters := detail["shelf"].(map[string]any)["parameters"].(map[string]any)
	energy := parameters["energy"].([]any)
	require.Len(t, energy, 2)
	require.Equal(t, map[string]any{"min": float64(9), "max": float64(54)}, energy[0].(map[string]any)["value"])
	require.EqualValues(t, 27, energy[1].(map[string]any)["value"])
	combination, _ := json.Marshal(map[string]string{"module_a": shelfNode(draft, "b")})
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "model")+"?combination="+url.QueryEscape(string(combination)), nil, http.StatusOK, &detail)
	energy = detail["shelf"].(map[string]any)["parameters"].(map[string]any)["energy"].([]any)
	require.Len(t, energy, 1)
	require.EqualValues(t, 27, energy[0].(map[string]any)["value"])
	// Removing the owning line from a used field must not reinterpret history.
	fields[len(fields)-1].ProductLineIDs = []string{other.ID}
	schema.Fields = fields
	f.request(http.MethodPut, path, map[string]any{"entity_schema": schema}, http.StatusBadRequest, nil)
	// A field for another line cannot be assigned.
	f.request(http.MethodGet, path, nil, http.StatusOK, &library)
	power := types.EntityFieldDefinition{Key: "power", Name: "Power", Type: "number", Kinds: []string{"model"}, ProductLineIDs: []string{other.ID}}
	schema.Fields = append(library.EntitySchema.Fields, power)
	f.request(http.MethodPut, path, map[string]any{"entity_schema": schema}, http.StatusOK, nil)
	f.request(http.MethodPost, path+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "outside", "type": "set_parameter_values", "entity_id": shelfNode(draft, "model"), "revision": 2, "parameter_values": map[string]any{"power": []any{map[string]any{"value": 10}}}}}}, http.StatusCreated, &saved)
	require.NotEmpty(t, saved.Items[0].Conflicts)
	require.Equal(t, "unparseable", saved.Items[0].Conflicts[0].Type)
	f.request(http.MethodPost, path+"/shelf/changesets/"+saved.ID+"/confirm", map[string]any{"revision": saved.Revision, "decisions": []any{map[string]any{"item_id": "outside", "action": "apply"}}}, http.StatusConflict, nil)
}

// Medium spec, task 04: changing a separately sold component's status warns
// about other models using that part without rewriting their statuses.
func TestShelfModelComponentStatusOnlyWarns(t *testing.T) {
	f, library, draft := shelfModelSetup(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var saved types.ShelfChangeset
	set := func(id string, revision int, status string) {
		f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "status", "type": "set_sale_statuses", "entity_id": id, "revision": revision, "sale_statuses": []any{map[string]any{"region": "region_a", "status": status}}}}}, http.StatusCreated, &saved)
	}
	set(shelfNode(draft, "model"), 1, "GA")
	set(shelfNode(draft, "single"), 1, "DEV")
	require.Len(t, saved.Items[0].Warnings, 1)
	require.Equal(t, []string{shelfNode(draft, "model")}, saved.Items[0].Warnings[0].ModelIDs)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "model"), nil, http.StatusOK, &detail)
	require.EqualValues(t, 2, detail.Entity.Revision)
	require.Equal(t, "GA", detail.Shelf.SaleStatuses[0].Status)
	set(shelfNode(draft, "single"), 2, "EOM")
	require.Len(t, saved.Items[0].Warnings, 1)
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "model"), nil, http.StatusOK, &detail)
	require.Equal(t, "GA", detail.Shelf.SaleStatuses[0].Status)
	require.EqualValues(t, 2, detail.Entity.Revision)
}

// Medium spec, task 04: blank regions are unresolved proposals, never
// silently converted to a pending status; the submitter can correct or discard.
func TestShelfModelBlankRegionalStatusIsConflict(t *testing.T) {
	f, library, draft := shelfModelSetup(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var saved types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "blank", "type": "set_sale_statuses", "entity_id": shelfNode(draft, "model"), "revision": 1, "sale_statuses": []any{map[string]any{"region": "region_a", "status": "", "raw": ""}}}}}, http.StatusCreated, &saved)
	require.Equal(t, "uncertain", saved.Items[0].Conflicts[0].Type)
	f.request(http.MethodPost, path+"/"+saved.ID+"/confirm", map[string]any{"revision": saved.Revision, "decisions": []any{map[string]any{"item_id": "blank", "action": "apply"}}}, http.StatusConflict, nil)
	f.request(http.MethodPatch, path+"/"+saved.ID, map[string]any{"revision": saved.Revision, "decisions": []any{map[string]any{"item_id": "blank", "action": "replace", "replacement": map[string]any{"type": "set_sale_statuses", "entity_id": shelfNode(draft, "model"), "revision": 1, "sale_statuses": []any{map[string]any{"region": "region_a", "status": "待定", "raw": ""}}}}}}, http.StatusOK, &saved)
	f.request(http.MethodPost, path+"/"+saved.ID+"/confirm", map[string]any{"revision": saved.Revision}, http.StatusOK, nil)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "model"), nil, http.StatusOK, &detail)
	require.Equal(t, "待定", detail.Shelf.SaleStatuses[0].Status)
}

// Medium spec, task 04 / ADR 0023: module keys and part conditions are scoped
// to the model; invalid combinations and unfinished rules cannot become facts.
func TestShelfModelRejectsInvalidCompositionAndConditions(t *testing.T) {
	f, library, draft := shelfModelSetup(t)
	for _, tc := range []struct {
		name string
		op   map[string]any
	}{
		{"unknown module", map[string]any{"type": "save_relation", "relation": map[string]any{"subject_id": shelfNode(draft, "single"), "object_id": shelfNode(draft, "a"), "relation_type": "composed_of", "attributes": map[string]any{"module_type": "unknown"}}}},
		{"missing module", map[string]any{"type": "save_relation", "relation": map[string]any{"subject_id": shelfNode(draft, "single"), "object_id": shelfNode(draft, "a"), "relation_type": "composed_of"}}},
		{"part in another module", map[string]any{"type": "set_sale_statuses", "entity_id": shelfNode(draft, "model"), "revision": 1, "sale_statuses": []any{map[string]any{"region": "region_a", "status": "GA", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{shelfNode(draft, "fixed")}}}}}}},
		{"empty condition set", map[string]any{"type": "set_sale_statuses", "entity_id": shelfNode(draft, "model"), "revision": 1, "sale_statuses": []any{map[string]any{"region": "region_a", "status": "GA", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{}}}}}}},
		{"unknown region", map[string]any{"type": "set_sale_statuses", "entity_id": shelfNode(draft, "model"), "revision": 1, "sale_statuses": []any{map[string]any{"region": "unknown", "status": "GA"}}}},
		{"overlapping statuses", map[string]any{"type": "set_sale_statuses", "entity_id": shelfNode(draft, "model"), "revision": 1, "sale_statuses": []any{map[string]any{"region": "region_a", "status": "GA"}, map[string]any{"region": "region_a", "status": "DEV"}}}},
		{"unfinished rules", map[string]any{"type": "update_entity", "entity_id": shelfNode(draft, "model"), "patch": map[string]any{"revision": 1, "attributes": map[string]any{"combination_rules": []any{[]any{map[string]any{"module_type": "module_a", "part_ids": []string{shelfNode(draft, "a")}}}}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.op["id"] = "invalid"
			var proposal types.ShelfChangeset
			f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{tc.op}}, http.StatusCreated, &proposal)
			require.NotEmpty(t, proposal.Items[0].Conflicts)
			require.NotEqual(t, "new", proposal.Items[0].Conflicts[0].Type)
			f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+proposal.ID+"/confirm", map[string]any{"revision": proposal.Revision, "decisions": []any{map[string]any{"item_id": "invalid", "action": "apply"}}}, http.StatusConflict, nil)
		})
	}
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "model")+"?combination="+url.QueryEscape(`{"module_a":"outside"}`), nil, http.StatusBadRequest, nil)
}

// Medium spec, task 04: pack, cluster and container identifiers are fields of
// one part, remain verbatim, and appear with that part in model detail.
func TestShelfModelPartDetailsRemainOnPart(t *testing.T) {
	f, _, draft := shelfModelSetup(t)
	var part types.Entity
	f.request(http.MethodPatch, "/entities/"+shelfNode(draft, "a"), map[string]any{"revision": 1, "attributes": map[string]any{"module_type": "module_a", "pack_model": " Pack X ", "cluster_model": "Cluster X", "container_model": "Container X"}}, http.StatusOK, &part)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "model"), nil, http.StatusOK, &detail)
	require.Equal(t, " Pack X ", detail.Shelf.Composition[0].Parts[0].Attributes["pack_model"])
	require.Equal(t, "Cluster X", detail.Shelf.Composition[0].Parts[0].Attributes["cluster_model"])
	require.Equal(t, "Container X", detail.Shelf.Composition[0].Parts[0].Attributes["container_model"])
	require.NotContains(t, detail.Entity.Attributes, "pack_model")
	f.request(http.MethodPatch, "/entities/"+part.ID, map[string]any{"revision": part.Revision, "attributes": map[string]any{"module_type": "module_b"}}, http.StatusBadRequest, nil)
}

// Medium spec, task 04: malformed numeric ranges cannot be confirmed.
func TestShelfModelRejectsInvalidParameterRanges(t *testing.T) {
	f, library, draft := shelfModelSetup(t)
	library.EntitySchema.Fields = append(library.EntitySchema.Fields, types.EntityFieldDefinition{Key: "energy", Name: "Energy", Type: "number", Kinds: []string{"model"}, ProductLineIDs: []string{shelfNode(draft, "line")}})
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_schema": library.EntitySchema}, http.StatusOK, nil)
	for _, value := range []any{map[string]any{"min": 54, "max": 9}, map[string]any{"min": 9}, map[string]any{"min": nil, "max": 54}, map[string]any{"min": "9", "max": 54}, map[string]any{"min": 9, "max": 54, "unit": "kWh"}, nil} {
		var proposal types.ShelfChangeset
		f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "range", "type": "set_parameter_values", "entity_id": shelfNode(draft, "model"), "revision": 1, "parameter_values": map[string]any{"energy": []any{map[string]any{"value": value}}}}}}, http.StatusCreated, &proposal)
		require.NotEmpty(t, proposal.Items[0].Conflicts)
		require.Equal(t, "unparseable", proposal.Items[0].Conflicts[0].Type)
	}
}

// Medium spec, task 04: referenced parts in a deferred status proposal
// remain valid after its original batch is confirmed.
func TestShelfModelStatusReferencesSurviveDeferral(t *testing.T) {
	f, library, draft := shelfModelSetup(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var saved types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{
		map[string]any{"id": "part", "type": "create_entity", "entity": map[string]any{"kind": "part", "name": "Deferred optional part"}},
		map[string]any{"id": "composition", "type": "save_relation", "relation": map[string]any{"subject_id": shelfNode(draft, "model"), "object_id": "$part", "relation_type": "composed_of", "attributes": map[string]any{"module_type": "module_a"}}},
		map[string]any{"id": "status", "type": "set_sale_statuses", "entity_id": shelfNode(draft, "model"), "revision": 1, "sale_statuses": []any{map[string]any{"region": "region_a", "status": "GA", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{"$part"}}}}}},
	}}, http.StatusCreated, &saved)
	require.Equal(t, "modified", saved.Items[2].Conflicts[0].Type)
	partID := saved.Items[0].EntityID
	f.request(http.MethodPost, path+"/"+saved.ID+"/confirm", map[string]any{"revision": saved.Revision, "decisions": []any{map[string]any{"item_id": "part", "action": "apply"}, map[string]any{"item_id": "composition", "action": "apply"}, map[string]any{"item_id": "status", "action": "defer"}}}, http.StatusOK, &saved)
	f.request(http.MethodGet, path+"/"+saved.DeferredChangesetID, nil, http.StatusOK, &saved)
	f.request(http.MethodPost, path+"/"+saved.ID+"/confirm", map[string]any{"revision": saved.Revision, "decisions": []any{map[string]any{"item_id": "status", "action": "apply"}}}, http.StatusOK, nil)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "model"), nil, http.StatusOK, &detail)
	require.Equal(t, []string{partID}, detail.Shelf.SaleStatuses[0].Conditions[0].PartIDs)
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "model")+"?as_of=2000-01-01T00:00:00Z", nil, http.StatusOK, &detail)
	require.Empty(t, detail.Shelf.SaleStatuses)
	require.Empty(t, detail.Shelf.Composition)
}

// Medium spec, task 04: importing composition and conditional statuses uses
// the same file date, so a historical query sees both together.
func TestShelfModelImportUsesFileDateForComposition(t *testing.T) {
	f, library := newProductShelfFixture(t)
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": types.ShelfConfig{ModuleTypes: []types.ShelfOption{{Key: "module_a", Name: "Module A"}}, Regions: []types.ShelfOption{{Key: "region_a", Name: "Region A"}}}}, http.StatusOK, nil)
	var saved types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "import", "import_batch_id": "historical-components", "file_date": "2025-01-01T00:00:00Z"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "model", "type": "create_entity", "entity": map[string]any{"kind": "model", "name": "Historical model"}},
		map[string]any{"id": "part", "type": "create_entity", "entity": map[string]any{"kind": "part", "name": "Historical part"}},
		map[string]any{"id": "composition", "type": "save_relation", "relation": map[string]any{"subject_id": "$model", "object_id": "$part", "relation_type": "composed_of", "attributes": map[string]any{"module_type": "module_a"}}},
		map[string]any{"id": "status", "type": "set_sale_statuses", "entity_id": "$model", "revision": 1, "sale_statuses": []any{map[string]any{"region": "region_a", "status": "GA", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{"$part"}}}}}},
	}}, http.StatusCreated, &saved)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(saved, "model")+"?as_of=2025-01-02T00:00:00Z", nil, http.StatusOK, &detail)
	require.Len(t, detail.Shelf.Composition, 1)
	require.Equal(t, "GA", detail.Shelf.SaleStatuses[0].Status)
}

// Medium regression, task 04: composition edits must not strand conditional
// facts; clearing the fact first permits the edit and preserves history.
func TestShelfModelCompositionEditsPreserveConditionalFacts(t *testing.T) {
	f, library, draft := shelfModelSetup(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	id := shelfNode(draft, "model")
	var relation types.Relation
	for _, item := range draft.Items {
		if item.ID == "model_a" {
			require.NoError(t, json.Unmarshal(item.After, &relation))
		}
	}
	require.NotEmpty(t, relation.ID)
	source := types.OperationSource{Type: "web"}
	status := types.ShelfOperation{ID: "status", Type: "set_sale_statuses", EntityID: id, Revision: 1, SaleStatuses: &[]types.ShelfSaleStatus{{Region: "region_a", Status: "GA", Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(draft, "a")}}}}}}
	var saved types.ShelfChangeset
	f.request(http.MethodPost, path, types.ShelfChangesetInput{Source: source, Confirmed: true, Operations: []types.ShelfOperation{status}}, http.StatusCreated, &saved)
	beforeEdit := time.Now().UTC().Format(time.RFC3339Nano)
	end := types.ShelfOperation{ID: "end", Type: "end_relation", RelationID: relation.ID, Revision: relation.Version}
	f.request(http.MethodPost, path, types.ShelfChangesetInput{Source: source, Operations: []types.ShelfOperation{end}}, http.StatusCreated, &saved)
	require.NotEqual(t, "modified", saved.Items[0].Conflicts[0].Type)
	f.request(http.MethodPost, path+"/"+saved.ID+"/confirm", types.ShelfChangesetReview{Revision: saved.Revision, Decisions: []types.ShelfDecision{{ItemID: "end", Action: "apply"}}}, http.StatusConflict, nil)
	clear := types.ShelfOperation{ID: "clear", Type: "set_sale_statuses", EntityID: id, Revision: 2, SaleStatuses: &[]types.ShelfSaleStatus{}}
	f.request(http.MethodPost, path, types.ShelfChangesetInput{Source: source, Confirmed: true, Operations: []types.ShelfOperation{clear, end}}, http.StatusCreated, nil)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &detail)
	require.Empty(t, detail.Shelf.SaleStatuses)
	require.Len(t, detail.Shelf.Composition[0].Parts, 1)
	f.request(http.MethodGet, "/entities/"+id+"?as_of="+url.QueryEscape(beforeEdit), nil, http.StatusOK, &detail)
	require.Equal(t, "GA", detail.Shelf.SaleStatuses[0].Status)
	require.Len(t, detail.Shelf.Composition[0].Parts, 2)
}

// Medium regression, task 04: a scheduled component status proposal must show
// its future value and warn dependents while current status remains unchanged.
func TestShelfModelScheduledStatusReturnsFutureDiff(t *testing.T) {
	f, library, draft := shelfModelSetup(t)
	id := shelfNode(draft, "single")
	future := time.Date(2090, 1, 1, 0, 0, 0, 0, time.UTC)
	status := types.ShelfOperation{ID: "scheduled", Type: "set_sale_statuses", EntityID: id, Revision: 1, EffectiveAt: &future, SaleStatuses: &[]types.ShelfSaleStatus{{Region: "region_a", Status: "EOM"}}}
	var saved types.ShelfChangeset
	path := "/libraries/" + library.ID + "/shelf/changesets"
	f.request(http.MethodPost, path, types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Operations: []types.ShelfOperation{status}}, http.StatusCreated, &saved)
	var proposed types.Entity
	require.NoError(t, json.Unmarshal(saved.Items[0].After, &proposed))
	var statuses []types.ShelfSaleStatus
	require.NoError(t, types.DecodeShelfValue(proposed.Attributes["sale_statuses"], &statuses))
	require.Len(t, statuses, 1)
	require.Equal(t, "EOM", statuses[0].Status)
	require.Len(t, saved.Items[0].Warnings, 1)
	f.request(http.MethodPost, path+"/"+saved.ID+"/confirm", types.ShelfChangesetReview{Revision: saved.Revision, Decisions: []types.ShelfDecision{{ItemID: "scheduled", Action: "apply"}}}, http.StatusOK, nil)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &detail)
	require.Empty(t, detail.Shelf.SaleStatuses)
	f.request(http.MethodGet, "/entities/"+id+"?as_of=2090-01-02T00:00:00Z", nil, http.StatusOK, &detail)
	require.Equal(t, "EOM", detail.Shelf.SaleStatuses[0].Status)
}
