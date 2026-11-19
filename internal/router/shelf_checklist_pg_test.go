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

func checklistSetup(t *testing.T) (*managedNamesFixture, types.Library, types.ShelfChangeset) {
	t.Helper()
	f, l, n := shelfModelSetup(t)
	l.ShelfConfig.Markets = []types.ShelfOption{{Key: "market_a", Name: "Market A"}}
	l.ShelfConfig.Stages = []types.ShelfOption{{Key: "early", Name: "Early custom"}, {Key: "draft_due", Name: "Draft custom"}, {Key: "final_due", Name: "Final custom"}}
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, &l)
	return f, l, n
}
func checklistRequirement(t *testing.T, f *managedNamesFixture, l types.Library, name, draft, final string) types.Entity {
	t.Helper()
	var e types.Entity
	f.request(http.MethodPost, "/libraries/"+l.ID+"/entities", map[string]any{"name": name, "kind": "document_requirement", "attributes": map[string]any{"requirement": map[string]any{"list_name": "Synthetic list", "name_zh": " 资料原名 ", "name_en": " Original name ", "file_category": "Drawing", "stage_label": "Source label", "draft_due_stage": draft, "final_due_stage": final, "markets": map[string]string{"market_a": "required"}, "owner": " Owner ", "priority": " P1 ", "source_list": "Synthetic list", "source_version": "Rev 1"}}}, http.StatusCreated, &e)
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

func checklistMarketSetup(t *testing.T) (*managedNamesFixture, types.Library, types.ShelfChangeset) {
	t.Helper()
	f, l, n := checklistSetup(t)
	l.ShelfConfig.Regions = append(l.ShelfConfig.Regions, types.ShelfOption{Key: "gb", Name: "GB"}, types.ShelfOption{Key: "iec", Name: "IEC"}, types.ShelfOption{Key: "pending", Name: "Pending"})
	l.ShelfConfig.Markets = append(l.ShelfConfig.Markets, types.ShelfOption{Key: "domestic", Name: "国内"}, types.ShelfOption{Key: "overseas", Name: "海外"})
	l.ShelfConfig.ChecklistMarketsByRegion = map[string]string{"gb": "domestic", "iec": "overseas", "pending": "overseas"}
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, &l)
	return f, l, n
}

func checklistMarketRequirement(t *testing.T, f *managedNamesFixture, l types.Library, n types.ShelfChangeset, name string, markets map[string]string) types.Entity {
	t.Helper()
	var e types.Entity
	f.request(http.MethodPost, "/libraries/"+l.ID+"/entities", map[string]any{"name": name, "kind": "document_requirement", "attributes": map[string]any{"requirement": map[string]any{"list_name": "Synthetic markets", "final_due_stage": "final_due", "markets": markets}}}, http.StatusCreated, &e)
	checklistBind(f, l, e, shelfNode(n, "line"))
	return e
}

