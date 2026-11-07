package router

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// The workbook is entirely synthetic. It replaces the customer Excel at the
// file boundary and retains merges, two headers, rich strings and yellow fills.
func importWorkbook(t *testing.T, rows, merges string, styleXML ...string) []byte {
	t.Helper()
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	styles := `<styleSheet><fills><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FFFFFF00"/></patternFill></fill></fills><cellXfs><xf fillId="0"/><xf fillId="1"/></cellXfs></styleSheet>`
	if len(styleXML) > 0 {
		styles = styleXML[0]
	}
	for _, file := range []struct{ name, content string }{
		{"xl/workbook.xml", `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Map" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`},
		{"xl/styles.xml", styles},
		{"xl/sharedStrings.xml", `<sst><si><r><t>Shared </t></r><r><t>platform</t></r></si></sst>`},
		{"xl/worksheets/sheet1.xml", `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>Header group</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>Header</t></is></c></row>` + rows + `</sheetData><mergeCells>` + merges + `</mergeCells></worksheet>`},
	} {
		w, err := z.Create(file.name)
		require.NoError(t, err)
		_, err = w.Write([]byte(file.content))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	return data.Bytes()
}

func importRow(row int, platform, product, model, status, energy string) string {
	return fmt.Sprintf(`<row r="%d"><c r="A%d" t="inlineStr"><is><t>%s</t></is></c><c r="B%d" t="inlineStr"><is><t>%s</t></is></c><c r="C%d" t="inlineStr"><is><t>%s</t></is></c><c r="D%d" t="inlineStr"><is><t>%s</t></is></c><c r="E%d"><v>%s</v></c></row>`, row, row, platform, row, product, row, model, row, status, row, energy)
}

func importUpload(t *testing.T, f *managedNamesFixture, libraryID, filename, date string, data []byte, status int) types.ShelfChangeset {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	require.NoError(t, form.WriteField("plan_key", "map"))
	if date != "" {
		require.NoError(t, form.WriteField("file_date", date))
	}
	part, err := form.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, form.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/libraries/"+libraryID+"/shelf/imports", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-API-Key", f.key)
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	require.Equal(t, status, response.Code, response.Body.String())
	var result struct{ Data types.ShelfChangeset }
	if status == http.StatusCreated {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	}
	return result.Data
}

func importSetup(t *testing.T) (*managedNamesFixture, types.Library) {
	f, library, nodes := shelfModelSetup(t)
	fields := append(library.EntitySchema.Fields, types.EntityFieldDefinition{Key: "energy", Name: "Energy", Type: "number", Kinds: []string{"model"}, ProductLineIDs: []string{shelfNode(nodes, "line")}})
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_schema": types.EntitySchema{Kinds: library.EntitySchema.Kinds, Fields: fields}}, http.StatusOK, &library)
	plan := map[string]any{"key": "map", "name": "Synthetic map", "file_kind": "product_map", "model_separators": []string{";"}, "missing_values": []string{"型号待定", "待定"}, "status_values": map[string]any{"GA (Demo)": map[string]any{"status": "GA", "notes": "Demo"}}, "fill_meanings": map[string]string{"FFFFFF00": "planned_eom"}, "sheets": []any{map[string]any{
		"worksheet": "Map", "product_line_id": shelfNode(nodes, "line"), "header_rows": 2, "columns": []any{map[string]any{"column": "A", "field": "platform"}, map[string]any{"column": "B", "field": "product"}, map[string]any{"column": "C", "field": "model"}, map[string]any{"column": "D", "field": "sale_status", "key": "region_a"}, map[string]any{"column": "E", "field": "parameter", "key": "energy"}},
	}}}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "plan", "type": "save_import_plan", "import_plan": plan}}}, http.StatusCreated, nil)
	return f, library
}

