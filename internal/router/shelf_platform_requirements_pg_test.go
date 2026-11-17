package router

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
)

func checklistNames(f *managedNamesFixture, l types.Library, model string) []string {
	names := []string{}
	for _, raw := range checklistRead(f, l, model)["checklist"].([]any) {
		names = append(names, raw.(map[string]any)["requirement"].(map[string]any)["canonical_name"].(string))
	}
	return names
}

// Medium spec, ADR 0024 (2026-11-17): requirements apply to product lines or
// platforms. A model gets the union of both layers; a sibling platform's model
// does not see the other platform's list. Shelves whose stored applies_to rule
// predates platform targets accept them without a data migration.
func TestShelfRequirementsApplyToPlatforms(t *testing.T) {
	f, l, n := checklistSetup(t)
	line, platform := shelfNode(n, "line"), shelfNode(n, "platform")
	var other types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "platform", "type": "create_entity", "entity": map[string]any{"kind": "platform", "name": "Other platform"}},
		map[string]any{"id": "product", "type": "create_entity", "entity": map[string]any{"kind": "product", "name": "Other product"}},
		map[string]any{"id": "model", "type": "create_entity", "entity": map[string]any{"kind": "model", "name": "Other model"}},
		map[string]any{"id": "platform_line", "type": "save_relation", "relation": map[string]any{"subject_id": "$platform", "object_id": line, "relation_type": "belongs_to"}},
		map[string]any{"id": "product_platform", "type": "save_relation", "relation": map[string]any{"subject_id": "$product", "object_id": "$platform", "relation_type": "belongs_to"}},
		map[string]any{"id": "model_product", "type": "save_relation", "relation": map[string]any{"subject_id": "$model", "object_id": "$product", "relation_type": "belongs_to"}},
	}}, http.StatusCreated, &other)
	require.NoError(t, f.db.Exec(`UPDATE library_relation_types SET object_kinds='["product_line"]' WHERE library_id=? AND name='applies_to'`, l.ID).Error)

	lineRequirement := checklistRequirement(t, f, l, "Line requirement", "", "final_due")
	checklistBind(f, l, lineRequirement, line)
	platformRequirement := checklistRequirement(t, f, l, "Platform requirement", "", "final_due")
	checklistBind(f, l, platformRequirement, platform)
	for _, target := range []string{shelfNode(n, "product"), shelfNode(n, "model")} {
		f.request(http.MethodPost, "/libraries/"+l.ID+"/relations", map[string]any{"subject_id": platformRequirement.ID, "object_id": target, "relation_type": "applies_to"}, http.StatusBadRequest, nil)
	}

	require.ElementsMatch(t, []string{"Line requirement", "Platform requirement"}, checklistNames(f, l, shelfNode(n, "model")))
	require.Equal(t, []string{"Line requirement"}, checklistNames(f, l, shelfNode(other, "model")))
}

// Medium spec, ADR 0024: an ATQ worksheet binds its requirements to every
// confirmed target; older plans with one product_line_id keep working, and a
// worksheet cannot name both forms.
func TestShelfImportATQBindsPlatformTargets(t *testing.T) {
	f, l, n := checklistSetup(t)
	line, platform := shelfNode(n, "line"), shelfNode(n, "platform")
	for _, invalid := range []map[string]any{
		{"apply_to_ids": []string{shelfNode(n, "product")}},
		{"apply_to_ids": []string{platform, platform}},
		{"apply_to_ids": []string{platform}, "product_line_id": line},
		{},
	} {
		sheet := map[string]any{"worksheet": "Map", "header_rows": 2, "columns": []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}}}
		for k, v := range invalid {
			sheet[k] = v
		}
		var draft types.ShelfChangeset
		f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "plan", "type": "save_import_plan", "import_plan": map[string]any{"key": "map", "name": "Synthetic records", "file_kind": "atq", "sheets": []any{sheet}}}}}, http.StatusCreated, &draft)
		require.NotEmpty(t, draft.Items[0].Conflicts, invalid)
	}

	importRecordPlan(f, l, "atq", []types.ShelfImportColumn{{Column: "A", Field: "name"}, {Column: "B", Field: "due_stages"}}, map[string]any{"apply_to_ids": []string{line, platform}})
	draft := confirmImport(t, f, l.ID, importUpload(t, f, l.ID, "atq_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Shared document", "Final custom"), ""), http.StatusCreated))
	var relations []types.Relation
	require.NoError(t, f.db.Where("library_id=? AND relation_type='applies_to' AND valid_to IS NULL", l.ID).Find(&relations).Error)
	targets := []string{}
	for _, relation := range relations {
		require.Equal(t, draft.Items[0].EntityID, relation.SubjectID)
		targets = append(targets, relation.ObjectID)
	}
	require.ElementsMatch(t, []string{line, platform}, targets)
	require.Equal(t, []string{"Shared document"}, checklistNames(f, l, shelfNode(n, "model")))
}