// Medium spec, shelf-checklist-markets task 03: active mapped regions select
// requirements; the strongest necessity wins and optional rows never have gaps.
func TestShelfChecklistMarketsSelectRequirements(t *testing.T) {
	for _, test := range []struct {
		name, gb, iec string
		markets       []any
		necessities   map[string]string
	}{
		{"domestic", "GA", "EOM", []any{"domestic"}, map[string]string{"Domestic": "required", "Optional": "optional", "Strongest": "optional", "Legacy": ""}},
		{"both", "DEV", "Planning", []any{"domestic", "overseas"}, map[string]string{"Domestic": "required", "Overseas": "required", "Optional": "optional", "Strongest": "required", "Legacy": ""}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, l, n := checklistMarketSetup(t)
			for _, r := range []struct {
				name    string
				markets map[string]string
			}{
				{"Domestic", map[string]string{"domestic": "required"}},
				{"Overseas", map[string]string{"overseas": "required"}},
				{"Optional", map[string]string{"domestic": "optional"}},
				{"Strongest", map[string]string{"domestic": "optional", "overseas": "required"}},
				{"Not needed", map[string]string{"domestic": "not_needed", "overseas": "not_needed"}},
				{"Other market", map[string]string{"market_a": "required"}},
				{"Legacy", map[string]string{}},
			} {
				checklistMarketRequirement(t, f, l, n, r.name, r.markets)
			}
			model := shelfNode(n, "model")
			f.request(http.MethodPatch, "/entities/"+model, map[string]any{"revision": 1, "attributes": map[string]any{"sale_statuses": []any{
				map[string]any{"region": "gb", "status": test.gb, "planned_eom": true},
				map[string]any{"region": "iec", "status": test.iec},
				map[string]any{"region": "pending", "status": "待定"},
				map[string]any{"region": "region_a", "status": "GA"},
			}}}, http.StatusOK, nil)
			shelf := checklistRead(f, l, model)
			require.Equal(t, test.markets, shelf["checklist_markets"])
			require.NotContains(t, shelf, "checklist_skipped_reason")
			rows := shelf["checklist"].([]any)
			require.Len(t, rows, len(test.necessities))
			for _, raw := range rows {
				row := raw.(map[string]any)
				name := row["requirement"].(map[string]any)["canonical_name"].(string)
				want, ok := test.necessities[name]
				require.True(t, ok, "unexpected requirement %s", name)
				if want == "" {
					require.NotContains(t, row, "necessity")
				} else {
					require.Equal(t, want, row["necessity"], name)
				}
				require.Equal(t, "missing", row["status"])
				require.Equal(t, want != "optional", row["gap"], name)
			}
		})
	}
}

// Medium spec, shelf-checklist-markets task 03: optional rows and skipped
// models have the same gap meaning through every public read surface.
func TestShelfChecklistMarketsGapsAgreeAcrossReads(t *testing.T) {
	for _, test := range []struct {
		name, necessity, file, stage, sale, status string
		gap                                        bool
	}{
		{"required", "required", "", "", "GA", "missing", true},
		{"optional missing", "optional", "", "", "GA", "missing", false},
		{"optional draft", "optional", "draft", "", "GA", "draft_only", false},
		{"optional overdue", "optional", "draft", "final_due", "GA", "missing", false},
		{"retired", "required", "", "", "EOM", "", false},
		{"no statuses", "required", "", "", "", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, l, n := checklistMarketSetup(t)
			r := checklistMarketRequirement(t, f, l, n, "Market requirement", map[string]string{"domestic": test.necessity})
			if test.status == "" {
				checklistMarketRequirement(t, f, l, n, "Legacy requirement", map[string]string{})
			}
			model, single := shelfNode(n, "model"), shelfNode(n, "single")
			for _, id := range []string{model, single} {
				sale := "EOM"
				if id == model {
					sale = test.sale
				}
				attrs := map[string]any{}
				if sale != "" {
					attrs["sale_statuses"] = []any{map[string]any{"region": "gb", "status": sale}, map[string]any{"region": "iec", "status": "EOM"}, map[string]any{"region": "pending", "status": "待定"}, map[string]any{"region": "region_a", "status": "GA"}}
				}
				f.request(http.MethodPatch, "/entities/"+id, map[string]any{"revision": 1, "attributes": attrs}, http.StatusOK, nil)
			}
			if test.stage != "" {
				f.request(http.MethodPatch, "/entities/"+shelfNode(n, "product"), map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": test.stage}}, http.StatusOK, nil)
			}
			if test.file != "" {
				checklistFile(f, l, model, r.ID, test.file)
			}
			shelf := checklistRead(f, l, model)
			require.Equal(t, test.gap, shelf["has_gaps"])
			rows := shelf["checklist"].([]any)
			if test.status == "" {
				require.Empty(t, rows)
				require.Equal(t, []any{}, shelf["checklist_markets"])
				require.Equal(t, "no_active_market", shelf["checklist_skipped_reason"])
			} else {
				require.Len(t, rows, 1)
				row := rows[0].(map[string]any)
				require.Equal(t, test.necessity, row["necessity"])
				require.Equal(t, test.status, row["status"])
				require.Equal(t, test.gap, row["gap"])
			}
			count := 0
			if test.gap {
				count = 1
			}
			for _, key := range []string{"product", "platform", "line"} {
				summary := checklistRead(f, l, shelfNode(n, key))["checklist_summary"].(map[string]any)
				require.EqualValues(t, 2, summary["model_count"])
				require.EqualValues(t, count, summary["models_with_gaps"])
			}
			var result types.ShelfQueryResult
			f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/query", map[string]any{"document_gaps": true, "detail": "full"}, http.StatusOK, &result)
			require.EqualValues(t, count, result.Total)
			if test.gap {
				require.Equal(t, model, result.Items[0].Entity.ID)
				require.Equal(t, "required", result.Items[0].Shelf.Checklist[0].Necessity)
			}
			client := f.connect(t, "2025-11-25")
			for _, key := range []string{"model", "product", "platform", "line"} {
				var guide guideGapResult
				mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID, "node_id": shelfNode(n, key)}, &guide)
				if key == "model" {
					require.EqualValues(t, 1, guide.Gaps.ModelCount)
				} else {
					require.EqualValues(t, 2, guide.Gaps.ModelCount)
				}
				require.EqualValues(t, count, guide.Gaps.Documents.Total)
				require.Zero(t, guide.Gaps.NoFileDocuments.Total)
				if test.gap {
					require.Equal(t, model, guide.Gaps.Documents.Items[0].ModelID)
					require.True(t, guide.Gaps.Documents.Items[0].Gap)
				}
			}
		})
	}
}

