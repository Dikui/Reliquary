package router

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Synthetic workbook fragments replace customer files at the HTTP upload boundary.
func importRecordRow(row int, values ...string) string {
	var out strings.Builder
	fmt.Fprintf(&out, `<row r="%d">`, row)
	for i, value := range values {
		fmt.Fprintf(&out, `<c r="%c%d" t="inlineStr"><is><t>`, 'A'+i, row)
		requireXML := xml.EscapeText(&out, []byte(value))
		if requireXML != nil {
			panic(requireXML)
		}
		out.WriteString(`</t></is></c>`)
	}
	out.WriteString(`</row>`)
	return out.String()
}

func importRecordPlan(f *managedNamesFixture, library types.Library, kind string, columns []types.ShelfImportColumn, extra map[string]any) {
	sheet := map[string]any{"worksheet": "Map", "header_rows": 2, "owner_kind": "model", "columns": columns}
	for k, v := range extra {
		sheet[k] = v
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "plan", "type": "save_import_plan", "import_plan": map[string]any{"key": "map", "name": "Synthetic records", "file_kind": kind, "sheets": []any{sheet}}}}}, http.StatusCreated, nil)
}

// Medium spec, task 12: progress sheets update the unique certification key,
// preserve unmapped metadata and source spelling, and remain inert until confirmed.
func TestShelfImportCertificationProgressUsesIdentityAndFileDate(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	var seed types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "effective_at": "2026-10-01T00:00:00Z", "certification": map[string]any{"name": "Original certificate", "owner_id": shelfNode(n, "model"), "standard": "standard_a", "certification_object": "Container", "region": "Country", "progress": "Planning", "number": "KEEP-1"}}}}, http.StatusCreated, &seed)
	var model types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &model)
	importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}, {Column: "E", Field: "progress"}, {Column: "F", Field: "start_date"}}, nil)
	data := importWorkbook(t, importRecordRow(3, model.Entity.CanonicalName, "Customer spelling", "Container", "Country", " In progress ", "2026/10/02"), "")
	draft := importUpload(t, f, l.ID, "progress_20261028.xlsx", "", data, http.StatusCreated)
	require.Equal(t, "update_certification", draft.Items[0].Operation.Type)
	require.Equal(t, seed.Items[0].EntityID, draft.Items[0].EntityID)
	require.Equal(t, draft, importUpload(t, f, l.ID, "renamed_20261028.xlsx", "", data, http.StatusCreated))
	var before types.EntityDetail
	f.request(http.MethodGet, "/entities/"+seed.Items[0].EntityID, nil, http.StatusOK, &before)
	require.Equal(t, "Planning", before.Entity.Attributes["certification"].(map[string]any)["progress"])
	confirmImport(t, f, l.ID, draft)
	f.request(http.MethodGet, "/entities/"+seed.Items[0].EntityID, nil, http.StatusOK, &before)
	record := before.Entity.Attributes["certification"].(map[string]any)
	require.Equal(t, " In progress ", record["progress"])
	require.Equal(t, "KEEP-1", record["number"])
	require.Equal(t, "2026-10-02", record["start_date"])
	f.request(http.MethodGet, "/entities/"+seed.Items[0].EntityID+"?as_of=2026-10-27T00:00:00Z", nil, http.StatusOK, &before)
	require.Equal(t, "Planning", before.Entity.Attributes["certification"].(map[string]any)["progress"])
	var logs types.OperationLogPage
	f.request(http.MethodGet, "/entities/"+seed.Items[0].EntityID+"/history?import_batch_id="+draft.Source.ImportBatchID, nil, http.StatusOK, &logs)
	require.NotEmpty(t, logs.Items)
	require.Contains(t, logs.Items[0].Source.Cells, "F3")
}

