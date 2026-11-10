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

// Medium spec, task 22 / ADR 0024: identical requirement names belong to
// independent lists. All writes and observations use the shelf HTTP boundary.
func TestShelfRequirementIdentityIsScopedToList(t *testing.T) {
	f, l, _ := checklistSetup(t)
	path := "/libraries/" + l.ID + "/shelf/changesets"
	create := func(list, owner string, status int) types.ShelfChangeset {
		var result types.ShelfChangeset
		var output any
		if status == http.StatusCreated {
			output = &result
		}
		f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "req", "type": "create_entity", "entity": map[string]any{"kind": "document_requirement", "name": "参数表", "code": "SYN-REQ", "aliases": []string{"合成别名"}, "attributes": map[string]any{"requirement": map[string]any{"list_name": list, "final_due_stage": "final_due", "owner": owner}}}}}}, status, output)
		return result
	}
	first := create("01 清单", "甲", http.StatusCreated)
	second := create("02 清单", "乙", http.StatusCreated)
	require.NotEqual(t, first.Items[0].EntityID, second.Items[0].EntityID)
	create("01 清单", "丙", http.StatusConflict)
	create("", "未提供清单", http.StatusConflict)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+first.Items[0].EntityID, nil, http.StatusOK, &detail)
	require.Equal(t, "甲", detail.Entity.Attributes["requirement"].(map[string]any)["owner"])
	var proposed types.ShelfChangeset
	f.request(http.MethodPost, path, types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Operations: []types.ShelfOperation{{ID: "req", Type: "create_entity", Entity: &types.ManagedEntityInput{Kind: "document_requirement", Name: "另一个名称", Attributes: map[string]any{"requirement": map[string]any{"list_name": "03 清单", "final_due_stage": "final_due"}}}}}}, http.StatusCreated, &proposed)
	f.request(http.MethodPost, path+"/"+proposed.ID+"/confirm", types.ShelfChangesetReview{Revision: proposed.Revision, Decisions: []types.ShelfDecision{{ItemID: "req", Action: "same_entity", TargetEntityID: first.Items[0].EntityID, TargetRevision: 1}}}, http.StatusConflict, nil)

}

// Medium regression, task 22: the synthetic 01/02 worksheets reproduce the
// cross-list overwrite without containing any customer workbook content.
func TestShelfImportRequirementsMatchAndDisappearWithinTheirList(t *testing.T) {
	f, l, n := checklistSetup(t)
	columns := []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}, {Column: "C", Field: "owner"}, {Column: "D", Field: "source_version"}, {Column: "E", Field: "market", Key: "market_a"}}
	sheets := []types.ShelfImportSheet{}
	for _, sheet := range []string{"01", "02"} {
		sheets = append(sheets, types.ShelfImportSheet{Worksheet: sheet, HeaderRows: 2, ProductLineID: shelfNode(n, "line"), Columns: columns})
	}
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Synthetic two lists", FileKind: "atq", ShelfImportDefinition: types.ShelfImportDefinition{Sheets: sheets}}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
	first := importUpload(t, f, l.ID, "lists_20261028.xlsx", "", requirementListsWorkbook(t, importRecordRow(3, "参数表", "Draft custom", "甲", "V1", "required"), importRecordRow(3, "参数表", "Final custom", "乙", "V2", "optional")), http.StatusCreated)
	for _, item := range first.Items {
		for _, conflict := range item.Conflicts {
			require.Equal(t, "new", conflict.Type, conflict.Message)
		}
	}
	first = confirmImport(t, f, l.ID, first)
	ids := map[string]string{}
	for _, item := range first.Items {
		if item.Operation.Type != "create_entity" {
			continue
		}
		var detail types.EntityDetail
		f.request(http.MethodGet, "/entities/"+item.EntityID, nil, http.StatusOK, &detail)
		meta := detail.Entity.Attributes["requirement"].(map[string]any)
		list := meta["list_name"].(string)
		ids[list] = detail.Entity.ID
		if list == "map / 01" {
			require.Equal(t, "甲", meta["owner"])
			require.Equal(t, "draft_due", meta["final_due_stage"])
			require.Equal(t, "V1", meta["source_version"])
			require.Equal(t, "required", meta["markets"].(map[string]any)["market_a"])
		} else {
			require.Equal(t, "map / 02", list)
			require.Equal(t, "乙", meta["owner"])
			require.Equal(t, "final_due", meta["final_due_stage"])
			require.Equal(t, "V2", meta["source_version"])
			require.Equal(t, "optional", meta["markets"].(map[string]any)["market_a"])
		}
	}
	require.Len(t, ids, 2)
	second := importUpload(t, f, l.ID, "lists_20261029.xlsx", "", requirementListsWorkbook(t, "", importRecordRow(3, "参数表", "Final custom", "乙", "V3", "optional")), http.StatusCreated)
	missing := []string{}
	for _, item := range second.Items {
		if item.Operation.Type == "missing_entity" {
			missing = append(missing, item.EntityID)
		}
		if item.Operation.Type == "update_entity" {
			require.Equal(t, ids["map / 02"], item.EntityID)
		}
	}
	require.Equal(t, []string{ids["map / 01"]}, missing)
	duplicate := importUpload(t, f, l.ID, "lists_20261030.xlsx", "", requirementListsWorkbook(t, importRecordRow(3, "参数表", "Draft custom", "甲", "V1", "required")+importRecordRow(4, "参数表", "Draft custom", "丙", "V1", "required"), importRecordRow(3, "参数表", "Final custom", "乙", "V2", "optional")), http.StatusCreated)
	conflicts := 0
	for _, item := range duplicate.Items {
		for _, conflict := range item.Conflicts {
			if conflict.Type == "uncertain" {
				conflicts++
				require.Equal(t, "01", item.Operation.ImportLocation.Worksheet)
			}
		}
	}
	require.Equal(t, 1, conflicts)
}

