package router

import (
	"archive/zip"
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Synthetic XLSX replaces customer files at the upload boundary. Unmapped-only
// rows, empty cells, header-only columns and an ignored sheet remain distinct.
func importFeedbackWorkbook(t *testing.T, mappedRow string) []byte {
	t.Helper()
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	rows := `<row r="1"><c r="F1" t="inlineStr"><is><t> Extra information </t></is></c></row>` +
		importRecordRow(2, "", "", "", "", "", " Notes & context ", "Header only") +
		strings.Replace(mappedRow, `</row>`, `<c r="F3" t="inlineStr"><is><t> First original </t></is></c><c r="H3"/><c r="Z3" t="inlineStr"><is><t>No header</t></is></c><c r="AA3"><f>1+1</f></c></row>`, 1) +
		`<row r="4"><c r="F4"/></row><row r="5"><c r="F5"><v>0</v></c></row>` +
		`<row r="6"><c r="F6" t="inlineStr"><is><t>Third</t></is></c></row>` +
		`<row r="7"><c r="F7" t="inlineStr"><is><t>Fourth</t></is></c></row>`
	for _, file := range []struct{ name, content string }{
		{"xl/workbook.xml", `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Map" r:id="rId1"/><sheet name="Ignored" r:id="rId2"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="worksheets/sheet2.xml"/></Relationships>`},
		{"xl/worksheets/sheet1.xml", `<worksheet><sheetData>` + rows + `</sheetData><mergeCells><mergeCell ref="F1:G1"/></mergeCells></worksheet>`},
		{"xl/worksheets/sheet2.xml", `<worksheet><sheetData>` + importRecordRow(3, "Ignored content") + `</sheetData></worksheet>`},
	} {
		w, err := z.Create(file.name)
		require.NoError(t, err)
		_, err = w.Write([]byte(file.content))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	return data.Bytes()
}

// Medium spec, task 25 / ADR 0023: unmapped content is persisted as an
// acknowledgeable notice, without losing the mapped product proposal.
func TestShelfImportReportsUnmappedColumns(t *testing.T) {
	f, library := importSetup(t)
	var plans []types.ShelfImportPlan
	base := "/libraries/" + library.ID + "/shelf"
	f.request(http.MethodGet, base+"/import-plans", nil, http.StatusOK, &plans)
	definition := plans[0].Definition
	definition.Sheets = append(definition.Sheets, types.ShelfImportSheet{Worksheet: "Ignored", Ignore: true})
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic map", FileKind: "product_map", Revision: 1, ShelfImportDefinition: definition}
	f.request(http.MethodPost, base+"/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
	data := importFeedbackWorkbook(t, importRow(3, "Map platform", "Map product", "Feedback model", "GA", "12"))
	draft := importUpload(t, f, library.ID, "feedback_20261028.xlsx", "", data, http.StatusCreated)
	notices := []types.ShelfChangesetItem{}
	for _, item := range draft.Items {
		if item.Operation.Type == "import_notice" {
			notices = append(notices, item)
		}
	}
	require.Len(t, notices, 3)
	require.Equal(t, "unmapped_column", notices[0].Conflicts[0].Type)
	require.Equal(t, map[string]any{"worksheet": "Map", "column": "F", "header": " Extra information \n Notes & context ", "nonempty_rows": float64(4), "examples": []any{" First original ", "0", "Third"}}, notices[0].Conflicts[0].Details)
	require.Equal(t, "Z", notices[1].Conflicts[0].Details.(map[string]any)["column"])
	require.Equal(t, "", notices[1].Conflicts[0].Details.(map[string]any)["header"])
	require.Equal(t, []any{"=1+1"}, notices[2].Conflicts[0].Details.(map[string]any)["examples"])
	require.Equal(t, []string{"F3", "F5", "F6"}, notices[0].Operation.ImportLocation.Cells)
	var read types.ShelfChangeset
	f.request(http.MethodGet, base+"/changesets/"+draft.ID, nil, http.StatusOK, &read)
	require.Equal(t, draft, read)
	require.Equal(t, draft, importUpload(t, f, library.ID, "feedback_20261028.xlsx", "", data, http.StatusCreated))
	confirmed := confirmImport(t, f, library.ID, draft)
	require.Equal(t, "confirmed", confirmed.Status)
	var page struct{ Items []types.Entity }
	f.request(http.MethodGet, base+"/find?q=Feedback+model", nil, http.StatusOK, &page)
	require.Len(t, page.Items, 1)
}

// Medium spec, task 25: all record import kinds can produce a feedback-only
// batch. Acknowledging it changes no shelf data, even without mapped row values.
func TestShelfImportRecordKindsAcknowledgeUnmappedColumnsWithoutWrites(t *testing.T) {
	for _, kind := range []string{"certification_progress", "certification_report", "atq", "overseas_sales"} {
		t.Run(kind, func(t *testing.T) {
			f, library, nodes := shelfModelSetup(t)
			mapping := types.ShelfImportSheet{Worksheet: "Map", HeaderRows: 2, OwnerKind: "model", Progress: "Completed"}
			mapping.Columns = []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}}
			if kind == "atq" {
				mapping.ProductLineID = shelfNode(nodes, "line")
				mapping.Columns = []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}}
			}
			if kind == "overseas_sales" {
				mapping.Columns = []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "sale_status", Key: "region_a"}}
			}
			plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic feedback", FileKind: kind, ShelfImportDefinition: types.ShelfImportDefinition{
				Sheets: []types.ShelfImportSheet{mapping, {Worksheet: "Ignored", Ignore: true}}, MissingValues: []string{"Third"},
			}}
			base := "/libraries/" + library.ID + "/shelf"
			f.request(http.MethodPost, base+"/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
			var before, after types.EntityDetail
			f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model"), nil, http.StatusOK, &before)
			draft := importUpload(t, f, library.ID, "feedback_20261028.xlsx", "", importFeedbackWorkbook(t, `<row r="3"></row>`), http.StatusCreated)
			require.Len(t, draft.Items, 3)
			for _, item := range draft.Items {
				require.Equal(t, "import_notice", item.Operation.Type)
				require.Equal(t, "unmapped_column", item.Conflicts[0].Type)
				require.Equal(t, "Map", item.Operation.ImportLocation.Worksheet)
			}
			require.Equal(t, float64(4), draft.Items[0].Conflicts[0].Details.(map[string]any)["nonempty_rows"])
			confirmImport(t, f, library.ID, draft)
			f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model"), nil, http.StatusOK, &after)
			require.Equal(t, before, after)
		})
	}
}