// Medium spec, task 12: dates that cannot be parsed retain the original cell;
// unknown owners and unmatched certification keys require an explicit correction.
func TestShelfImportCertificationInvalidDatesAndKeysNeedReview(t *testing.T) {
	f, l, _ := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}, {Column: "E", Field: "obtained_date"}}, nil)
	for _, sample := range []struct{ owner, date, kind string }{{"Synthetic model", "2026.02.30", "unparseable"}, {"BMS unknown", "", "uncertain"}, {"Synthetic model", "", "uncertain"}} {
		t.Run(sample.owner+sample.date, func(t *testing.T) {
			draft := importUpload(t, f, l.ID, "progress_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, sample.owner, "standard_a", "BMS", "Country", sample.date), ""), http.StatusCreated)
			require.Len(t, draft.Items, 1)
			require.Equal(t, sample.kind, draft.Items[0].Conflicts[0].Type)
			if sample.date != "" {
				require.Equal(t, sample.date, draft.Items[0].Conflicts[0].Details.(map[string]any)["E3"])
			}
			f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "apply"}}}, http.StatusConflict, nil)
		})
	}
}

// Medium spec, task 12: report imports propose parts, attributes and explicit
// model composition; creating a missing certification requires a human decision.
func TestShelfImportCertificationReportProposesPartAndComposition(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	importRecordPlan(f, l, "certification_report", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}, {Column: "E", Field: "cell_part"}, {Column: "F", Field: "pack_model"}, {Column: "G", Field: "cluster_model"}, {Column: "H", Field: "number"}, {Column: "I", Field: "issued_date"}, {Column: "J", Field: "validity"}, {Column: "K", Field: "cloud_url"}}, map[string]any{"module_type": "module_a", "progress": "已完成"})
	data := importWorkbook(t, importRecordRow(3, "Synthetic model", "Customer spelling", "Container", "Country", " New cell ", " Pack original ", "Cluster original", "SYN-REPORT-1", "2026年10月20日", " Five years ", "https://example.com/report"), "")
	draft := importUpload(t, f, l.ID, "report_20261028.xlsx", "", data, http.StatusCreated)
	require.Len(t, draft.Items, 3)
	require.Equal(t, "uncertain", draft.Items[2].Conflicts[0].Type)
	replacement := draft.Items[2].Operation
	replacement.Type = "create_certification"
	replacement.Certification.Name = "Human chosen certificate name"
	decisions := []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "apply"}, {ItemID: draft.Items[1].ID, Action: "apply"}, {ItemID: draft.Items[2].ID, Action: "replace", Replacement: &replacement}}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, &draft)
	var part types.EntityDetail
	f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusOK, &part)
	require.Equal(t, " New cell ", part.Entity.CanonicalName)
	require.Equal(t, " Pack original ", part.Entity.Attributes["pack_model"])
	require.Equal(t, "Cluster original", part.Entity.Attributes["cluster_model"])
	var cert types.EntityDetail
	f.request(http.MethodGet, "/entities/"+draft.Items[2].EntityID, nil, http.StatusOK, &cert)
	record := cert.Entity.Attributes["certification"].(map[string]any)
	require.Equal(t, "已完成", record["progress"])
	require.Equal(t, "SYN-REPORT-1", record["number"])
	require.Equal(t, "2026-10-20", record["issued_date"])
	require.Equal(t, " Five years ", record["validity"])
	require.Equal(t, "https://example.com/report", record["cloud_url"])
	require.Equal(t, part.Entity.ID, record["cell_part_id"])
	var model types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &model)
	require.Len(t, model.Shelf.Composition[0].Parts, 3)
}