// Medium spec, shelf-checklist-markets task 03: no mapping retains the full
// checklist, even for retired models and optional/not-needed market metadata.
func TestShelfChecklistMarketsUnconfiguredPreservesChecklist(t *testing.T) {
	f, l, n := checklistSetup(t)
	for _, value := range []string{"optional", "not_needed"} {
		checklistMarketRequirement(t, f, l, n, value, map[string]string{"market_a": value})
	}
	f.request(http.MethodPatch, "/entities/"+shelfNode(n, "model"), map[string]any{"revision": 1, "attributes": map[string]any{"sale_statuses": []any{map[string]any{"region": "region_a", "status": "EOM"}}}}, http.StatusOK, nil)
	shelf := checklistRead(f, l, shelfNode(n, "model"))
	require.NotContains(t, shelf, "checklist_markets")
	require.NotContains(t, shelf, "checklist_skipped_reason")
	require.Equal(t, true, shelf["has_gaps"])
	rows := shelf["checklist"].([]any)
	require.Len(t, rows, 2)
	for _, raw := range rows {
		row := raw.(map[string]any)
		require.NotContains(t, row, "necessity")
		require.Equal(t, true, row["gap"])
	}
}

// Medium spec, shelf-checklist-markets task 03: business time selects the sale
// status snapshot, so retiring today does not remove yesterday's obligations.
func TestShelfChecklistMarketsUseHistoricalSaleStatuses(t *testing.T) {
	f, l, n := checklistMarketSetup(t)
	checklistMarketRequirement(t, f, l, n, "Domestic", map[string]string{"domestic": "required"})
	model := shelfNode(n, "model")
	f.request(http.MethodPatch, "/entities/"+model, map[string]any{"revision": 1, "attributes": map[string]any{"sale_statuses": []any{map[string]any{"region": "gb", "status": "GA"}}}}, http.StatusOK, nil)
	asOf := time.Now().UTC().Format(time.RFC3339Nano)
	f.request(http.MethodPatch, "/entities/"+model, map[string]any{"revision": 2, "attributes": map[string]any{"sale_statuses": []any{map[string]any{"region": "gb", "status": "EOM"}}}}, http.StatusOK, nil)
	require.Equal(t, "no_active_market", checklistRead(f, l, model)["checklist_skipped_reason"])
	var detail map[string]any
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+model+"?as_of="+url.QueryEscape(asOf), nil, http.StatusOK, &detail)
	shelf := detail["shelf"].(map[string]any)
	require.Equal(t, []any{"domestic"}, shelf["checklist_markets"])
	require.Len(t, shelf["checklist"], 1)
	require.Equal(t, true, shelf["has_gaps"])
	var result types.ShelfQueryResult
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/query", map[string]any{"as_of": asOf, "document_gaps": true}, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.Equal(t, model, result.Items[0].Entity.ID)
}

