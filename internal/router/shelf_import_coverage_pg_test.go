package router

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Styles for the synthetic workbooks: xf 1 yellow fill, xf 2 strikethrough,
// xf 3 red text. Font 0 is the workbook default and is never reported.
const coverageStyles = `<styleSheet><fonts><font/><font><strike/></font><font><color rgb="FFFF0000"/></font></fonts>` +
	`<fills><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FFFFFF00"/></patternFill></fill></fills>` +
	`<cellXfs><xf fillId="0" fontId="0"/><xf fillId="2" fontId="0"/><xf fillId="0" fontId="1"/><xf fillId="0" fontId="2"/></cellXfs></styleSheet>`

// coverageWorkbook builds a one-sheet "Map" workbook. extra holds additional
// archive parts such as worksheet relationships and comments.
func coverageWorkbook(t *testing.T, sheetXML string, extra map[string]string) []byte {
	t.Helper()
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	files := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Map" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/styles.xml":              coverageStyles,
		"xl/worksheets/sheet1.xml":   sheetXML,
	}
	for name, content := range extra {
		files[name] = content
	}
	for name, content := range files {
		w, err := z.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	return data.Bytes()
}

func coverageCell(ref, value string, style int) string {
	return fmt.Sprintf(`<c r="%s" s="%d" t="inlineStr"><is><t>%s</t></is></c>`, ref, style, value)
}

func coverageNotices(draft types.ShelfChangeset) map[string]map[string]any {
	result := map[string]map[string]any{}
	for _, item := range draft.Items {
		for _, conflict := range item.Conflicts {
			if conflict.Type == "uncovered_content" {
				details := conflict.Details.(map[string]any)
				result[details["reason"].(string)] = details
			}
		}
	}
	return result
}

func confirmImportDiscardingUncovered(t *testing.T, f *managedNamesFixture, libraryID string, draft types.ShelfChangeset) types.ShelfChangeset {
	t.Helper()
	decisions := []types.ShelfDecision{}
	for _, item := range draft.Items {
		action := "apply"
		for _, conflict := range item.Conflicts {
			if conflict.Type == "uncovered_content" {
				action = "discard"
			}
		}
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: action})
	}
	f.request(http.MethodPost, "/libraries/"+libraryID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, &draft)
	return draft
}