// Medium spec, task 12: overseas lists only report contradictions. Accepting
// those notices cannot change shelf status, identity, certification or revision.
func TestShelfImportOverseasSalesOnlyReportsContradictions(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "status", "type": "set_sale_statuses", "entity_id": shelfNode(n, "model"), "revision": 1, "effective_at": "2026-10-01T00:00:00Z", "sale_statuses": []any{map[string]any{"region": "region_a", "status": "DEV"}}}}}, http.StatusCreated, nil)
	importRecordPlan(f, l, "overseas_sales", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "sale_status", Key: "region_a"}}, nil)
	var before types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &before)
	draft := importUpload(t, f, l.ID, "overseas_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Synthetic model", "GA"), ""), http.StatusCreated)
	require.Len(t, draft.Items, 1)
	require.Equal(t, "import_notice", draft.Items[0].Operation.Type)
	require.Equal(t, "cross_table_contradiction", draft.Items[0].Conflicts[0].Type)
	details := draft.Items[0].Conflicts[0].Details.(map[string]any)
	require.Equal(t, "GA", details["expected"])
	require.Equal(t, "DEV", details["actual"])
	confirmImport(t, f, l.ID, draft)
	var after types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &after)
	require.Equal(t, before.Entity, after.Entity)
	matching := importUpload(t, f, l.ID, "overseas_20261029.xlsx", "", importWorkbook(t, importRecordRow(3, "Synthetic model", "DEV"), ""), http.StatusCreated)
	require.Empty(t, matching.Items[0].Conflicts)
	confirmImport(t, f, l.ID, matching)
}

// Medium spec, task 12 / ADR 0024: ATQ due stages come from the R&D deadline
// cell, not the display stage. New editions add rows and explicitly archive omissions.
func TestShelfImportATQStagesAndNewEditionArchive(t *testing.T) {
	f, l, n := checklistSetup(t)
	importRecordPlan(f, l, "atq", []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}, {Column: "C", Field: "stage_label"}, {Column: "D", Field: "name_en"}, {Column: "E", Field: "market", Key: "region_a"}, {Column: "F", Field: "owner"}, {Column: "G", Field: "source_version"}}, map[string]any{"product_line_id": shelfNode(n, "line")})
	first := importUpload(t, f, l.ID, "atq_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, " 原名资料 ", "初稿：Draft custom；终稿：Final custom", "Early custom", " Original English ", "required", " Owner ", "V1")+importRecordRow(4, "Removed document", "Final custom", "Draft custom", "Removed", "optional", "Team", "V1"), ""), http.StatusCreated)
	first = confirmImport(t, f, l.ID, first)
	var requirement types.EntityDetail
	f.request(http.MethodGet, "/entities/"+first.Items[0].EntityID, nil, http.StatusOK, &requirement)
	meta := requirement.Entity.Attributes["requirement"].(map[string]any)
	require.Equal(t, "draft_due", meta["draft_due_stage"])
	require.Equal(t, "final_due", meta["final_due_stage"])
	require.Equal(t, "Early custom", meta["stage_label"])
	require.Equal(t, "初稿：Draft custom；终稿：Final custom", meta["due_text"])
	require.Equal(t, " Original English ", meta["name_en"])
	second := importUpload(t, f, l.ID, "atq_20261029.xlsx", "", importWorkbook(t, importRecordRow(3, " 原名资料 ", "Final custom", "Early custom", " Original English ", "required", " Owner ", "V2")+importRecordRow(4, "Added document", "Draft custom", "Final custom", "Added", "not_needed", "Team", "V2"), ""), http.StatusCreated)
	missingID := ""
	for _, item := range second.Items {
		if item.Operation.Type == "missing_entity" {
			missingID = item.EntityID
		}
	}
	require.NotEmpty(t, missingID)
	var before types.EntityDetail
	f.request(http.MethodGet, "/entities/"+missingID, nil, http.StatusOK, &before)
	require.Nil(t, before.Entity.ArchivedAt)
	decisions := []types.ShelfDecision{}
	for _, item := range second.Items {
		action := "apply"
		if item.Operation.Type == "missing_entity" {
			action = "retire"
		}
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: action})
	}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+second.ID+"/confirm", types.ShelfChangesetReview{Revision: second.Revision, Decisions: decisions}, http.StatusOK, nil)
	f.request(http.MethodGet, "/entities/"+missingID, nil, http.StatusOK, &before)
	require.NotNil(t, before.Entity.ArchivedAt)
	checklist := checklistRead(f, l, shelfNode(n, "model"))["checklist"].([]any)
	require.Len(t, checklist, 2)
	names := []string{}
	for _, raw := range checklist {
		names = append(names, raw.(map[string]any)["requirement"].(map[string]any)["canonical_name"].(string))
	}
	require.ElementsMatch(t, []string{" 原名资料 ", "Added document"}, names)
	f.request(http.MethodGet, "/entities/"+requirement.Entity.ID, nil, http.StatusOK, &requirement)
	meta = requirement.Entity.Attributes["requirement"].(map[string]any)
	require.Empty(t, meta["draft_due_stage"])
	require.Equal(t, "V2", meta["source_version"])
}