// Medium spec, task 11: repeated uploads reuse the same pending batch. Only
// confirmation makes split models, historical statuses and parameters visible.
func TestShelfImportMapIsDeterministicAndUsesFileDate(t *testing.T) {
	f, library := importSetup(t)
	data := importWorkbook(t, importRow(3, "Map platform", "Map product", "Model A;Model B", "GA (Demo)", "9~54"), "")
	draft := importUpload(t, f, library.ID, "map_20261028.xlsx", "", data, http.StatusCreated)
	repeated := importUpload(t, f, library.ID, "map_20261028.xlsx", "", data, http.StatusCreated)
	require.Equal(t, draft, repeated)
	require.Equal(t, "pending", draft.Status)
	require.Equal(t, "2026-10-28", draft.Source.FileDate.Format("2006-01-02"))
	decisions := []types.ShelfDecision{}
	for _, item := range draft.Items {
		for _, c := range item.Conflicts {
			require.Contains(t, []string{"new", "modified"}, c.Type, c.Message)
		}
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: "apply"})
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, &draft)
	models := []types.Entity{}
	for _, item := range draft.Items {
		if item.Operation.Entity != nil && item.Operation.Entity.Kind == "model" {
			var detail types.EntityDetail
			f.request(http.MethodGet, "/entities/"+item.EntityID, nil, http.StatusOK, &detail)
			models = append(models, detail.Entity)
			require.Equal(t, "GA", detail.Shelf.SaleStatuses[0].Status)
			require.Equal(t, "Demo", detail.Shelf.SaleStatuses[0].Notes)
			require.Equal(t, map[string]any{"min": float64(9), "max": float64(54)}, detail.Shelf.Parameters["energy"][0].Value)
		}
	}
	require.Len(t, models, 2)
	require.Equal(t, "Model A", models[0].CanonicalName)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+models[0].ID+"?as_of=2026-10-27T00:00:00Z", nil, http.StatusOK, &detail)
	require.Empty(t, detail.Shelf.SaleStatuses)
}

// Medium spec, task 11: structural table features and ambiguous rows retain
// explicit review conflicts, while mandatory placeholders never become models.
func TestShelfImportMapMergedCellsAndAmbiguousRows(t *testing.T) {
	f, library := importSetup(t)
	rows := strings.Replace(importRow(3, "Shared platform", "Merged product", "Valid A", "GA", "12"), `<c r="A3" t="inlineStr"><is><t>Shared platform</t></is></c>`, `<c r="A3" t="s"><v>0</v></c>`, 1)
	rows += importRow(4, "", "", "Valid B", "DEV", "24")
	rows += importRow(5, "Shared platform", "Merged product", "型号待定", "GA", "1")
	rows += importRow(6, "Grouping title", "", "", "", "")
	rows += importRow(7, "Other platform", "Merged product", "Ambiguous C", "GA", "1")
	rows += importRow(8, "Shared platform", "Merged product", "Blank region", "", "1")
	rows += strings.Replace(importRow(9, "Shared platform", "Merged product", "Yellow model", "GA", "1"), `r="C9" t=`, `r="C9" s="1" t=`, 1)
	rows += importRow(10, "Shared platform", "Merged product", "Valid A", "GA", "12")
	draft := importUpload(t, f, library.ID, "map_20261028.xlsx", "", importWorkbook(t, rows, `<mergeCell ref="A3:A4"/><mergeCell ref="B3:B4"/>`), http.StatusCreated)
	conflicts := map[string]int{}
	models := map[string]string{}
	decisions := []types.ShelfDecision{}
	for _, item := range draft.Items {
		if item.Operation.Entity != nil && item.Operation.Entity.Name != "" {
			models[item.Operation.Entity.Name] = item.EntityID
		}
		action := "apply"
		for _, c := range item.Conflicts {
			conflicts[c.Type]++
			if c.Type != "new" && c.Type != "modified" {
				action = "discard"
			}
		}
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: action})
	}
	require.Equal(t, 1, conflicts["required_missing"])
	require.Equal(t, 4, conflicts["uncertain"])
	require.Contains(t, models, "Valid B")
	require.Contains(t, models, "Blank region")
	require.NotContains(t, models, "型号待定")
	require.NotContains(t, models, "Ambiguous C")
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, nil)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+models["Yellow model"], nil, http.StatusOK, &detail)
	require.True(t, detail.Shelf.SaleStatuses[0].PlannedEOM)
	f.request(http.MethodGet, "/entities/"+models["Blank region"], nil, http.StatusOK, &detail)
	require.Empty(t, detail.Shelf.SaleStatuses)
}

