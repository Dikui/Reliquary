package router

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// The test reads the downloaded Office package independently of the importer.
type exportedShelfSheet struct {
	Rows []struct {
		Cells []struct {
			Ref     string `xml:"r,attr"`
			Type    string `xml:"t,attr"`
			Style   int    `xml:"s,attr"`
			Text    string `xml:"is>t"`
			Formula string `xml:"f"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
}

func shelfExportDownload(t *testing.T, f *managedNamesFixture, library string, input any) map[string][]byte {
	t.Helper()
	body, err := json.Marshal(input)
	require.NoError(t, err)
	response := personalRequest(t, f.router, http.MethodPost, "/api/v1/libraries/"+library+"/shelf/export", string(body), "", false)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", response.Header().Get("Content-Type"))
	require.Contains(t, response.Header().Get("Content-Disposition"), ".xlsx")
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	require.NoError(t, err)
	files := map[string][]byte{}
	for _, file := range archive.File {
		r, err := file.Open()
		require.NoError(t, err)
		files[file.Name], err = io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
	}
	return files
}

func exportedShelfTexts(t *testing.T, data []byte) (exportedShelfSheet, [][]string) {
	t.Helper()
	var sheet exportedShelfSheet
	require.NoError(t, xml.Unmarshal(data, &sheet))
	rows := [][]string{}
	for _, row := range sheet.Rows {
		texts := []string{}
		for _, cell := range row.Cells {
			require.Empty(t, cell.Formula, "customer text must never become an Excel formula")
			texts = append(texts, cell.Text)
		}
		rows = append(rows, texts)
	}
	return sheet, rows
}

// Medium spec, task 15 / ADR 0023: the HTTP download is the export boundary;
// all data is synthetic and saved through confirmed changesets.
func TestShelfExportProductMapContents(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	statuses := []types.ShelfSaleStatus{
		{Region: "region_a", Status: "GA", PlannedEOM: true, Notes: "Synthetic note", Raw: "GA (source)", Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "a")}}}},
		{Region: "region_a", Status: "DEV", Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "b")}}}},
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "status", Type: "set_sale_statuses", EntityID: shelfNode(nodes, "model"), Revision: 1, SaleStatuses: &statuses}}}, http.StatusCreated, nil)
	files := shelfExportDownload(t, f, library.ID, map[string]any{"product_line_id": shelfNode(nodes, "line")})
	require.Contains(t, files, "[Content_Types].xml")
	require.Contains(t, files, "_rels/.rels")
	require.Contains(t, string(files["xl/workbook.xml"]), `name="Synthetic line"`)
	sheet, rows := exportedShelfTexts(t, files["xl/worksheets/sheet1.xml"])
	require.Len(t, rows, 4)
	require.Equal(t, "Synthetic line", rows[0][0])
	require.Equal(t, []string{"产品平台", "产品名称", "产品型号", "描述", "Region A"}, rows[1])
	require.Equal(t, []string{"Synthetic platform", "Synthetic product", "Synthetic model", ""}, rows[2][:4])
	require.Contains(t, rows[2][4], "GA [计划退市]")
	require.Contains(t, rows[2][4], "Synthetic note")
	require.Contains(t, rows[2][4], "GA (source)")
	require.Contains(t, rows[2][4], "Module A ∈ {Synthetic a}")
	require.Contains(t, rows[2][4], "DEV")
	require.Contains(t, rows[2][4], "Module A ∈ {Synthetic b}")
	require.NotZero(t, sheet.Rows[2].Cells[4].Style, "planned retirement must also have a visible fill")
	var styles struct {
		Fills []struct {
			Pattern struct {
				Color struct {
					RGB string `xml:"rgb,attr"`
				} `xml:"fgColor"`
			} `xml:"patternFill"`
		} `xml:"fills>fill"`
		Formats []struct {
			Fill int `xml:"fillId,attr"`
		} `xml:"cellXfs>xf"`
	}
	require.NoError(t, xml.Unmarshal(files["xl/styles.xml"], &styles))
	format := styles.Formats[sheet.Rows[2].Cells[4].Style]
	require.Equal(t, "FFFFFF00", styles.Fills[format.Fill].Pattern.Color.RGB)
	require.Equal(t, "Synthetic single", rows[3][2])
	require.Empty(t, rows[3][4], "no record stays blank, rather than becoming pending")
}

// Medium spec, task 15: worksheet labels obey Excel limits while the full
// customer name remains unchanged in the sheet title, including formula text.
func TestShelfExportWorksheetNamesPreserveFullTitles(t *testing.T) {
	f, library := newProductShelfFixture(t)
	longName := "'=甲/" + strings.Repeat("示例", 20)
	operations := []types.ShelfOperation{}
	for i, name := range []string{longName, strings.ReplaceAll(longName, "/", "?"), "History", "history?", strings.Repeat("示例😀", 15)} {
		operations = append(operations, types.ShelfOperation{ID: fmt.Sprint(i), Type: "create_entity", Entity: &types.ManagedEntityInput{Kind: "product_line", Name: name}})
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: operations}, http.StatusCreated, nil)
	files := shelfExportDownload(t, f, library.ID, map[string]any{})
	var workbook struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
		} `xml:"sheets>sheet"`
	}
	require.NoError(t, xml.Unmarshal(files["xl/workbook.xml"], &workbook))
	require.Len(t, workbook.Sheets, 5)
	seen := map[string]bool{}
	titles := []string{}
	for i, sheet := range workbook.Sheets {
		require.LessOrEqual(t, len(utf16.Encode([]rune(sheet.Name))), 31)
		require.NotContains(t, sheet.Name, "/")
		require.NotContains(t, sheet.Name, "?")
		require.False(t, strings.HasPrefix(sheet.Name, "'") || strings.HasSuffix(sheet.Name, "'"))
		require.NotEqual(t, "history", strings.ToLower(sheet.Name))
		require.False(t, seen[strings.ToLower(sheet.Name)], "Excel names are case insensitive")
		seen[strings.ToLower(sheet.Name)] = true
		_, rows := exportedShelfTexts(t, files[fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)])
		require.Len(t, rows, 2, "empty product lines retain a title and columns")
		titles = append(titles, rows[0][0])
	}
	require.Contains(t, titles, longName)
}

// Medium spec, task 15: export is complete beyond the query page size and each
// line keeps its own region columns even when the applied filters return no rows.
func TestShelfExportAllLinesFiltersAndPagination(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	library.ShelfConfig.Regions = []types.ShelfOption{{Key: "region_b", Name: "South & West"}, {Key: "region_a", Name: "Region A"}, {Key: "unused", Name: "Unused"}}
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": library.ShelfConfig}, http.StatusOK, nil)
	operations := []types.ShelfOperation{}
	for i := 0; i < 101; i++ {
		id := fmt.Sprintf("bulk%d", i)
		statuses := []types.ShelfSaleStatus{{Region: "region_a", Status: []string{"GA", "DEV", "Planning", "EOM", "待定"}[i%5]}}
		if i == 0 {
			statuses = append(statuses, types.ShelfSaleStatus{Region: "region_b", Status: "EOM"})
		}
		operations = append(operations,
			types.ShelfOperation{ID: id, Type: "create_entity", Entity: &types.ManagedEntityInput{Kind: "model", Name: fmt.Sprintf("Bulk %03d", i), Description: "=SUM(1,2) <synthetic>", Attributes: map[string]any{"sale_statuses": statuses}}},
			types.ShelfOperation{ID: id + "_parent", Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: "$" + id, ObjectID: shelfNode(nodes, "product"), RelationType: "belongs_to"}},
		)
	}
	for _, node := range []struct{ id, kind, parent string }{{"line2", "product_line", ""}, {"platform2", "platform", "line2"}, {"product2", "product", "platform2"}, {"model2", "model", "product2"}} {
		entity := &types.ManagedEntityInput{Kind: node.kind, Name: "Second " + node.kind}
		if node.kind == "model" {
			entity.Attributes = map[string]any{"sale_statuses": []types.ShelfSaleStatus{{Region: "region_b", Status: "GA"}}}
		}
		operations = append(operations, types.ShelfOperation{ID: node.id, Type: "create_entity", Entity: entity})
		if node.parent != "" {
			operations = append(operations, types.ShelfOperation{ID: node.id + "_parent", Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: "$" + node.id, ObjectID: "$" + node.parent, RelationType: "belongs_to"}})
		}
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: operations}, http.StatusCreated, nil)
	files := shelfExportDownload(t, f, library.ID, map[string]any{"query": map[string]any{"limit": 1, "offset": 1000}})
	_, second := exportedShelfTexts(t, files["xl/worksheets/sheet1.xml"])
	require.Equal(t, []string{"产品平台", "产品名称", "产品型号", "描述", "South & West"}, second[1])
	_, first := exportedShelfTexts(t, files["xl/worksheets/sheet2.xml"])
	require.Len(t, first, 105)
	require.Equal(t, []string{"产品平台", "产品名称", "产品型号", "描述", "South & West", "Region A"}, first[1])
	require.Equal(t, "=SUM(1,2) <synthetic>", first[2][3])
	require.Equal(t, []string{"GA", "DEV", "Planning", "EOM", "待定"}, []string{first[2][5], first[3][5], first[4][5], first[5][5], first[6][5]})
	require.Equal(t, "Bulk 100", first[102][2])
	filtered := shelfExportDownload(t, f, library.ID, map[string]any{"query": map[string]any{"sale_statuses": []types.ShelfStatusFilter{{Region: "region_a", Status: "DEV"}}}})
	_, second = exportedShelfTexts(t, filtered["xl/worksheets/sheet1.xml"])
	require.Len(t, second, 2)
	require.Equal(t, "South & West", second[1][4], "empty filtered results must retain the line's columns")
	_, first = exportedShelfTexts(t, filtered["xl/worksheets/sheet2.xml"])
	require.Len(t, first, 22)
	for _, row := range first[2:] {
		require.Equal(t, "DEV", row[5])
	}
}

// Medium spec, task 15 / ADR 0023: a download cannot cross a library boundary
// or interpret an ordinary library or non-line node as a product line.
func TestShelfExportValidatesScope(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	var other types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Other shelf", "directory": "other-shelf", "entity_mode": "managed", "shelf_enabled": true}, http.StatusCreated, &other)
	var foreign types.Entity
	f.request(http.MethodPost, "/libraries/"+other.ID+"/entities", types.ManagedEntityInput{Kind: "product_line", Name: "Foreign line"}, http.StatusCreated, &foreign)
	path := "/libraries/" + library.ID + "/shelf/export"
	for _, id := range []string{foreign.ID, shelfNode(nodes, "model"), "missing"} {
		f.request(http.MethodPost, path, map[string]any{"product_line_id": id}, http.StatusBadRequest, nil)
	}
	f.request(http.MethodPost, path, map[string]any{"query": map[string]any{"sale_statuses": []types.ShelfStatusFilter{{Status: "unknown"}}}}, http.StatusBadRequest, nil)
	var ordinary types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Ordinary", "directory": "ordinary", "entity_mode": "managed"}, http.StatusCreated, &ordinary)
	f.request(http.MethodPost, "/libraries/"+ordinary.ID+"/shelf/export", map[string]any{}, http.StatusBadRequest, nil)
}

// Medium spec, task 15: an Excel cell cannot hold more than 32767 UTF-16 units;
// refusing the download protects the full original text from silent truncation.
func TestShelfExportRejectsCellOverflow(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	statuses := []types.ShelfSaleStatus{{Region: "region_a", Status: "GA", Notes: strings.Repeat("字", 20000), Raw: strings.Repeat("原", 20000)}}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "status", Type: "set_sale_statuses", EntityID: shelfNode(nodes, "model"), Revision: 1, SaleStatuses: &statuses}}}, http.StatusCreated, nil)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/export", map[string]any{}, http.StatusBadRequest, nil)
}
