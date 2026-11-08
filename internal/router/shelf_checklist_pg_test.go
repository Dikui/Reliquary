package router

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func checklistSetup(t *testing.T) (*managedNamesFixture, types.Library, types.ShelfChangeset) {
	t.Helper()
	f, l, n := shelfModelSetup(t)
	l.ShelfConfig.Stages = []types.ShelfOption{{Key: "early", Name: "Early custom"}, {Key: "draft_due", Name: "Draft custom"}, {Key: "final_due", Name: "Final custom"}}
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, &l)
	return f, l, n
}
func checklistRequirement(t *testing.T, f *managedNamesFixture, l types.Library, name, draft, final string) types.Entity {
	t.Helper()
	var e types.Entity
	f.request(http.MethodPost, "/libraries/"+l.ID+"/entities", map[string]any{"name": name, "kind": "document_requirement", "attributes": map[string]any{"requirement": map[string]any{"list_name": "Synthetic list", "name_zh": " 资料原名 ", "name_en": " Original name ", "file_category": "Drawing", "stage_label": "Source label", "draft_due_stage": draft, "final_due_stage": final, "markets": map[string]string{"region_a": "required"}, "owner": " Owner ", "priority": " P1 ", "source_list": "Synthetic list", "source_version": "Rev 1"}}}, http.StatusCreated, &e)
	return e
}
func checklistBind(f *managedNamesFixture, l types.Library, r types.Entity, target string, modules ...string) {
	attrs := map[string]any{}
	if len(modules) > 0 {
		attrs["module_types"] = modules
	}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/relations", map[string]any{"subject_id": r.ID, "object_id": target, "relation_type": "applies_to", "attributes": attrs}, http.StatusCreated, nil)
}
func checklistFile(f *managedNamesFixture, l types.Library, owner, req, stage string, conditions ...types.ShelfCondition) {
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": "File for " + owner + req, "owner_id": owner, "requirement_id": req, "cloud_url": "https://example.invalid/manual", "stage": stage, "conditions": conditions}}}}, http.StatusCreated, nil)
}
func checklistRead(f *managedNamesFixture, l types.Library, id string) map[string]any {
	var detail map[string]any
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+id, nil, http.StatusOK, &detail)
	return detail["shelf"].(map[string]any)
}

// Medium spec, task 08 / ADR 0024: the configured stage order drives the
// four states; inherited stage and exact requirement metadata are observable.
func TestShelfChecklistFourStates(t *testing.T) {
	f, l, n := checklistSetup(t)
	var product types.Entity
	f.request(http.MethodPatch, "/entities/"+shelfNode(n, "product"), map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "draft_due"}}, http.StatusOK, &product)
	for _, test := range []struct{ name, draft, final, file, want string }{{"Delivered", "", "early", "final", "delivered"}, {"Draft", "", "final_due", "draft", "draft_only"}, {"Missing", "draft_due", "final_due", "", "missing"}, {"Later", "", "final_due", "", "not_due"}} {
		r := checklistRequirement(t, f, l, test.name, test.draft, test.final)
		checklistBind(f, l, r, shelfNode(n, "line"))
		if test.file != "" {
			checklistFile(f, l, shelfNode(n, "platform"), r.ID, test.file)
		}
	}
	shelf := checklistRead(f, l, shelfNode(n, "model"))
	require.Equal(t, "draft_due", shelf["current_stage"])
	items := shelf["checklist"].([]any)
	require.Len(t, items, 4)
	states := map[string]string{}
	for _, raw := range items {
		row := raw.(map[string]any)
		states[row["requirement"].(map[string]any)["canonical_name"].(string)] = row["status"].(string)
	}
	require.Equal(t, map[string]string{"Delivered": "delivered", "Draft": "draft_only", "Missing": "missing", "Later": "not_due"}, states)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+product.ID, nil, http.StatusOK, &detail)
	require.Equal(t, "draft_due", detail.Entity.Attributes["current_stage"])
}