// Medium spec, shelf-checklist-markets task 03: the default view unions markets;
// a selected combination admits only sale statuses satisfying all conditions.
func TestShelfChecklistMarketsRespectCombination(t *testing.T) {
	f, l, n := checklistMarketSetup(t)
	checklistMarketRequirement(t, f, l, n, "Domestic", map[string]string{"domestic": "required"})
	checklistMarketRequirement(t, f, l, n, "Overseas", map[string]string{"overseas": "required"})
	model := shelfNode(n, "model")
	f.request(http.MethodPatch, "/entities/"+model, map[string]any{"revision": 1, "attributes": map[string]any{"sale_statuses": []any{
		map[string]any{"region": "gb", "status": "GA", "conditions": []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(n, "a")}}, {ModuleType: "module_b", PartIDs: []string{shelfNode(n, "fixed")}}}},
		map[string]any{"region": "iec", "status": "DEV", "conditions": []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(n, "b")}}}},
	}}}, http.StatusOK, nil)
	shelf := checklistRead(f, l, model)
	require.Equal(t, []any{"domestic", "overseas"}, shelf["checklist_markets"])
	require.Len(t, shelf["checklist"], 2)
	for _, test := range []struct{ part, market, name string }{{"a", "domestic", "Domestic"}, {"b", "overseas", "Overseas"}} {
		combination, err := json.Marshal(map[string]string{"module_a": shelfNode(n, test.part)})
		require.NoError(t, err)
		var detail map[string]any
		f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+model+"?combination="+url.QueryEscape(string(combination)), nil, http.StatusOK, &detail)
		shelf = detail["shelf"].(map[string]any)
		require.Equal(t, []any{test.market}, shelf["checklist_markets"])
		rows := shelf["checklist"].([]any)
		require.Len(t, rows, 1)
		require.Equal(t, test.name, rows[0].(map[string]any)["requirement"].(map[string]any)["canonical_name"])
	}
}

// Medium spec, task 08 and shelf-checklist-markets task 01 / ADR 0024: configured
// stage order drives four states; only missing rows have a gap at a set stage.
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
	gaps := map[string]bool{}
	for _, raw := range items {
		row := raw.(map[string]any)
		name := row["requirement"].(map[string]any)["canonical_name"].(string)
		states[name] = row["status"].(string)
		gap, ok := row["gap"].(bool)
		require.True(t, ok, "gap must be a boolean, including when false")
		gaps[name] = gap
	}
	require.Equal(t, map[string]string{"Delivered": "delivered", "Draft": "draft_only", "Missing": "missing", "Later": "not_due"}, states)
	require.Equal(t, map[string]bool{"Delivered": false, "Draft": false, "Missing": true, "Later": false}, gaps)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+product.ID, nil, http.StatusOK, &detail)
	require.Equal(t, "draft_due", detail.Entity.Attributes["current_stage"])
}

// Medium spec, tasks 08 and 30: product-line requirements share nearest-layer
// files, and the model may override the product stage.
func TestShelfChecklistInheritanceAndArchive(t *testing.T) {
	f, l, n := checklistSetup(t)
	product, model := shelfNode(n, "product"), shelfNode(n, "model")
	f.request(http.MethodPatch, "/entities/"+product, map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "early"}}, http.StatusOK, nil)
	r := checklistRequirement(t, f, l, "Shared requirement", "draft_due", "final_due")
	checklistBind(f, l, r, shelfNode(n, "line"))
	extra := checklistRequirement(t, f, l, "Extra requirement", "", "final_due")
	checklistBind(f, l, extra, shelfNode(n, "line"))
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
	require.Len(t, shelf["checklist"], 2)
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