func saveCoveragePlan(t *testing.T, f *managedNamesFixture, library types.Library, sheet types.ShelfImportSheet) {
	t.Helper()
	var plans []types.ShelfImportPlan
	base := "/libraries/" + library.ID + "/shelf"
	f.request(http.MethodGet, base+"/import-plans", nil, http.StatusOK, &plans)
	definition := plans[0].Definition
	sheet.ProductLineID = definition.Sheets[0].ProductLineID
	sheet.Columns = definition.Sheets[0].Columns
	definition.Sheets = []types.ShelfImportSheet{sheet}
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic map", FileKind: "product_map", Revision: plans[0].Revision, ShelfImportDefinition: definition}
	f.request(http.MethodPost, base+"/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
}

// ADR 0025: a messy product map. Title, legend and footnote sit outside the
// configured range; one row is struck through, one hidden, one carries red
// text and a link, a mapped cell has a comment, a row holds only a fill and the
// sheet has a picture. Every one is reported and blocks a plain "apply", while
// the mapped models are still proposed unchanged.
func TestShelfImportCoverageReportsEverySourceSignal(t *testing.T) {
	f, library := importSetup(t)
	saveCoveragePlan(t, f, library, types.ShelfImportSheet{Worksheet: "Map", Range: "A3:E20", HeaderRows: 1})
	row := func(n int, style int, hidden bool, platform, product, model, status, energy string) string {
		attr := ""
		if hidden {
			attr = ` hidden="1"`
		}
		return fmt.Sprintf(`<row r="%d"%s>%s%s%s%s<c r="E%d"><v>%s</v></c></row>`, n, attr, coverageCell(fmt.Sprintf("A%d", n), platform, 0), coverageCell(fmt.Sprintf("B%d", n), product, 0), coverageCell(fmt.Sprintf("C%d", n), model, style), coverageCell(fmt.Sprintf("D%d", n), status, 0), n, energy)
	}
	sheet := `<worksheet xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheetData>` +
		`<row r="1">` + coverageCell("A1", "2026 product map", 0) + `</row>` +
		`<row r="2">` + coverageCell("G2", "Yellow = planned EOM", 0) + `</row>` +
		`<row r="3">` + coverageCell("A3", "Platform", 0) + coverageCell("B3", "Product", 0) + coverageCell("C3", "Model", 0) + coverageCell("D3", "Region A", 0) + coverageCell("E3", "Energy", 0) + `</row>` +
		row(4, 2, false, "Cov platform", "Cov product", "Model Strike", "GA", "9") +
		row(5, 0, true, "Cov platform", "Cov product", "Model Hidden", "GA", "10") +
		`<row r="6">` + coverageCell("A6", "Cov platform", 3) + coverageCell("B6", "Cov product", 0) + coverageCell("C6", "Model Link", 0) + coverageCell("D6", "GA", 0) + `<c r="E6"><v>11</v></c></row>` +
		`<row r="7"><c r="E7" s="1"/></row>` +
		`<row r="25">` + coverageCell("A25", "Note: draft figures", 0) + `</row>` +
		`</sheetData><hyperlinks><hyperlink ref="B6" r:id="rId1"/></hyperlinks><drawing r:id="rId3"/><legacyDrawing r:id="rId4"/></worksheet>`
	data := coverageWorkbook(t, sheet, map[string]string{
		"xl/worksheets/_rels/sheet1.xml.rels": `<Relationships><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/spec" TargetMode="External"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="../comments1.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing1.xml"/></Relationships>`,
		"xl/comments1.xml":                    `<comments><commentList><comment ref="D4"><text><r><t>Pilot customers only</t></r></text></comment></commentList></comments>`,
	})
	draft := importUpload(t, f, library.ID, "coverage_20261028.xlsx", "", data, http.StatusCreated)

	notices := coverageNotices(draft)
	require.Equal(t, map[string]any{"worksheet": "Map", "reason": "outside_range", "count": float64(3), "examples": []any{"2026 product map", "Yellow = planned EOM", "Note: draft figures"}}, notices["outside_range"])
	require.Equal(t, []any{"FFFFFF00"}, notices["format_only"]["examples"])
	require.Equal(t, []any{"Model Strike"}, notices["strike"]["examples"])
	require.Equal(t, []any{"FFFF0000 Cov platform"}, notices["font_color"]["examples"])
	require.Equal(t, []any{"Pilot customers only"}, notices["comment"]["examples"])
	require.Equal(t, []any{"https://example.com/spec"}, notices["hyperlink"]["examples"])
	require.Equal(t, float64(5), notices["hidden"]["count"])
	require.Equal(t, float64(1), notices["drawing"]["count"])
	require.Len(t, notices, 8)
	for _, item := range draft.Items {
		if item.Operation.Type == "import_notice" && item.Conflicts[0].Type == "uncovered_content" && item.Conflicts[0].Details.(map[string]any)["reason"] == "outside_range" {
			require.Equal(t, []string{"A1", "G2", "A25"}, item.Operation.ImportLocation.Cells)
		}
	}

	// Mapped content is still proposed; the range starts at row 3 so the
	// header and data keep their source coordinates.
	models := map[string][]string{}
	for _, item := range draft.Items {
		if item.Operation.Entity != nil && item.Operation.Entity.Kind == "model" && item.Operation.Entity.Name != "" {
			models[item.Operation.Entity.Name] = item.Operation.ImportLocation.Cells
		}
	}
	require.Equal(t, []string{"A4", "B4", "C4", "D4", "E4"}, models["Model Strike"])
	require.Contains(t, models, "Model Hidden")
	require.Contains(t, models, "Model Link")

	// Plain apply is refused while any coverage notice is undecided.
	decisions := []types.ShelfDecision{}
	for _, item := range draft.Items {
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: "apply"})
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusConflict, nil)
	confirmed := confirmImportDiscardingUncovered(t, f, library.ID, draft)
	require.Equal(t, "confirmed", confirmed.Status)
}