func confirmImport(t *testing.T, f *managedNamesFixture, libraryID string, draft types.ShelfChangeset) types.ShelfChangeset {
	t.Helper()
	decisions := []types.ShelfDecision{}
	for _, item := range draft.Items {
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: "apply"})
	}
	f.request(http.MethodPost, "/libraries/"+libraryID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, &draft)
	return draft
}

// Medium spec, task 11 / ADR 0023: later maps propose manual-value conflicts
// and missing models, but neither the upload nor keep/discard changes the shelf.
func TestShelfImportMapProtectsManualValuesAndMissingModels(t *testing.T) {
	f, library := importSetup(t)
	data := importWorkbook(t, importRow(3, "Map platform", "Map product", "Model A;Model B", "GA", "12"), "")
	draft := confirmImport(t, f, library.ID, importUpload(t, f, library.ID, "map_20261028.xlsx", "", data, http.StatusCreated))
	ids := map[string]string{}
	for _, item := range draft.Items {
		if item.Operation.Entity != nil {
			ids[item.Operation.Entity.Name] = item.EntityID
		}
	}
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+ids["Model A"], nil, http.StatusOK, &detail)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "manual", "type": "set_sale_statuses", "entity_id": ids["Model A"], "revision": detail.Entity.Revision, "sale_statuses": []any{map[string]any{"region": "region_a", "status": "DEV"}}}}}, http.StatusCreated, nil)
	draft = importUpload(t, f, library.ID, "map_20261029.xlsx", "", importWorkbook(t, importRow(3, "Map platform", "Map product", "Model A", "EOM", "12"), ""), http.StatusCreated)
	manual, missing := false, false
	decisions := []types.ShelfDecision{}
	for _, item := range draft.Items {
		action := "discard"
		for _, c := range item.Conflicts {
			if c.Type == "modified" && len(c.ManualBefore) > 0 {
				manual = true
			}
			if c.Type == "disappeared" {
				missing = true
				require.Equal(t, ids["Model B"], item.EntityID)
				action = "keep"
			}
		}
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: action})
	}
	require.True(t, manual)
	require.True(t, missing)
	f.request(http.MethodGet, "/entities/"+ids["Model A"], nil, http.StatusOK, &detail)
	require.Equal(t, "DEV", detail.Shelf.SaleStatuses[0].Status)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, nil)
	f.request(http.MethodGet, "/entities/"+ids["Model B"], nil, http.StatusOK, &detail)
	require.Nil(t, detail.Entity.ArchivedAt)
	var logs types.OperationLogPage
	f.request(http.MethodGet, "/entities/"+ids["Model A"]+"/history?source_type=import&import_batch_id="+draft.Source.ImportBatchID, nil, http.StatusOK, &logs)
	// A keep/discard batch writes no entity log. The first confirmed upload does.
	first := importUpload(t, f, library.ID, "map_20261028.xlsx", "", data, http.StatusCreated)
	f.request(http.MethodGet, "/entities/"+ids["Model A"]+"/history?source_type=import&import_batch_id="+first.Source.ImportBatchID, nil, http.StatusOK, &logs)
	require.NotEmpty(t, logs.Items)
	require.Equal(t, "Map", logs.Items[0].Source.Worksheet)
	require.Contains(t, logs.Items[0].Source.Cells, "C3")
	// Refusing a value change retains the model in the file's identity baseline.
	third := importUpload(t, f, library.ID, "map_20261030.xlsx", "", importWorkbook(t, importRow(3, "Map platform", "Map product", "Model B", "GA", "12"), ""), http.StatusCreated)
	missingA := false
	for _, item := range third.Items {
		missingA = missingA || (item.Operation.Type == "missing_entity" && item.EntityID == ids["Model A"])
	}
	require.True(t, missingA)
}

