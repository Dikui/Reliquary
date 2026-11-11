package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Medium regression, task 33: fill conflicts expose the exact server key and
// source cell, so callers can resubmit a precise fill_meanings interpretation.
func TestShelfImportFillConflictExposesServerKeyAndCell(t *testing.T) {
	for _, sample := range []struct{ kind, color, key string }{
		{"certification_progress", `theme="9" tint="0.59999389629810485"`, "theme:9:0.59999389629810485"},
		{"atq", `theme="9"`, "theme:9:"},
		{"product_map", `rgb="ffaabbcc"`, "FFAABBCC"},
		{"overseas_sales", `indexed="6"`, "indexed:6"},
	} {
		t.Run(sample.kind, func(t *testing.T) {
			var f *managedNamesFixture
			var l types.Library
			var row, ref string
			if sample.kind == "product_map" {
				f, l = importSetup(t)
				row, ref = importRow(3, "Platform", "Product", "Model", "GA", "9"), "D3"
			} else {
				var nodes types.ShelfChangeset
				if sample.kind == "atq" {
					f, l, nodes = checklistSetup(t)
					importRecordPlan(f, l, "atq", []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}}, map[string]any{"product_line_id": shelfNode(nodes, "line")})
					row = importRecordRow(3, "Synthetic requirement", "Final custom")
				} else {
					f, l, _ = shelfModelSetup(t)
					configureCertificationStandards(f, &l)
					if sample.kind == "overseas_sales" {
						importRecordPlan(f, l, sample.kind, []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "sale_status", Key: "region_a"}}, nil)
						row = importRecordRow(3, "Synthetic model", "GA")
					} else {
						importRecordPlan(f, l, sample.kind, []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}}, nil)
						row = importRecordRow(3, "Synthetic model", "standard_a", "Container", "Country")
					}
				}
				ref = "A3"
			}
			row = strings.Replace(row, `r="`+ref+`"`, `r="`+ref+`" s="1"`, 1)
			styles := `<styleSheet><fills><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="solid"><fgColor ` + sample.color + `/></patternFill></fill></fills><cellXfs><xf fillId="0"/><xf fillId="1"/></cellXfs></styleSheet>`
			data := importWorkbook(t, row, "", styles)
			draft := importUpload(t, f, l.ID, "synthetic_20261028.xlsx", "", data, http.StatusCreated)
			require.Len(t, draft.Items, 1)
			require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
			details := draft.Items[0].Conflicts[0].Details.(map[string]any)
			require.Equal(t, sample.key, details["fill_key"])
			require.Equal(t, ref, details["cell"])
			var plans []types.ShelfImportPlan
			f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/import-plans", nil, http.StatusOK, &plans)
			def := plans[0].Definition
			def.FillMeanings = map[string]string{sample.key: "ignore"}
			plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic fill plan", FileKind: sample.kind, Revision: 1, ShelfImportDefinition: def}
			f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
			retried := importUpload(t, f, l.ID, "synthetic_20261028.xlsx", "", data, http.StatusCreated)
			for _, item := range retried.Items {
				for _, conflict := range item.Conflicts {
					require.NotContains(t, conflict.Message, "fill")
				}
			}
		})
	}
}

// Medium regression, task 33: first-time progress rows create named records;
// configured labels and original names survive confirmation and later updates.
func TestShelfImportNewCertificationNames(t *testing.T) {
	for _, sample := range []struct {
		nameColumn         bool
		name, region, want string
	}{
		{false, "", "region_a", "Synthetic model / Synthetic Standard /  Container original  / Region A"},
		{false, "", "", "Synthetic model / Synthetic Standard /  Container original "},
		{true, " Raw certificate name ", "Country", " Raw certificate name "},
		{true, "", "Country", "Synthetic model / Synthetic Standard /  Container original  / Country"},
	} {
		t.Run(sample.want, func(t *testing.T) {
			f, l, n := shelfModelSetup(t)
			configureCertificationStandards(f, &l)
			columns := []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}}
			if sample.nameColumn {
				columns = append(columns, types.ShelfImportColumn{Column: "E", Field: "name"})
			}
			importRecordPlan(f, l, "certification_progress", columns, nil)
			draft := importUpload(t, f, l.ID, "progress_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, shelfNode(n, "model"), "Customer spelling", " Container original ", sample.region, sample.name), ""), http.StatusCreated)
			require.Len(t, draft.Items, 1)
			require.Equal(t, "create_certification", draft.Items[0].Operation.Type)
			require.Equal(t, sample.want, draft.Items[0].Operation.Certification.Name)
			require.Len(t, draft.Items[0].Conflicts, 1)
			require.Equal(t, "new", draft.Items[0].Conflicts[0].Type)
			draft = confirmImport(t, f, l.ID, draft)
			var detail types.EntityDetail
			f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusOK, &detail)
			require.Equal(t, sample.want, detail.Entity.CanonicalName)
		})
	}
}