// Medium spec, task 12: an explicitly styled Excel date is decoded using the
// workbook date system; the fictitious 1900 leap day remains an original-value conflict.
func TestShelfImportCertificationExcelDatesRetainInvalidSerial(t *testing.T) {
	f, l, _ := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}, {Column: "E", Field: "issued_date"}}, nil)
	style := `<styleSheet><fills><fill><patternFill patternType="none"/></fill></fills><cellXfs><xf fillId="0"/><xf fillId="0" numFmtId="14"/></cellXfs></styleSheet>`
	for _, sample := range []struct{ raw, want, conflict string }{{"46320", "2026-10-25", "uncertain"}, {"60", "", "unparseable"}, {"invalid-date", "", "unparseable"}} {
		t.Run(sample.raw, func(t *testing.T) {
			row := importRecordRow(3, "Synthetic model", "standard_a", "Container", "Country", sample.raw)
			if sample.raw != "invalid-date" {
				row = strings.Replace(row, `<c r="E3" t="inlineStr"><is><t>`+sample.raw+`</t></is></c>`, `<c r="E3" s="1"><v>`+sample.raw+`</v></c>`, 1)
			}
			draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", importWorkbook(t, row, "", style), http.StatusCreated)
			require.Equal(t, sample.conflict, draft.Items[0].Conflicts[0].Type)
			if sample.want != "" {
				require.Equal(t, sample.want, draft.Items[0].Operation.Certification.IssuedDate)
			} else {
				require.Equal(t, sample.raw, draft.Items[0].Conflicts[0].Details.(map[string]any)["E3"])
			}
		})
	}
}

// Medium spec, task 12: unclear deadline text and missing worksheets cannot
// silently archive an existing list. Explicit module bindings enter model checklists.
func TestShelfImportATQUncertainDeadlineAndIgnoredSheetPreserveRequirements(t *testing.T) {
	f, l, n := checklistSetup(t)
	importRecordPlan(f, l, "atq", []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}}, map[string]any{"module_type": "module_b"})
	first := confirmImport(t, f, l.ID, importUpload(t, f, l.ID, "atq_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Module document", "Draft custom（初稿）；Final custom（终稿）"), ""), http.StatusCreated))
	id := first.Items[0].EntityID
	require.Len(t, checklistRead(f, l, shelfNode(n, "model"))["checklist"], 1)
	uncertain := importUpload(t, f, l.ID, "atq_20261029.xlsx", "", importWorkbook(t, importRecordRow(3, "Module document", "When ready"), ""), http.StatusCreated)
	require.Len(t, uncertain.Items, 1)
	require.Equal(t, "uncertain", uncertain.Items[0].Conflicts[0].Type)
	require.Equal(t, "When ready", uncertain.Items[0].Conflicts[0].Details.(map[string]any)["B3"])
	var plans []types.ShelfImportPlan
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/import-plans", nil, http.StatusOK, &plans)
	def := plans[0].Definition
	def.Sheets = append(def.Sheets, types.ShelfImportSheet{Worksheet: "Absent", HeaderRows: 2, ModuleType: "module_b", Columns: def.Sheets[0].Columns})
	def.Sheets[0].Ignore = true
	def.Deadlines = map[string]types.ShelfImportDeadline{"When ready": {FinalStage: "final_due"}}
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic records", FileKind: "atq", Revision: 1, ShelfImportDefinition: def}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
	skipped := importUpload(t, f, l.ID, "atq_20261030.xlsx", "", importWorkbook(t, "", ""), http.StatusCreated)
	for _, item := range skipped.Items {
		require.NotEqual(t, "missing_entity", item.Operation.Type)
	}
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &detail)
	require.Nil(t, detail.Entity.ArchivedAt)
}