func requirementListsWorkbook(t *testing.T, first, second string) []byte {
	t.Helper()
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	files := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="01" r:id="rId1"/><sheet name="02" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="worksheets/sheet2.xml"/></Relationships>`,
	}
	for i, rows := range []string{first, second} {
		files[fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)] = `<worksheet><sheetData>` + rows + `</sheetData></worksheet>`
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

// Medium spec, task 22: one draft and final file can satisfy multiple list
// requirements, while deadlines and reclassification remain independent.
func TestShelfDocumentFulfillsMultipleRequirementsIndependently(t *testing.T) {
	f, l, n := checklistSetup(t)
	path := "/libraries/" + l.ID + "/shelf/changesets"
	ops := []any{map[string]any{"id": "stage", "type": "update_entity", "entity_id": shelfNode(n, "model"), "patch": map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "draft_due"}}}}
	for _, r := range []struct{ id, list, final string }{{"one", "01", "draft_due"}, {"two", "02", "final_due"}} {
		ops = append(ops, map[string]any{"id": r.id, "type": "create_entity", "entity": map[string]any{"kind": "document_requirement", "name": "参数表", "attributes": map[string]any{"requirement": map[string]any{"list_name": r.list, "final_due_stage": r.final}}}}, map[string]any{"id": r.id + "_bind", "type": "save_relation", "relation": map[string]any{"subject_id": "$" + r.id, "object_id": shelfNode(n, "line"), "relation_type": "applies_to"}})
	}
	ops = append(ops, map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": "共用参数表", "owner_id": shelfNode(n, "product"), "requirement_ids": []string{"$two", "$one"}, "cloud_url": "https://example.invalid/shared", "stage": "draft"}})
	var saved types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": ops}, http.StatusCreated, &saved)
	one, two, file := shelfNode(saved, "one"), shelfNode(saved, "two"), shelfNode(saved, "file")
	states := func() map[string]string {
		result := map[string]string{}
		for _, raw := range checklistRead(f, l, shelfNode(n, "model"))["checklist"].([]any) {
			row := raw.(map[string]any)
			result[row["requirement"].(map[string]any)["id"].(string)] = row["status"].(string)
		}
		return result
	}
	require.Equal(t, map[string]string{one: "missing", two: "draft_only"}, states())
	var versions []map[string]any
	f.request(http.MethodGet, "/entities/"+file+"/document-versions", nil, http.StatusOK, &versions)
	require.ElementsMatch(t, []any{one, two}, versions[0]["requirement_ids"])
	product := checklistRead(f, l, shelfNode(n, "product"))
	require.Len(t, product["documents"], 1)
	require.Len(t, product["documents"].([]any)[0].(map[string]any)["requirements"], 2)
	var next types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "final", "type": "add_document_version", "document": map[string]any{"owner_id": shelfNode(n, "product"), "requirement_ids": []string{one, two}, "cloud_url": "https://example.invalid/shared-final", "stage": "final"}}}}, http.StatusCreated, &next)
	require.Equal(t, file, next.Items[0].EntityID)
	require.Equal(t, map[string]string{one: "delivered", two: "delivered"}, states())
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "classify", "type": "classify_document", "entity_id": file, "revision": 2, "classification": map[string]any{"owner_id": shelfNode(n, "product"), "requirement_ids": []string{two}}}}}, http.StatusCreated, nil)
	require.Equal(t, map[string]string{one: "missing", two: "delivered"}, states())
	var page map[string]any
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/documents?group=requirements", nil, http.StatusOK, &page)
	require.EqualValues(t, 1, page["total"])
	item := page["items"].([]any)[0].(map[string]any)
	require.Len(t, item["requirements"], 1)
	require.Equal(t, two, item["requirements"].([]any)[0].(map[string]any)["id"])
	for _, ids := range [][]string{{one, one}, {one, shelfNode(n, "model")}, {one, "missing"}} {
		f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "invalid", "type": "classify_document", "entity_id": file, "revision": 3, "classification": map[string]any{"owner_id": shelfNode(n, "product"), "requirement_ids": ids}}}}, http.StatusConflict, nil)
	}
	require.Equal(t, map[string]string{one: "missing", two: "delivered"}, states())
}

// Medium spec, task 22: a parser rule may match any selected requirement;
// rule order, rather than selection order, remains authoritative.
func TestShelfDocumentMultipleRequirementsSelectParserByRuleOrder(t *testing.T) {
	f, l, n := checklistSetup(t)
	one := checklistRequirement(t, f, l, "First parser requirement", "", "final_due")
	two := checklistRequirement(t, f, l, "Second parser requirement", "", "final_due")
	l.ShelfConfig.Parsers = []types.ShelfParser{{Key: "first", Name: "First", Type: "none"}, {Key: "second", Name: "Second", Type: "none"}}
	l.ShelfConfig.ParserRules = []types.ShelfParserRule{{ParserKey: "second", RequirementID: two.ID}, {ParserKey: "first", RequirementID: one.ID}}
	l.ShelfConfig.DefaultParser = "first"
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, nil)
	attachment := shelfUpload(t, f, l.ID, "Synthetic original", http.StatusCreated)
	var saved types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": "Synthetic shared parse", "owner_id": shelfNode(n, "model"), "requirement_ids": []string{one.ID, two.ID}, "attachment_id": attachment.ID, "stage": "final"}}}}, http.StatusCreated, &saved)
	var versions []types.ShelfDocumentVersion
	f.request(http.MethodGet, "/entities/"+saved.Items[0].EntityID+"/document-versions", nil, http.StatusOK, &versions)
	require.Equal(t, "second", versions[0].ParserKey)
}