// Medium spec, task 08: line and module requirements form a union, shared
// files use the nearest layer, and the model may override the product stage.
func TestShelfChecklistInheritanceModulesAndArchive(t *testing.T) {
	f, l, n := checklistSetup(t)
	product, model := shelfNode(n, "product"), shelfNode(n, "model")
	f.request(http.MethodPatch, "/entities/"+product, map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "early"}}, http.StatusOK, nil)
	r := checklistRequirement(t, f, l, "Shared requirement", "draft_due", "final_due")
	checklistBind(f, l, r, shelfNode(n, "line"))
	checklistBind(f, l, r, r.ID, "module_b")
	extra := checklistRequirement(t, f, l, "Module requirement", "", "final_due")
	checklistBind(f, l, extra, extra.ID, "module_b")
	for _, owner := range []string{shelfNode(n, "platform"), product, shelfNode(n, "fixed"), model} {
		stage := "final"
		if owner == model {
			stage = "draft"
		}
		checklistFile(f, l, owner, r.ID, stage)
		row := checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)
		// Requirements are sorted by name; select the shared one explicitly.
		for _, raw := range checklistRead(f, l, model)["checklist"].([]any) {
			if raw.(map[string]any)["requirement"].(map[string]any)["id"] == r.ID {
				row = raw.(map[string]any)
			}
		}
		require.Equal(t, owner, row["owner_id"])
	}
	shelf := checklistRead(f, l, model)
	require.Len(t, shelf["checklist"], 2, "line/module union must deduplicate")
	f.request(http.MethodPatch, "/entities/"+model, map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "final_due"}}, http.StatusOK, nil)
	shelf = checklistRead(f, l, model)
	require.Equal(t, "final_due", shelf["current_stage"])
	for _, raw := range shelf["checklist"].([]any) {
		require.Equal(t, "missing", raw.(map[string]any)["status"], "a draft is missing after the final deadline")
	}
	for _, key := range []string{"product", "platform", "line"} {
		summary := checklistRead(f, l, shelfNode(n, key))["checklist_summary"].(map[string]any)
		require.EqualValues(t, 2, summary["model_count"])
		require.EqualValues(t, 1, summary["models_with_gaps"])
	}
	var removal types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "removed", "type": "missing_entity", "entity_id": extra.ID, "revision": 1}}}, http.StatusCreated, &removal)
	require.Len(t, checklistRead(f, l, model)["checklist"], 2, "a missing row stays active until explicitly retired")
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+removal.ID+"/confirm", map[string]any{"revision": removal.Revision, "decisions": []any{map[string]any{"item_id": "removed", "action": "retire"}}}, http.StatusOK, nil)

	require.Len(t, checklistRead(f, l, model)["checklist"], 1)
}

