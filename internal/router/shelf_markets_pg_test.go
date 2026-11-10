package router

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Medium spec, task 30: configured ATQ markets are independent of sale regions.
func TestShelfMarketsAreIndependentOfSaleRegions(t *testing.T) {
	f, l, _ := checklistSetup(t)
	config := map[string]any{"regions": l.ShelfConfig.Regions, "module_types": l.ShelfConfig.ModuleTypes, "stages": l.ShelfConfig.Stages, "markets": []any{map[string]string{"key": "overseas", "name": " Overseas original "}}}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]string{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "settings", "type": "configure_shelf", "library_updated_at": l.UpdatedAt, "library": map[string]any{"shelf_config": config}}}}, http.StatusCreated, nil)
	var saved map[string]any
	f.request(http.MethodGet, "/libraries/"+l.ID, nil, http.StatusOK, &saved)
	require.Equal(t, []any{map[string]any{"key": "overseas", "name": " Overseas original "}}, saved["shelf_config"].(map[string]any)["markets"])
	for _, sample := range []struct {
		market string
		status int
	}{{"overseas", http.StatusCreated}, {"region_a", http.StatusBadRequest}} {
		f.request(http.MethodPost, "/libraries/"+l.ID+"/entities", map[string]any{"kind": "document_requirement", "name": sample.market, "attributes": map[string]any{"requirement": map[string]any{"list_name": "Synthetic", "final_due_stage": "final_due", "markets": map[string]string{sample.market: "required"}}}}, sample.status, nil)
	}
}

// Medium spec, task 30: unused markets can be removed without orphaning requirements.
func TestShelfMarketsCanRemoveUnusedButRetainReferencedKeys(t *testing.T) {
	f, l, _ := checklistSetup(t)
	l.ShelfConfig.Markets = append(l.ShelfConfig.Markets, types.ShelfOption{Key: "unused", Name: "Unused"})
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, &l)
	l.ShelfConfig.Markets = l.ShelfConfig.Markets[:1]
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, &l)
	r := checklistRequirement(t, f, l, "Market requirement", "", "final_due")
	l.ShelfConfig.Markets = []types.ShelfOption{}
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusBadRequest, nil)
	attrs := map[string]any{"requirement": map[string]any{"list_name": "Synthetic list", "final_due_stage": "final_due"}}
	f.request(http.MethodPatch, "/entities/"+r.ID, map[string]any{"revision": r.Revision, "attributes": attrs}, http.StatusOK, nil)
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusBadRequest, nil)
}

// Medium spec, task 30: a saved plan still requires its configured market.
func TestShelfMarketsRetainImportPlanReferences(t *testing.T) {
	f, l, n := checklistSetup(t)
	importRecordPlan(f, l, "atq", []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}, {Column: "C", Field: "market", Key: "market_a"}}, map[string]any{"product_line_id": shelfNode(n, "line")})
	l.ShelfConfig.Markets = []types.ShelfOption{}
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusBadRequest, nil)
}

// Medium spec, task 30: legacy bindings are retained as stored history but
// never imply product-line obligations. The DB seed represents an old backup.
func TestShelfLegacyModuleBindingsDoNotCreateChecklistItems(t *testing.T) {
	f, l, n := checklistSetup(t)
	r := checklistRequirement(t, f, l, "Legacy module requirement", "", "final_due")
	require.NoError(t, f.db.Create(&types.Relation{ID: "synthetic-legacy-binding", SpaceID: l.SpaceID, LibraryID: l.ID, SubjectID: r.ID, ObjectID: r.ID, RelationType: "applies_to", Attributes: map[string]any{"module_types": []string{"module_a"}}, Version: 1, ValidFrom: time.Now().Add(-time.Minute)}).Error)
	require.NoError(t, f.db.Exec(`UPDATE library_relation_types SET object_kinds='["product_line","document_requirement"]', fields='[{"key":"module_types","name":"Legacy modules","type":"list","kinds":[]}]' WHERE library_id=? AND name='applies_to'`, l.ID).Error)
	require.Empty(t, checklistRead(f, l, shelfNode(n, "model"))["checklist"])
	require.Empty(t, checklistRead(f, l, shelfNode(n, "a"))["checklist"])
	l.ShelfConfig.Markets = append(l.ShelfConfig.Markets, types.ShelfOption{Key: "new_market", Name: "New market"})
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, nil)
	f.request(http.MethodPost, "/libraries/"+l.ID+"/relations", map[string]any{"subject_id": r.ID, "object_id": r.ID, "relation_type": "applies_to", "attributes": map[string]any{"module_types": []string{"module_a"}}}, http.StatusBadRequest, nil)
}