// Medium spec, shelf-checklist-markets task 01 / ADR 0024: unstaged models
// require a current final; replacing it with a draft reopens the gap.
func TestShelfChecklistUnstagedAndVersionReplacement(t *testing.T) {
	f, l, n := checklistSetup(t)
	r := checklistRequirement(t, f, l, "Presence requirement", "draft_due", "final_due")
	checklistBind(f, l, r, shelfNode(n, "line"))
	model := shelfNode(n, "model")
	row := checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)
	require.Equal(t, "missing", row["status"])
	require.Equal(t, true, row["gap"])
	checklistFile(f, l, model, r.ID, "final")
	row = checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)
	require.Equal(t, "delivered", row["status"])
	require.Equal(t, false, row["gap"])
	checklistFile(f, l, model, r.ID, "draft")
	row = checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)
	require.Equal(t, "draft_only", row["status"])
	require.Equal(t, true, row["gap"])
	f.request(http.MethodPatch, "/entities/"+shelfNode(n, "product"), map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "final_due"}}, http.StatusOK, nil)
	row = checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)
	require.Equal(t, "missing", row["status"])
	require.Equal(t, true, row["gap"])
	require.EqualValues(t, 2, row["document"].(map[string]any)["ordinal"])
	f.request(http.MethodPatch, "/entities/"+model, map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "early"}}, http.StatusOK, nil)
	row = checklistRead(f, l, model)["checklist"].([]any)[0].(map[string]any)
	require.Equal(t, "draft_only", row["status"])
	require.Equal(t, false, row["gap"])
	f.request(http.MethodPatch, "/entities/"+model, map[string]any{"revision": 2, "attributes": map[string]any{}}, http.StatusOK, nil)
	require.Equal(t, "final_due", checklistRead(f, l, model)["current_stage"])
	var detail types.ShelfNodeDetail
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+model+"?as_of=2000-01-01T00:00:00Z", nil, http.StatusOK, &detail)
	require.False(t, detail.Recorded)
	require.Nil(t, detail.Shelf)
}

// Medium spec, shelf-checklist-markets task 01: node details, ancestor summaries,
// query filters and the guide's HTTP MCP response agree for all unstaged states.
func TestShelfChecklistUnstagedGapsAgreeAcrossReads(t *testing.T) {
	for _, test := range []struct {
		status, label, file string
		gap                 bool
		modelsWithGaps      int
	}{
		{"missing", "缺", "", true, 1},
		{"draft_only", "只有初稿", "draft", true, 1},
		{"delivered", "已交", "final", false, 0},
	} {
		t.Run(test.status, func(t *testing.T) {
			f, l, n := checklistSetup(t)
			r := checklistRequirement(t, f, l, "Unstaged requirement", "draft_due", "final_due")
			checklistBind(f, l, r, shelfNode(n, "line"))
			model, single := shelfNode(n, "model"), shelfNode(n, "single")
			checklistFile(f, l, single, r.ID, "final")
			client := f.connect(t, "2025-11-25")
			if test.file != "" {
				checklistFile(f, l, model, r.ID, test.file)
			}
			shelf := checklistRead(f, l, model)
			require.Empty(t, shelf["current_stage"])
			require.Equal(t, test.gap, shelf["has_gaps"])
			rows := shelf["checklist"].([]any)
			require.Len(t, rows, 1)
			row := rows[0].(map[string]any)
			require.Equal(t, test.status, row["status"])
			require.Equal(t, test.label, row["status_label"])
			require.Equal(t, test.gap, row["gap"])
			for _, key := range []string{"product", "platform", "line"} {
				summary := checklistRead(f, l, shelfNode(n, key))["checklist_summary"].(map[string]any)
				require.EqualValues(t, 2, summary["model_count"])
				require.EqualValues(t, test.modelsWithGaps, summary["models_with_gaps"])
				for _, raw := range summary["models"].([]any) {
					item := raw.(map[string]any)
					if item["model_id"] == model {
						require.Equal(t, test.gap, item["has_gaps"])
					}
				}
			}
			var result types.ShelfQueryResult
			f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/query", map[string]any{"document_gaps": true, "detail": "full"}, http.StatusOK, &result)
			require.EqualValues(t, test.modelsWithGaps, result.Total)
			if test.gap {
				require.Len(t, result.Items, 1)
				require.Equal(t, model, result.Items[0].Entity.ID)
				require.True(t, result.Items[0].Shelf.HasGaps)
				require.Len(t, result.Items[0].Shelf.Checklist, 1)
				require.True(t, result.Items[0].Shelf.Checklist[0].Gap)
			}
			for _, key := range []string{"model", "product", "platform", "line"} {
				var guide guideGapResult
				mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID, "node_id": shelfNode(n, key)}, &guide)
				require.Zero(t, guide.Gaps.NoFileDocuments.Total)
				require.EqualValues(t, test.modelsWithGaps, guide.Gaps.Documents.Total)
				for _, item := range guide.Gaps.Documents.Items {
					require.Equal(t, model, item.ModelID)
					require.Equal(t, test.status, item.Status)
					require.True(t, item.Gap)
				}
			}
		})
	}
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
		{"final_due_stage": "final_due", "markets": map[string]string{"market_a": "unclear"}},
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