// Medium spec, task 08: optional components cannot satisfy one another's
// requirements; gap filtering intersects the same combination as other filters.
func TestShelfChecklistConditionalGapsUseSameCombination(t *testing.T) {
	f, l, n := checklistSetup(t)
	model := shelfNode(n, "model")
	f.request(http.MethodPatch, "/entities/"+shelfNode(n, "product"), map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "final_due"}}, http.StatusOK, nil)
	r := checklistRequirement(t, f, l, "Conditional requirement", "", "final_due")
	checklistBind(f, l, r, shelfNode(n, "line"))
	checklistFile(f, l, shelfNode(n, "a"), r.ID, "final")
	var detail types.ShelfNodeDetail
	// The default view must include the missing alternative as well as the file.
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+model, nil, http.StatusOK, &detail)
	shelf := checklistRead(f, l, model)
	rows := shelf["checklist"].([]any)
	require.Len(t, rows, 2)
	got := map[string]string{}
	for _, raw := range rows {
		row := raw.(map[string]any)
		c := row["conditions"].([]any)
		require.Len(t, c, 1)
		part := c[0].(map[string]any)["part_ids"].([]any)[0].(string)
		got[part] = row["status"].(string)
		require.True(t, row["partial_options"].(bool))
	}
	require.Equal(t, map[string]string{shelfNode(n, "a"): "delivered", shelfNode(n, "b"): "missing"}, got)
	path := "/libraries/" + l.ID + "/shelf/query"
	var result types.ShelfQueryResult
	f.request(http.MethodPost, path, map[string]any{"document_gaps": true, "part_id": shelfNode(n, "a")}, http.StatusOK, &result)
	require.Zero(t, result.Total)
	f.request(http.MethodPost, path, map[string]any{"document_gaps": true, "part_id": shelfNode(n, "b")}, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.True(t, result.Items[0].PartialOptions)
	f.request(http.MethodPost, path, map[string]any{"document_gaps": true, "limit": 1}, http.StatusOK, &result)
	require.EqualValues(t, 2, result.Total)
	require.True(t, result.Truncated)
	// Nearer conditional draft wins over a farther final only where it applies.
	condition := types.ShelfCondition{ModuleType: "module_a", PartIDs: []string{shelfNode(n, "b")}}
	checklistFile(f, l, shelfNode(n, "platform"), r.ID, "final")
	checklistFile(f, l, model, r.ID, "draft", condition)
	require.Len(t, checklistRead(f, l, model)["checklist"], 2)
	f.request(http.MethodPost, path, map[string]any{"document_gaps": true, "part_id": shelfNode(n, "a")}, http.StatusOK, &result)
	require.Zero(t, result.Total)
	f.request(http.MethodPost, path, map[string]any{"document_gaps": true, "part_id": shelfNode(n, "b")}, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
}

// Medium spec, task 08: unknown stages show presence only, and clearing the
// override resumes inheritance. A replacement draft cannot reuse an old final.
func TestShelfChecklistUnstagedAndVersionReplacement(t *testing.T) {
	f, l, n := checklistSetup(t)
	r := checklistRequirement(t, f, l, "Presence requirement", "draft_due", "final_due")
	checklistBind(f, l, r, shelfNode(n, "line"))
	model := shelfNode(n, "model")
	row := checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)
	require.Equal(t, "no_file", row["status"])
	checklistFile(f, l, model, r.ID, "final")
	row = checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)
	require.Equal(t, "has_file", row["status"])
	f.request(http.MethodPatch, "/entities/"+shelfNode(n, "product"), map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "final_due"}}, http.StatusOK, nil)
	row = checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)
	require.Equal(t, "delivered", row["status"])
	checklistFile(f, l, model, r.ID, "draft")
	row = checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)
	require.Equal(t, "missing", row["status"])
	require.EqualValues(t, 2, row["document"].(map[string]any)["ordinal"])
	f.request(http.MethodPatch, "/entities/"+model, map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "early"}}, http.StatusOK, nil)
	require.Equal(t, "draft_only", checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)["status"])
	f.request(http.MethodPatch, "/entities/"+model, map[string]any{"revision": 2, "attributes": map[string]any{}}, http.StatusOK, nil)
	require.Equal(t, "final_due", checklistRead(f, l, model)["current_stage"])
	var detail types.ShelfNodeDetail
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+model+"?as_of=2000-01-01T00:00:00Z", nil, http.StatusOK, &detail)
	require.False(t, detail.Recorded)
	require.Nil(t, detail.Shelf)
}

// Medium spec, task 08: invalid requirements and applicability are conflicts
// before confirmation; exact source names and current list versions survive.
func TestShelfChecklistRejectsUncertainRequirements(t *testing.T) {
	f, l, n := checklistSetup(t)
	r := checklistRequirement(t, f, l, "Validation requirement", "draft_due", "final_due")
	for _, attrs := range []map[string]any{
		{"current_stage": "unconfigured"},
	} {
		f.request(http.MethodPatch, "/entities/"+shelfNode(n, "model"), map[string]any{"revision": 1, "attributes": attrs}, http.StatusBadRequest, nil)
	}
	for _, record := range []map[string]any{
		{"final_due_stage": "unconfigured"},
		{"final_due_stage": ""},
		{"draft_due_stage": "final_due", "final_due_stage": "early"},
		{"final_due_stage": "final_due", "markets": map[string]string{"other": "required"}},
		{"final_due_stage": "final_due", "markets": map[string]string{"region_a": "unclear"}},
	} {
		var draft types.ShelfChangeset
		f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "invalid", "type": "update_entity", "entity_id": r.ID, "patch": map[string]any{"revision": 1, "attributes": map[string]any{"requirement": record}}}}}, http.StatusCreated, &draft)
		require.NotEmpty(t, draft.Items[0].Conflicts)
		f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "invalid", "action": "apply"}}}, http.StatusConflict, nil)
	}
	for _, test := range []struct {
		target  string
		modules []string
	}{{r.ID, nil}, {r.ID, []string{"unknown"}}, {shelfNode(n, "line"), []string{"module_a"}}, {shelfNode(n, "model"), nil}} {
		attrs := map[string]any{}
		if test.modules != nil {
			attrs["module_types"] = test.modules
		}
		f.request(http.MethodPost, "/libraries/"+l.ID+"/relations", map[string]any{"subject_id": r.ID, "object_id": test.target, "relation_type": "applies_to", "attributes": attrs}, http.StatusBadRequest, nil)
	}
	var entity types.EntityDetail
	f.request(http.MethodGet, "/entities/"+r.ID, nil, http.StatusOK, &entity)
	record := entity.Entity.Attributes["requirement"].(map[string]any)
	require.Equal(t, " 资料原名 ", record["name_zh"])
	require.Equal(t, " Original name ", record["name_en"])
	require.Equal(t, "Rev 1", record["source_version"])
}