// Medium spec, task 11: absent, invalid or ambiguous filename dates require an
// explicit date; a supplied date is validated rather than replaced by upload time.
func TestShelfImportRequiresReliableFileDate(t *testing.T) {
	f, library := importSetup(t)
	data := importWorkbook(t, importRow(3, "Map platform", "Map product", "Model A", "GA", "1"), "")
	for _, name := range []string{"map.xlsx", "map_20261350.xlsx", "map_20261028_20261029.xlsx"} {
		importUpload(t, f, library.ID, name, "", data, http.StatusBadRequest)
	}
	for _, date := range []string{"2026-20-01", "0001-01-01T00:00:00Z", "2206-02-03T00:00:00Z"} {
		importUpload(t, f, library.ID, "map.xlsx", date, data, http.StatusBadRequest)
	}
	draft := importUpload(t, f, library.ID, "map.xlsx", "2026-10-20T12:30:00Z", data, http.StatusCreated)
	require.Equal(t, "2026-10-20T12:30:00Z", draft.Source.FileDate.Format(time.RFC3339))
}

// Medium regression, task 11 / ADR 0022: a map must not copy an unrelated
// later parameter into the past when it replaces other model values.
func TestShelfImportPreservesUnmappedParameterTimeline(t *testing.T) {
	f, library := importSetup(t)
	draft := confirmImport(t, f, library.ID, importUpload(t, f, library.ID, "map_20261028.xlsx", "", importWorkbook(t, importRow(3, "Map platform", "Map product", "Model A", "GA", "12"), ""), http.StatusCreated))
	id := ""
	for _, item := range draft.Items {
		if item.Operation.Entity != nil && item.Operation.Entity.Kind == "model" {
			id = item.EntityID
		}
	}
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &detail)
	field := library.EntitySchema.Fields[len(library.EntitySchema.Fields)-1]
	field.Key = "power"
	field.Name = "Power"
	library.EntitySchema.Fields = append(library.EntitySchema.Fields, field)
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_schema": library.EntitySchema}, http.StatusOK, nil)
	attrs := detail.Entity.Attributes
	attrs["power"] = []any{map[string]any{"value": 99}}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "future", "type": "update_entity", "entity_id": id, "patch": map[string]any{"revision": detail.Entity.Revision, "effective_at": "2026-11-01T00:00:00Z", "attributes": attrs}}}}, http.StatusCreated, nil)
	confirmImport(t, f, library.ID, importUpload(t, f, library.ID, "map_20261029.xlsx", "", importWorkbook(t, importRow(3, "Map platform", "Map product", "Model A", "DEV", "15"), ""), http.StatusCreated))
	f.request(http.MethodGet, "/entities/"+id+"?as_of=2026-10-30T00:00:00Z", nil, http.StatusOK, &detail)
	require.NotContains(t, detail.Shelf.Parameters, "power")
	f.request(http.MethodGet, "/entities/"+id+"?as_of=2090-01-02T00:00:00Z", nil, http.StatusOK, &detail)
	require.EqualValues(t, 99, detail.Shelf.Parameters["power"][0].Value)
}

// Medium regression, task 11: confirming a deferred subset cannot replace the
// complete previous file when the next import detects disappeared models.
func TestShelfImportDisappearanceIncludesDeferredBatchParts(t *testing.T) {
	f, library := importSetup(t)
	draft := importUpload(t, f, library.ID, "map_20261028.xlsx", "", importWorkbook(t, importRow(3, "Map platform", "Map product", "Model A;Model B", "GA", "12"), ""), http.StatusCreated)
	refA, idB := "", ""
	for _, item := range draft.Items {
		if item.Operation.Entity != nil {
			if item.Operation.Entity.Name == "Model A" {
				refA = "$" + item.ID
			}
			if item.Operation.Entity.Name == "Model B" {
				idB = item.EntityID
			}
		}
	}
	decisions := []types.ShelfDecision{}
	for _, item := range draft.Items {
		action := "apply"
		if item.Operation.Type == "update_entity" && item.Operation.EntityID == refA {
			action = "defer"
		}
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: action})
	}
	path := "/libraries/" + library.ID + "/shelf/changesets/"
	f.request(http.MethodPost, path+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, &draft)
	require.NotEmpty(t, draft.DeferredChangesetID)
	f.request(http.MethodGet, path+draft.DeferredChangesetID, nil, http.StatusOK, &draft)
	confirmImport(t, f, library.ID, draft)
	draft = importUpload(t, f, library.ID, "map_20261029.xlsx", "", importWorkbook(t, importRow(3, "Map platform", "Map product", "Model A", "GA", "12"), ""), http.StatusCreated)
	missing := []string{}
	for _, item := range draft.Items {
		if item.Operation.Type == "missing_entity" {
			missing = append(missing, item.EntityID)
		}
	}
	require.Equal(t, []string{idB}, missing)
}