// A certification report linked to a requirement counts at the certification
// owner's level and keeps the certification's cell part as a condition, so the
// checklist recognizes every saved requirement link.
func TestShelfChecklistCountsCertificationFiles(t *testing.T) {
	f, l, n := checklistSetup(t)
	l.ShelfConfig.Standards = []types.ShelfStandard{{Key: "standard_a", Name: "Synthetic Standard"}}
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, &l)
	model := shelfNode(n, "model")
	f.request(http.MethodPatch, "/entities/"+shelfNode(n, "product"), map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "final_due"}}, http.StatusOK, nil)
	r := checklistRequirement(t, f, l, "Certification report", "", "final_due")
	checklistBind(f, l, r, shelfNode(n, "line"))
	path := "/libraries/" + l.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Cell A certificate", "owner_id": model, "standard": "standard_a", "certification_object": "Container", "cell_part_id": shelfNode(n, "a"), "region": "region_a", "number": "CERT-A"}}}}, http.StatusCreated, &draft)
	cert := draft.Items[0].EntityID
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": "Certificate report", "owner_id": cert, "certificate_number": "CERT-A", "requirement_id": r.ID, "cloud_url": "https://example.invalid/report", "stage": "final"}}}}, http.StatusCreated, nil)
	got := map[string]string{}
	for _, raw := range checklistRead(f, l, model)["checklist"].([]any) {
		row := raw.(map[string]any)
		c := row["conditions"].([]any)
		require.Len(t, c, 1)
		got[c[0].(map[string]any)["part_ids"].([]any)[0].(string)] = row["status"].(string)
		if row["status"] == "delivered" {
			require.Equal(t, cert, row["owner_id"])
			require.Equal(t, "self", row["origin"])
		}
	}
	require.Equal(t, map[string]string{shelfNode(n, "a"): "delivered", shelfNode(n, "b"): "missing"}, got)
	var result types.ShelfQueryResult
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/query", map[string]any{"document_gaps": true, "part_id": shelfNode(n, "a")}, http.StatusOK, &result)
	require.Zero(t, result.Total)
	// The other model does not contain cell A, so the report does not apply to it.
	rows := checklistRead(f, l, shelfNode(n, "single"))["checklist"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, "missing", rows[0].(map[string]any)["status"])
}

// Regression: restored data once carried an unknown requirement field, and the
// checklist silently dropped every such requirement. Reads now fail and name
// the requirement so the data can be corrected.
func TestShelfChecklistRejectsUnreadableRequirement(t *testing.T) {
	f, l, n := checklistSetup(t)
	r := checklistRequirement(t, f, l, "Malformed requirement", "", "final_due")
	checklistBind(f, l, r, shelfNode(n, "line"))
	require.Len(t, checklistRead(f, l, shelfNode(n, "model"))["checklist"], 1)
	require.NoError(t, f.db.Exec(`UPDATE entity_attribute_versions SET attributes=jsonb_set(attributes,'{requirement,source_row}','2') WHERE entity_id=?`, r.ID).Error)
	body := f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+shelfNode(n, "model"), nil, http.StatusConflict, nil)
	require.Contains(t, string(body), "Malformed requirement")
	require.Contains(t, string(body), "source_row")
}