// Medium spec, task 08: two component owners at the same nearest layer must
// remain explicit candidates instead of an arbitrary choice by entity ID.
func TestShelfChecklistSameLayerFilesRemainCandidates(t *testing.T) {
	f, l, n := checklistSetup(t)
	r := checklistRequirement(t, f, l, "Multiple component files", "", "final_due")
	checklistBind(f, l, r, shelfNode(n, "line"))
	checklistFile(f, l, shelfNode(n, "a"), r.ID, "draft")
	checklistFile(f, l, shelfNode(n, "fixed"), r.ID, "final")
	rows := checklistRead(f, l, shelfNode(n, "model"))["checklist"].([]any)
	ambiguous := 0
	for _, raw := range rows {
		if raw.(map[string]any)["ambiguous"] == true {
			ambiguous++
		}
	}
	require.Equal(t, 2, ambiguous)
}

// Medium spec, task 08: a historical model view uses the latest effective list
// revision after confirmation, while an unconfirmed revision changes nothing.
func TestShelfChecklistUsesLatestConfirmedRequirement(t *testing.T) {
	f, l, n := checklistSetup(t)
	model := shelfNode(n, "model")
	f.request(http.MethodPatch, "/entities/"+model, map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "early"}}, http.StatusOK, nil)
	r := checklistRequirement(t, f, l, "Latest requirement", "", "early")
	checklistBind(f, l, r, shelfNode(n, "line"))
	require.Equal(t, "missing", checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)["status"])
	at := time.Now().UTC().Format(time.RFC3339Nano)
	var change types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "update", "type": "update_entity", "entity_id": r.ID, "patch": map[string]any{"revision": 1, "attributes": map[string]any{"requirement": map[string]any{"list_name": "Synthetic list", "final_due_stage": "final_due", "source_version": "Rev 2"}}}}}}, http.StatusCreated, &change)
	require.Equal(t, "missing", checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)["status"])
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+change.ID+"/confirm", map[string]any{"revision": change.Revision, "decisions": []any{map[string]any{"item_id": "update", "action": "apply"}}}, http.StatusOK, nil)
	var detail map[string]any
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+model+"?as_of="+url.QueryEscape(at), nil, http.StatusOK, &detail)
	row := detail["shelf"].(map[string]any)["checklist"].([]any)[0].(map[string]any)
	require.Equal(t, "not_due", row["status"])
	require.Equal(t, "Rev 2", row["requirement"].(map[string]any)["attributes"].(map[string]any)["requirement"].(map[string]any)["source_version"])
}

// Medium spec, task 08: changing the library stage order changes due status
// without changing stored stage keys or requiring any customer stage names.
func TestShelfChecklistUsesConfiguredStageOrder(t *testing.T) {
	f, l, n := checklistSetup(t)
	r := checklistRequirement(t, f, l, "Stage order requirement", "", "final_due")
	checklistBind(f, l, r, shelfNode(n, "line"))
	model := shelfNode(n, "model")
	f.request(http.MethodPatch, "/entities/"+model, map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "early"}}, http.StatusOK, nil)
	require.Equal(t, "not_due", checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)["status"])
	l.ShelfConfig.Stages = []types.ShelfOption{l.ShelfConfig.Stages[1], l.ShelfConfig.Stages[2], {Key: "early", Name: " Renamed final step "}}
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, nil)
	require.Equal(t, "missing", checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)["status"])
}

// Medium regression, task 08: removing a requirement's fields must not hide an
// active obligation. Retirement remains an explicit changeset decision.
func TestShelfChecklistRequirementMetadataCannotBeErased(t *testing.T) {
	f, l, _ := checklistSetup(t)
	r := checklistRequirement(t, f, l, "Protected metadata", "", "final_due")
	f.request(http.MethodPatch, "/entities/"+r.ID, map[string]any{"revision": 1, "attributes": map[string]any{}}, http.StatusBadRequest, nil)
}