// Medium spec, task 30: plans reject unknown markets, meanings and obsolete
// module bindings while preserving the distinct-field contract outside markets.
func TestShelfATQPlanRejectsInvalidMarketAndModuleMappings(t *testing.T) {
	f, l, n := checklistSetup(t)
	for _, sample := range []struct {
		name, market, raw, value, module string
		columns                          []types.ShelfImportColumn
	}{
		{name: "region", market: "region_a"},
		{name: "unknown", market: "unknown"},
		{name: "blank", market: "market_a", raw: "", value: "required"},
		{name: "meaning", market: "market_a", raw: "Mandatory", value: "unclear"},
		{name: "module", market: "market_a", module: "module_a"},
		{name: "duplicate_field", columns: []types.ShelfImportColumn{{Column: "C", Field: "owner"}, {Column: "D", Field: "owner"}}},
		{name: "duplicate_column", columns: []types.ShelfImportColumn{{Column: "C", Field: "market", Key: "market_a"}, {Column: "C", Field: "market", Key: "market_a"}}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			fixture := *f
			fixture.t = t
			columns := []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}}
			if sample.columns != nil {
				columns = append(columns, sample.columns...)
			} else {
				columns = append(columns, types.ShelfImportColumn{Column: "C", Field: "market", Key: sample.market})
			}
			definition := types.ShelfImportDefinition{Sheets: []types.ShelfImportSheet{{Worksheet: "Map", ProductLineID: shelfNode(n, "line"), ModuleType: sample.module, HeaderRows: 2, Columns: columns}}}
			if sample.value != "" {
				definition.MarketValues = map[string]string{sample.raw: sample.value}
			}
			plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic ATQ", FileKind: "atq", ShelfImportDefinition: definition}
			var draft types.ShelfChangeset
			fixture.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, &draft)
			require.NotEmpty(t, draft.Items[0].Conflicts)
			fixture.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: "plan", Action: "apply"}}}, http.StatusConflict, nil)
		})
	}
}

// Medium spec, task 30: module binding is no longer an accepted write.
func TestShelfRequirementsOnlyBindProductLines(t *testing.T) {
	f, l, n := checklistSetup(t)
	r := checklistRequirement(t, f, l, "Synthetic requirement", "", "final_due")
	f.request(http.MethodPost, "/libraries/"+l.ID+"/relations", map[string]any{"subject_id": r.ID, "object_id": r.ID, "relation_type": "applies_to", "attributes": map[string]any{"module_types": []string{"module_a"}}}, http.StatusBadRequest, nil)
	checklistBind(f, l, r, shelfNode(n, "line"))
	require.Len(t, checklistRead(f, l, shelfNode(n, "model"))["checklist"], 1)
}

// Medium spec, task 30: provenance snapshots the authenticated key name and
// identical uploads continue to return the original batch and its source.
func TestShelfImportRecordsAPIKeyName(t *testing.T) {
	f, l, n := checklistSetup(t)
	importRecordPlan(f, l, "atq", []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}}, map[string]any{"product_line_id": shelfNode(n, "line")})
	data := importWorkbook(t, importRecordRow(3, "Synthetic document", "Final custom"), "")
	draft := importUpload(t, f, l.ID, "atq_20261028.xlsx", "", data, http.StatusCreated)
	require.Equal(t, "personal integration", draft.Source.AgentName)
	f.key = ""
	reused := importUpload(t, f, l.ID, "atq_20261028.xlsx", "", data, http.StatusCreated)
	require.Equal(t, draft.ID, reused.ID)
	require.Equal(t, draft.Source, reused.Source)
	web := importUpload(t, f, l.ID, "atq_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Web document", "Final custom"), ""), http.StatusCreated)
	require.Empty(t, web.Source.AgentName)
}

// Medium spec, task 30: many source columns may name one market; every column
// must parse explicitly and all normalized meanings must agree.
func TestShelfATQMarketColumnsRequireAgreement(t *testing.T) {
	for _, sample := range []struct{ name, left, right, want string }{
		{"mapped", "Mandatory", "√", "required"},
		{"numeric", "1", "必需", "required"},
		{"builtin", "可选", "optional", "optional"},
		{"disagree", "Mandatory", "不需要", ""},
		{"unknown_first", "mystery", "必需", ""},
		{"blank_first", "", "必需", ""},
	} {
		t.Run(sample.name, func(t *testing.T) {
			f, l, n := checklistSetup(t)
			l.ShelfConfig.Markets = []types.ShelfOption{{Key: "overseas", Name: "Overseas"}}
			f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, &l)
			plan := map[string]any{"key": "map", "name": "Synthetic ATQ", "file_kind": "atq", "market_values": map[string]string{"Mandatory": "required", "√": "required", "1": "required"}, "sheets": []any{map[string]any{"worksheet": "Map", "header_rows": 2, "product_line_id": shelfNode(n, "line"), "columns": []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}, {Column: "C", Field: "market", Key: "overseas"}, {Column: "D", Field: "market", Key: "overseas"}}}}}
			f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]string{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "plan", "type": "save_import_plan", "import_plan": plan}}}, http.StatusCreated, nil)
			draft := importUpload(t, f, l.ID, "atq_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Synthetic drawing", "Final custom", sample.left, sample.right), ""), http.StatusCreated)
			if sample.want == "" {
				require.Len(t, draft.Items, 1)
				require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
				require.Equal(t, sample.left, draft.Items[0].Operation.ImportLocation.Values["C3"])
				require.Equal(t, sample.right, draft.Items[0].Operation.ImportLocation.Values["D3"])
			} else {
				confirmed := confirmImport(t, f, l.ID, draft)
				var detail types.EntityDetail
				f.request(http.MethodGet, "/entities/"+confirmed.Items[0].EntityID, nil, http.StatusOK, &detail)
				require.Equal(t, sample.want, detail.Entity.Attributes["requirement"].(map[string]any)["markets"].(map[string]any)["overseas"])
			}
		})
	}
}