// Medium spec, task 12: repeated certification keys and confirmed model-cell
// separators must retain distinct review decisions rather than silently overwrite rows.
func TestShelfImportCertificationSplitsModelsAndFlagsRepeatedKeys(t *testing.T) {
	f, l, _ := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}, {Column: "E", Field: "progress"}}, nil)
	var plans []types.ShelfImportPlan
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/import-plans", nil, http.StatusOK, &plans)
	def := plans[0].Definition
	def.ModelSeparators = []string{";"}
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic records", FileKind: "certification_progress", Revision: 1, ShelfImportDefinition: def}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
	data := importWorkbook(t, importRecordRow(3, "Synthetic model;Synthetic single", "standard_a", "Container", "Country", "Planning")+importRecordRow(4, "Synthetic model", "Customer spelling", "Container", "Country", "Completed"), "")
	draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", data, http.StatusCreated)
	require.Len(t, draft.Items, 3)
	require.NotNil(t, draft.Items[0].Operation.Certification)
	require.NotNil(t, draft.Items[1].Operation.Certification)
	require.NotEqual(t, draft.Items[0].Operation.Certification.OwnerID, draft.Items[1].Operation.Certification.OwnerID)
	require.Contains(t, draft.Items[2].Conflicts[0].Message, "multiple rows")
	require.Equal(t, "E4", draft.Items[2].Operation.ImportLocation.Cells[4])
}

// Medium spec, task 12 / spec story 33: checkmark/cross certification matrices
// use confirmed flag meanings and completion spelling, and never create records.
func TestShelfImportOverseasCertificationFlagsOnlyCompare(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "effective_at": "2026-10-01T00:00:00Z", "certification": map[string]any{"name": "Completed original", "owner_id": shelfNode(n, "model"), "standard": "standard_a", "certification_object": "Container", "region": "Country", "progress": "完成原文"}}}}, http.StatusCreated, nil)
	importRecordPlan(f, l, "overseas_sales", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "certification", Key: "standard_a"}}, map[string]any{"progress": "完成原文", "check_values": map[string]bool{"√": true, "×": false}})
	for _, sample := range []struct {
		owner, flag   string
		contradiction bool
	}{{"Synthetic model", "√", false}, {"Synthetic model", "×", true}, {"Synthetic single", "√", true}} {
		draft := importUpload(t, f, l.ID, "overseas_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, sample.owner, sample.flag), ""), http.StatusCreated)
		require.Equal(t, "import_notice", draft.Items[0].Operation.Type)
		require.Equal(t, sample.contradiction, len(draft.Items[0].Conflicts) > 0)
		confirmImport(t, f, l.ID, draft)
	}
	var records []types.ShelfCertificationView
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/certifications", nil, http.StatusOK, &records)
	require.Len(t, records, 1)
	require.EqualValues(t, 1, records[0].Entity.Revision)
}

// Medium regression, task 12: discarding an invalid row must not erase the
// previous confirmed requirement from the next edition's disappearance baseline.
func TestShelfImportATQDiscardedInvalidRowRetainsDisappearanceBaseline(t *testing.T) {
	f, l, n := checklistSetup(t)
	importRecordPlan(f, l, "atq", []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}}, map[string]any{"product_line_id": shelfNode(n, "line")})
	first := confirmImport(t, f, l.ID, importUpload(t, f, l.ID, "atq_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Original document", "Final custom"), ""), http.StatusCreated))
	second := importUpload(t, f, l.ID, "atq_20261029.xlsx", "", importWorkbook(t, importRecordRow(3, "Original document", "Unclear deadline"), ""), http.StatusCreated)
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+second.ID+"/confirm", types.ShelfChangesetReview{Revision: second.Revision, Decisions: []types.ShelfDecision{{ItemID: second.Items[0].ID, Action: "discard"}}}, http.StatusOK, nil)
	third := importUpload(t, f, l.ID, "atq_20261030.xlsx", "", importWorkbook(t, importRecordRow(3, "New document", "Final custom"), ""), http.StatusCreated)
	missing := []string{}
	for _, item := range third.Items {
		if item.Operation.Type == "missing_entity" {
			missing = append(missing, item.EntityID)
		}
	}
	require.Equal(t, []string{first.Items[0].EntityID}, missing)
}