// Medium spec, task 11: malformed values remain traceable review conflicts,
// including nonfinite numbers and formulas whose cached value is not evidence.
func TestShelfImportUnparseableRowsRetainRawCells(t *testing.T) {
	f, library := importSetup(t)
	for _, sample := range []struct {
		name, raw string
		formula   bool
	}{{"Not a number", "NaN", false}, {"Nonfinite range", "1~Inf", false}, {"Formula model", "42", true}} {
		t.Run(sample.name, func(t *testing.T) {
			row := importRow(3, "Map platform", "Map product", sample.name, "GA", sample.raw)
			if sample.formula {
				row = strings.Replace(row, "<v>42</v>", "<f>6*7</f><v>42</v>", 1)
			}
			draft := importUpload(t, f, library.ID, "map_20261028.xlsx", "", importWorkbook(t, row, ""), http.StatusCreated)
			require.Len(t, draft.Items, 1)
			require.Equal(t, "unparseable", draft.Items[0].Conflicts[0].Type)
			details := draft.Items[0].Conflicts[0].Details.(map[string]any)
			require.Equal(t, sample.raw, details["E3"])
			f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "apply"}}}, http.StatusConflict, nil)
		})
	}
}

// Medium spec, task 11 / ADR 0023: fill rules are confirmed configuration;
// an uncertain yellow mark cannot be accepted as an inferred retirement.
func TestShelfImportUnknownFillRequiresSubmitterDecision(t *testing.T) {
	f, library := importSetup(t)
	var plans []types.ShelfImportPlan
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/import-plans", nil, http.StatusOK, &plans)
	definition := plans[0].Definition
	definition.FillMeanings = map[string]string{"FFFFFF00": "uncertain"}
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic map", FileKind: "product_map", Revision: 1, ShelfImportDefinition: definition}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
	row := strings.Replace(importRow(3, "Map platform", "Map product", "Yellow model", "GA", "12"), `r="C3" t=`, `r="C3" s="1" t=`, 1)
	draft := importUpload(t, f, library.ID, "map_20261028.xlsx", "", importWorkbook(t, row, ""), http.StatusCreated)
	require.EqualValues(t, 2, draft.Source.PlanRevision)
	require.Len(t, draft.Items, 1)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
}

// Medium spec, task 11: a configured status color cross-checks the cell text;
// contradictory encodings must be returned rather than selecting either value.
func TestShelfImportFillCannotContradictRegionalStatus(t *testing.T) {
	f, library := importSetup(t)
	var plans []types.ShelfImportPlan
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/import-plans", nil, http.StatusOK, &plans)
	definition := plans[0].Definition
	definition.FillMeanings = map[string]string{"FFFFFF00": "GA"}
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic map", FileKind: "product_map", Revision: 1, ShelfImportDefinition: definition}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
	row := strings.Replace(importRow(3, "Map platform", "Map product", "Colored model", "DEV", "12"), `r="D3" t=`, `r="D3" s="1" t=`, 1)
	draft := importUpload(t, f, library.ID, "map_20261028.xlsx", "", importWorkbook(t, row, ""), http.StatusCreated)
	require.Len(t, draft.Items, 1)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
}