// Medium spec, task 25: feedback scans each mapped worksheet independently,
// including a worksheet whose mapped columns are empty.
func TestShelfImportReportsUnmappedColumnsOnEveryMappedSheet(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic sheets", FileKind: "product_map", ShelfImportDefinition: types.ShelfImportDefinition{Sheets: []types.ShelfImportSheet{
		{Worksheet: "Map", ProductLineID: shelfNode(nodes, "line"), HeaderRows: 2, Columns: []types.ShelfImportColumn{{Column: "A", Field: "platform"}, {Column: "B", Field: "product"}, {Column: "C", Field: "model"}}},
		{Worksheet: "Ignored", ProductLineID: shelfNode(nodes, "line"), HeaderRows: 0, Columns: []types.ShelfImportColumn{{Column: "D", Field: "platform"}, {Column: "E", Field: "product"}, {Column: "F", Field: "model"}}},
	}}}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
	draft := importUpload(t, f, library.ID, "feedback_20261028.xlsx", "", importFeedbackWorkbook(t, `<row r="3"></row>`), http.StatusCreated)
	require.Len(t, draft.Items, 4)
	require.Equal(t, map[string]any{"worksheet": "Ignored", "column": "A", "header": "", "nonempty_rows": float64(1), "examples": []any{"Ignored content"}}, draft.Items[3].Conflicts[0].Details)
	confirmImport(t, f, library.ID, draft)
}

// Medium spec, task 25: only inert notices may use column feedback. Existing
// blocking issues cannot be converted into an acknowledgeable notice.
func TestShelfChangesetUnmappedColumnsCannotBypassBusinessValidation(t *testing.T) {
	f, library, _ := shelfModelSetup(t)
	for _, operation := range []types.ShelfOperation{
		{ID: "write", Type: "create_entity", Entity: &types.ManagedEntityInput{Kind: "model", Name: "Unvalidated model"}, Issues: []types.ShelfConflict{{Type: "unmapped_column"}}},
		{ID: "notice", Type: "import_notice", Issues: []types.ShelfConflict{{Type: "required_missing"}}},
	} {
		f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Operations: []types.ShelfOperation{operation}}, http.StatusBadRequest, nil)
	}
}