// Medium spec, task 12: multiple certification candidates require a corrected
// complete record so selecting a candidate cannot erase unmapped metadata.
func TestShelfImportCertificationAmbiguousKeyPreservesCandidates(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	for _, name := range []string{"First certificate", "Second certificate"} {
		f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "effective_at": "2026-10-01T00:00:00Z", "certification": map[string]any{"name": name, "owner_id": shelfNode(n, "model"), "standard": "standard_a", "certification_object": "Container", "region": "Country", "number": name}}}}, http.StatusCreated, nil)
	}
	importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}, {Column: "E", Field: "progress"}}, nil)
	draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Synthetic model", "standard_a", "Container", "Country", "Planning"), ""), http.StatusCreated)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
	details := draft.Items[0].Conflicts[0].Details.(map[string]any)
	require.EqualValues(t, 2, details["match_count"])
	require.Len(t, details["candidates"], 2)
	candidate := details["candidates"].([]any)[0].(map[string]any)
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "apply", TargetEntityID: candidate["id"].(string), TargetRevision: 1}}}, http.StatusConflict, nil)
	var records []types.ShelfCertificationView
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/certifications", nil, http.StatusOK, &records)
	for _, record := range records {
		require.Equal(t, record.Entity.CanonicalName, record.Certification.Number)
		require.EqualValues(t, 1, record.Entity.Revision)
	}
}

// Medium spec, task 12 / confirmed import plans: a worksheet's product-line
// scope cannot silently resolve an owner from another line.
func TestShelfImportCertificationOwnerMustMatchWorksheetLine(t *testing.T) {
	f, l, _ := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	var line types.Entity
	f.request(http.MethodPost, "/libraries/"+l.ID+"/entities", map[string]any{"name": "Other line", "kind": "product_line"}, http.StatusCreated, &line)
	importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}}, map[string]any{"product_line_id": line.ID})
	draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Synthetic model", "standard_a", "Container", "Country"), ""), http.StatusCreated)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
	require.Contains(t, draft.Items[0].Conflicts[0].Message, "product line")
}

// Medium spec, task 12 / ADR 0024: a Chinese-name column can identify an ATQ
// requirement and retain its bilingual metadata without duplicating column mappings.
func TestShelfImportATQBilingualNamesAndConfirmedDeadlineMapping(t *testing.T) {
	f, l, n := checklistSetup(t)
	importRecordPlan(f, l, "atq", []types.ShelfImportColumn{{Column: "A", Field: "name_zh"}, {Column: "B", Field: "name_en"}, {Column: "C", Field: "due_stages"}}, map[string]any{"product_line_id": shelfNode(n, "line")})
	var plans []types.ShelfImportPlan
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/import-plans", nil, http.StatusOK, &plans)
	def := plans[0].Definition
	def.Deadlines = map[string]types.ShelfImportDeadline{"研发评审前": {DraftStage: "early", FinalStage: "final_due"}}
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic records", FileKind: "atq", Revision: 1, ShelfImportDefinition: def}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
	draft := confirmImport(t, f, l.ID, importUpload(t, f, l.ID, "atq_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, " 原名图纸 ", " Original drawing ", "研发评审前"), ""), http.StatusCreated))
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusOK, &detail)
	require.Equal(t, " 原名图纸 ", detail.Entity.CanonicalName)
	meta := detail.Entity.Attributes["requirement"].(map[string]any)
	require.Equal(t, " 原名图纸 ", meta["name_zh"])
	require.Equal(t, " Original drawing ", meta["name_en"])
	require.Equal(t, "early", meta["draft_due_stage"])
	require.Equal(t, "final_due", meta["final_due_stage"])
}

// Medium regression, task 12: a formula's cached name cannot prove that an
// old requirement disappeared from an ATQ edition.
func TestShelfImportATQUntrustedIdentityCannotProposeArchive(t *testing.T) {
	f, l, n := checklistSetup(t)
	importRecordPlan(f, l, "atq", []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}}, map[string]any{"product_line_id": shelfNode(n, "line")})
	confirmImport(t, f, l.ID, importUpload(t, f, l.ID, "atq_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Original document", "Final custom"), ""), http.StatusCreated))
	row := importRecordRow(3, "Untrusted name", "Final custom")
	row = strings.Replace(row, `<c r="A3" t="inlineStr"><is><t>Untrusted name</t></is></c>`, `<c r="A3"><f>CONCAT("Untrusted", " name")</f><v>Untrusted name</v></c>`, 1)
	draft := importUpload(t, f, l.ID, "atq_20261029.xlsx", "", importWorkbook(t, row, ""), http.StatusCreated)
	require.Len(t, draft.Items, 1)
	require.Equal(t, "unparseable", draft.Items[0].Conflicts[0].Type)
	require.Contains(t, draft.Items[0].Operation.ImportLocation.Values, "A3.formula")
}