// Medium spec, task 11: formatted numbers can change the displayed identity
// or unit. Import must retain a conflict rather than use their raw storage value.
func TestShelfImportFormattedNumbersRequireExplicitValues(t *testing.T) {
	f, library := importSetup(t)
	styles := `<styleSheet><numFmts><numFmt numFmtId="164" formatCode="0000"/></numFmts><fills><fill><patternFill patternType="none"/></fill></fills><cellXfs><xf fillId="0"/><xf fillId="0" numFmtId="164"/></cellXfs></styleSheet>`
	row := strings.Replace(importRow(3, "Map platform", "Map product", "Formatted model", "GA", "12"), `r="E3"`, `r="E3" s="1"`, 1)
	draft := importUpload(t, f, library.ID, "map_20261028.xlsx", "", importWorkbook(t, row, "", styles), http.StatusCreated)
	require.Len(t, draft.Items, 1)
	require.Equal(t, "unparseable", draft.Items[0].Conflicts[0].Type)
	require.Equal(t, "12", draft.Items[0].Conflicts[0].Details.(map[string]any)["E3"])
}

// Medium spec, task 11: oversized maps are rejected as a whole, leaving no
// partial pending batch or catalog writes behind.
func TestShelfImportOperationLimitRejectsWholeBatch(t *testing.T) {
	f, library := importSetup(t)
	models := []string{}
	for i := 0; i < 170; i++ {
		models = append(models, fmt.Sprintf("Synthetic model %03d", i))
	}
	importUpload(t, f, library.ID, "map_20261028.xlsx", "", importWorkbook(t, importRow(3, "Map platform", "Map product", strings.Join(models, ";"), "GA", "12"), ""), http.StatusBadRequest)
	var page types.ShelfChangesetPage
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/changesets?status=pending", nil, http.StatusOK, &page)
	require.Zero(t, page.Total)
}

// Medium spec, task 11 / ADR 0023: a map does not bypass the shared cross-kind
// confirmation when a platform and product have the same customer spelling.
func TestShelfImportCrossKindNamesRequireConfirmation(t *testing.T) {
	f, library := importSetup(t)
	draft := importUpload(t, f, library.ID, "map_20261028.xlsx", "", importWorkbook(t, importRow(3, "Shared name", "Shared name", "Model A", "GA", "12"), ""), http.StatusCreated)
	found := false
	for _, item := range draft.Items {
		for _, c := range item.Conflicts {
			found = found || c.Type == "cross_kind_same_name"
		}
	}
	require.True(t, found)
	decisions := []types.ShelfDecision{}
	for _, item := range draft.Items {
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: "apply"})
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusConflict, nil)
}

// Medium spec, task 11 / ADR 0023: import plans are reviewed shelf operations,
// survive reads and cannot overwrite a newer revision.
func TestShelfImportPlanRequiresConfirmationAndRevision(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	path := "/libraries/" + library.ID + "/shelf"
	plan := map[string]any{"key": "map", "name": "Synthetic map", "file_kind": "product_map", "sheets": []any{map[string]any{
		"worksheet": "Map", "product_line_id": shelfNode(nodes, "line"), "header_rows": 2,
		"columns": []any{map[string]any{"column": "A", "field": "platform"}, map[string]any{"column": "B", "field": "product"}, map[string]any{"column": "C", "field": "model"}},
	}}}
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "plan", "type": "save_import_plan", "import_plan": plan}}}, http.StatusCreated, &draft)
	var plans []map[string]any
	f.request(http.MethodGet, path+"/import-plans", nil, http.StatusOK, &plans)
	require.Empty(t, plans)
	f.request(http.MethodPost, path+"/changesets/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "plan", "action": "apply"}}}, http.StatusOK, nil)
	f.request(http.MethodGet, path+"/import-plans", nil, http.StatusOK, &plans)
	require.Len(t, plans, 1)
	require.EqualValues(t, 1, plans[0]["revision"])
	require.Equal(t, "Synthetic map", plans[0]["name"])
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "stale", "type": "save_import_plan", "import_plan": plan}}}, http.StatusCreated, &draft)
	require.Equal(t, "revision_conflict", draft.Items[0].Conflicts[0].Type)
}