// ADR 0025: models laid out in columns are read through transpose. Plan
// columns address the normalized sheet; locations name the source cells.
func TestShelfImportTransposedSheetUsesSourceLocations(t *testing.T) {
	f, library := importSetup(t)
	saveCoveragePlan(t, f, library, types.ShelfImportSheet{Worksheet: "Map", Transpose: true, HeaderRows: 1})
	sheet := `<worksheet><sheetData>` +
		`<row r="1">` + coverageCell("A1", "Platform", 0) + coverageCell("B1", "T platform", 0) + coverageCell("C1", "T platform", 0) + `</row>` +
		`<row r="2">` + coverageCell("A2", "Product", 0) + coverageCell("B2", "T product", 0) + coverageCell("C2", "T product", 0) + `</row>` +
		`<row r="3">` + coverageCell("A3", "Model", 0) + coverageCell("B3", "T one", 0) + coverageCell("C3", "T two", 0) + `</row>` +
		`<row r="4">` + coverageCell("A4", "Region A", 0) + coverageCell("B4", "GA", 0) + coverageCell("C4", "DEV", 0) + `</row>` +
		`<row r="5">` + coverageCell("A5", "Energy", 0) + `<c r="B5"><v>9</v></c><c r="C5"><v>18</v></c></row>` +
		`</sheetData></worksheet>`
	draft := importUpload(t, f, library.ID, "transposed_20261028.xlsx", "", coverageWorkbook(t, sheet, nil), http.StatusCreated)
	require.Empty(t, coverageNotices(draft))
	locations := map[string][]string{}
	for _, item := range draft.Items {
		for _, conflict := range item.Conflicts {
			require.Contains(t, []string{"new", "modified"}, conflict.Type, conflict.Message)
		}
		if item.Operation.Entity != nil && item.Operation.Entity.Kind == "model" {
			locations[item.Operation.Entity.Name] = item.Operation.ImportLocation.Cells
		}
	}
	require.Equal(t, []string{"C1", "C2", "C3", "C4", "C5"}, locations["T two"])
	confirmed := confirmImport(t, f, library.ID, draft)
	statuses := map[string]string{}
	for _, item := range confirmed.Items {
		if item.Operation.Entity != nil && item.Operation.Entity.Kind == "model" {
			var detail types.EntityDetail
			f.request(http.MethodGet, "/entities/"+item.EntityID, nil, http.StatusOK, &detail)
			statuses[detail.Entity.CanonicalName] = detail.Shelf.SaleStatuses[0].Status
		}
	}
	require.Equal(t, map[string]string{"T one": "GA", "T two": "DEV"}, statuses)
}

// ADR 0025: invalid regions are rejected when the plan is saved.
func TestShelfImportPlanRejectsInvalidRange(t *testing.T) {
	f, library := importSetup(t)
	var plans []types.ShelfImportPlan
	base := "/libraries/" + library.ID + "/shelf"
	f.request(http.MethodGet, base+"/import-plans", nil, http.StatusOK, &plans)
	definition := plans[0].Definition
	definition.Sheets[0].Range = "E5:A1"
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic map", FileKind: "product_map", Revision: plans[0].Revision, ShelfImportDefinition: definition}
	var draft types.ShelfChangeset
	f.request(http.MethodPost, base+"/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, &draft)
	require.NotEmpty(t, draft.Items[0].Conflicts)
}