// Medium spec, task 12: an AC project's confirmed model separators can attach
// one certified cell part to multiple models without duplicating the part.
func TestShelfImportReportProjectModelsShareOnePart(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	importRecordPlan(f, l, "certification_report", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}, {Column: "E", Field: "cell_part"}, {Column: "F", Field: "model"}}, map[string]any{"owner_kind": "part", "module_type": "module_a", "progress": "Completed"})
	var plans []types.ShelfImportPlan
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/import-plans", nil, http.StatusOK, &plans)
	def := plans[0].Definition
	def.ModelSeparators = []string{";"}
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic records", FileKind: "certification_report", Revision: 1, ShelfImportDefinition: def}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
	draft := importUpload(t, f, l.ID, "report_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Shared cell", "standard_a", "Cell", "Country", "Shared cell", "Synthetic model;Synthetic single"), ""), http.StatusCreated)
	require.Len(t, draft.Items, 4)
	require.Equal(t, "part", draft.Items[0].Operation.Entity.Kind)
	replacement := draft.Items[3].Operation
	replacement.Type = "create_certification"
	replacement.Certification.Name = "Shared cell certificate"
	decisions := []types.ShelfDecision{}
	for _, item := range draft.Items {
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: "apply"})
	}
	decisions[3].Action = "replace"
	decisions[3].Replacement = &replacement
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, &draft)
	partID := draft.Items[0].EntityID
	for _, model := range []string{"model", "single"} {
		var detail types.EntityDetail
		f.request(http.MethodGet, "/entities/"+shelfNode(n, model), nil, http.StatusOK, &detail)
		found := false
		for _, group := range detail.Shelf.Composition {
			for _, part := range group.Parts {
				found = found || part.ID == partID
			}
		}
		require.True(t, found)
	}
}

// Medium regression, task 12 / ADR 0023: certificates for complementary part
// options collectively cover a model and must not be reported as a contradiction.
func TestShelfImportOverseasChecksCombinedCertificationCoverage(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	importRecordPlan(f, l, "overseas_sales", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "certification", Key: "standard_a"}}, map[string]any{"progress": "Completed", "check_values": map[string]bool{"√": true, "×": false}})
	data := importWorkbook(t, importRecordRow(3, "Synthetic model", "√"), "")
	for _, part := range []string{"a", "b"} {
		f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Certificate for " + part, "owner_id": shelfNode(n, part), "standard": "standard_a", "certification_object": "Cell", "region": "Country", "progress": "Completed"}}}}, http.StatusCreated, nil)
		draft := importUpload(t, f, l.ID, "overseas.xlsx", time.Now().UTC().Format(time.RFC3339Nano), data, http.StatusCreated)
		if part == "a" {
			require.Len(t, draft.Items[0].Conflicts, 1)
			require.Contains(t, draft.Items[0].Conflicts[0].Details.(map[string]any)["field"], "partial_options")
		} else {
			require.Empty(t, draft.Items[0].Conflicts)
		}
	}
}