// Medium spec, task 33: the library-wide MCP guide lists the actual plan
// vocabulary compactly, including key requirements and conditional sheet fields.
func TestShelfGuideListsImportFieldsByFileKind(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	client := f.connect(t, "2025-11-25")
	var guide struct {
		ImportFields map[string]struct {
			Columns         []string          `json:"columns"`
			ColumnKeys      map[string]string `json:"column_keys"`
			RequiredColumns []string          `json:"required_columns"`
			RequiredOneOf   [][]string        `json:"required_one_of"`
			SheetFields     map[string]string `json:"sheet_fields"`
		} `json:"import_fields"`
	}
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID}, &guide)
	require.Len(t, guide.ImportFields, 5)
	report := guide.ImportFields["certification_report"]
	require.Equal(t, []string{"name", "owner", "standard", "year", "certification_object", "cell_part", "region", "agency", "progress", "start_date", "obtained_date", "issued_date", "validity", "number", "cloud_url", "sample_description", "model", "pack_model", "cluster_model", "container_model"}, report.Columns)
	require.Equal(t, []string{"owner", "standard", "certification_object", "region"}, report.RequiredColumns)
	require.Equal(t, "required", report.SheetFields["owner_kind"])
	require.Equal(t, "required", report.SheetFields["progress"])
	require.Equal(t, "required_with_cell_part", report.SheetFields["module_type"])
	progress := guide.ImportFields["certification_progress"]
	require.Contains(t, progress.Columns, "cell_part")
	require.NotContains(t, progress.Columns, "pack_model")
	require.NotContains(t, progress.SheetFields, "module_type")
	product := guide.ImportFields["product_map"]
	require.Equal(t, []string{"platform", "product", "model"}, product.RequiredColumns)
	require.Equal(t, map[string]string{"sale_status": "region", "parameter": "product_line_field"}, product.ColumnKeys)
	require.Equal(t, "required", product.SheetFields["product_line_id"])
	atq := guide.ImportFields["atq"]
	require.Equal(t, []string{"due_stages"}, atq.RequiredColumns)
	require.Equal(t, [][]string{{"name", "name_zh", "name_en"}}, atq.RequiredOneOf)
	require.Equal(t, map[string]string{"market": "market"}, atq.ColumnKeys)
	require.NotContains(t, atq.SheetFields, "module_type")
	overseas := guide.ImportFields["overseas_sales"]
	require.Equal(t, [][]string{{"standard", "sale_status", "certification"}}, overseas.RequiredOneOf)
	require.Equal(t, "required_with_certification", overseas.SheetFields["check_values"])
	require.Equal(t, map[string]string{"sale_status": "region", "certification": "standard"}, overseas.ColumnKeys)
	// Node requests retain task 32's compact response and omit plan vocabulary.
	guide.ImportFields = nil
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID, "node_id": shelfNode(n, "model")}, &guide)
	require.Empty(t, guide.ImportFields)
}

// Medium regression, task 33: generated names obey the same uniqueness rules
// as explicit names, both against saved records and earlier rows in the batch.
func TestShelfImportGeneratedCertificationNameConflicts(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "same_batch", true: "saved_record"}[existing], func(t *testing.T) {
			f, l, n := shelfModelSetup(t)
			configureCertificationStandards(f, &l)
			name := "Synthetic model / Synthetic Standard / Container / Region A"
			if existing {
				f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "cert", Type: "create_certification", Certification: &types.ShelfCertificationInput{Name: name, ShelfCertification: types.ShelfCertification{OwnerID: shelfNode(n, "single"), Standard: "standard_a", CertificationObject: "Other", Region: "region_a"}}}}}, http.StatusCreated, nil)
			}
			importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}}, nil)
			rows := importRecordRow(3, "Synthetic model", "standard_a", "Container", "region_a")
			if !existing {
				rows += importRecordRow(4, "Synthetic model", "standard_a", "Container", "Region A")
			}
			draft := importUpload(t, f, l.ID, "progress_20261028.xlsx", "", importWorkbook(t, rows, ""), http.StatusCreated)
			item := draft.Items[len(draft.Items)-1]
			require.Equal(t, "create_certification", item.Operation.Type)
			require.Equal(t, name, item.Operation.Certification.Name)
			require.NotEmpty(t, item.Conflicts)
			require.NotEqual(t, "new", item.Conflicts[0].Type)
			require.Contains(t, strings.ToLower(item.Conflicts[0].Message), "name")
			decisions := []types.ShelfDecision{}
			for _, item := range draft.Items {
				decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: "apply"})
			}
			f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusConflict, nil)
			var certs []types.ShelfCertificationView
			f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/certifications", nil, http.StatusOK, &certs)
			if existing {
				require.Len(t, certs, 1)
			} else {
				require.Empty(t, certs)
			}
		})
	}
}

// Small regression: a standard cell may carry its edition after a colon; only a
// configured base standard is split, and the year lands in the year field.
func TestShelfImportSplitsStandardYear(t *testing.T) {
	for _, sample := range []struct {
		standard string
		year     int
		ok       bool
	}{{"Customer spelling：2022", 2022, true}, {" Customer spelling : 2019 ", 2019, true}, {"Unknown standard:2022", 0, false}} {
		t.Run(sample.standard, func(t *testing.T) {
			f, l, n := shelfModelSetup(t)
			configureCertificationStandards(f, &l)
			importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}}, nil)
			draft := importUpload(t, f, l.ID, "progress_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, shelfNode(n, "model"), sample.standard, "Container", "region_a", ""), ""), http.StatusCreated)
			require.Len(t, draft.Items, 1)
			if !sample.ok {
				require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
				return
			}
			require.Equal(t, "create_certification", draft.Items[0].Operation.Type)
			require.Equal(t, sample.year, draft.Items[0].Operation.Certification.Year)
		})
	}
}
