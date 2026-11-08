package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func saleFactsByRegion(t *testing.T, detail types.ShelfNodeDetail) map[string]types.ShelfFact {
	t.Helper()
	result := map[string]types.ShelfFact{}
	for _, fact := range detail.Facts {
		if fact.Field == "sale_statuses" {
			var status types.ShelfSaleStatus
			require.NoError(t, types.DecodeShelfValue(fact.Value, &status))
			result[status.Region] = fact
		}
	}
	return result
}

// Medium regression, task 26: regional evidence is independent of record and
// condition ordering, and scheduled changes only claim the edited region.
func TestShelfSaleStatusEvidenceTracksRegionalConditions(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	library.ShelfConfig.Regions = append(library.ShelfConfig.Regions, types.ShelfOption{Key: "region_b", Name: "Region B"})
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": library.ShelfConfig}, http.StatusOK, nil)
	id := shelfNode(nodes, "model")
	path := "/libraries/" + library.ID + "/shelf/nodes/" + id
	read := func(query string) types.ShelfNodeDetail {
		var detail types.ShelfNodeDetail
		f.request(http.MethodGet, path+query, nil, http.StatusOK, &detail)
		return detail
	}
	conditions := []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "b"), shelfNode(nodes, "a")}}, {ModuleType: "module_b", PartIDs: []string{shelfNode(nodes, "fixed")}}}
	statuses := []types.ShelfSaleStatus{{Region: "region_a", Status: "GA", Conditions: conditions}, {Region: "region_b", Status: "DEV"}}
	changesets := "/libraries/" + library.ID + "/shelf/changesets"
	write := func(source types.OperationSource, at *time.Time) types.ShelfChangeset {
		var saved types.ShelfChangeset
		f.request(http.MethodPost, changesets, types.ShelfChangesetInput{Source: source, Confirmed: true, Operations: []types.ShelfOperation{{ID: "statuses", Type: "set_sale_statuses", EntityID: id, Revision: read("").Entity.Revision, EffectiveAt: at, SaleStatuses: &statuses}}}, http.StatusCreated, &saved)
		return saved
	}
	write(types.OperationSource{Type: "web"}, nil)
	original := saleFactsByRegion(t, read(""))
	slices.Reverse(conditions[0].PartIDs)
	slices.Reverse(conditions)
	slices.Reverse(statuses)
	write(types.OperationSource{Type: "agent", AgentName: "Reorder editor"}, nil)
	reordered := saleFactsByRegion(t, read(""))
	for region, fact := range original {
		require.Equal(t, fact.Evidence, reordered[region].Evidence)
	}
	future := time.Date(2090, 1, 1, 0, 0, 0, 0, time.UTC)
	statuses[0].PlannedEOM = true
	scheduled := write(types.OperationSource{Type: "agent", AgentName: "Schedule editor"}, &future)
	current := saleFactsByRegion(t, read(""))
	require.Equal(t, reordered, current)
	combination, err := json.Marshal(map[string]string{"module_a": shelfNode(nodes, "a")})
	require.NoError(t, err)
	selected := saleFactsByRegion(t, read("?as_of=2090-02-01T00:00:00Z&combination="+url.QueryEscape(string(combination))))
	require.Equal(t, original["region_a"].Evidence, selected["region_a"].Evidence)
	require.Equal(t, scheduled.ID, selected["region_b"].Evidence.ChangesetID)
	require.True(t, future.Equal(selected["region_b"].Evidence.EffectiveAt))
}

// Medium regression, task 26 / ADR 0023: editing one imported region keeps
// the other region's file, cells, snapshot and submitting log intact.
func TestShelfSaleStatusEvidencePreservesUneditedRegions(t *testing.T) {
	for _, source := range []types.OperationSource{{Type: "web"}, {Type: "agent", AgentName: "Synthetic editor"}} {
		t.Run(source.Type, func(t *testing.T) {
			f, library := importSetup(t)
			library.ShelfConfig.Regions = append(library.ShelfConfig.Regions, types.ShelfOption{Key: "region_b", Name: "Region B"})
			f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": library.ShelfConfig}, http.StatusOK, nil)
			var plans []types.ShelfImportPlan
			f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/import-plans", nil, http.StatusOK, &plans)
			plan := plans[0]
			plan.Definition.Sheets[0].Columns = append(plan.Definition.Sheets[0].Columns, types.ShelfImportColumn{Column: "F", Field: "sale_status", Key: "region_b"})
			changesets := "/libraries/" + library.ID + "/shelf/changesets"
			f.request(http.MethodPost, changesets, types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &types.ShelfImportPlanInput{Key: plan.Key, Name: plan.Name, FileKind: plan.FileKind, Revision: plan.Revision, ShelfImportDefinition: plan.Definition}}}}, http.StatusCreated, nil)
			rows := strings.Replace(importRow(3, "Map platform", "Map product", "Regional model", "GA (Demo)", "12"), "</row>", `<c r="F3" t="inlineStr"><is><t>DEV</t></is></c></row>`, 1)
			batch := confirmImport(t, f, library.ID, importUpload(t, f, library.ID, "synthetic_20250101.xlsx", "", importWorkbook(t, rows, ""), http.StatusCreated))
			id := ""
			for _, item := range batch.Items {
				if item.Operation.Entity != nil && item.Operation.Entity.Kind == "model" {
					id = item.EntityID
				}
			}
			require.NotEmpty(t, id)
			path := "/libraries/" + library.ID + "/shelf/nodes/" + id
			read := func(query string) types.ShelfNodeDetail {
				var detail types.ShelfNodeDetail
				f.request(http.MethodGet, path+query, nil, http.StatusOK, &detail)
				return detail
			}
			detail := read("")
			original := saleFactsByRegion(t, detail)
			require.Len(t, original, 2)
			require.Equal(t, batch.ID, original["region_b"].Evidence.ChangesetID)
			require.Equal(t, batch.Source.ImportBatchID, original["region_b"].Evidence.Source.ImportBatchID)
			require.Equal(t, "synthetic_20250101.xlsx", original["region_b"].Evidence.Source.FileName)
			require.Equal(t, "Map", original["region_b"].Evidence.Source.Worksheet)
			require.Contains(t, original["region_b"].Evidence.Source.Cells, "F3")
			require.NotEmpty(t, original["region_b"].Evidence.OperationLogID)
			require.NotEmpty(t, original["region_b"].Evidence.SnapshotID)
			statuses := detail.Shelf.SaleStatuses
			statuses[0].Notes = "Manually revised note"
			if source.Type == "agent" {
				statuses[0].Status = "EOM"
			}
			var edited types.ShelfChangeset
			f.request(http.MethodPost, changesets, types.ShelfChangesetInput{Source: source, Confirmed: true, Operations: []types.ShelfOperation{{ID: "note", Type: "set_sale_statuses", EntityID: id, Revision: detail.Entity.Revision, SaleStatuses: &statuses}}}, http.StatusCreated, &edited)
			detail = read("")
			current := saleFactsByRegion(t, detail)
			require.Equal(t, original["region_b"].Evidence, current["region_b"].Evidence)
			require.Equal(t, edited.ID, current["region_a"].Evidence.ChangesetID)
			require.Equal(t, source, current["region_a"].Evidence.Source)
			require.NotEqual(t, original["region_a"].Evidence.OperationLogID, current["region_a"].Evidence.OperationLogID)
			var toolDetail types.ShelfNodeDetail
			mcpCall(t, f.connect(t, "2025-11-25"), "shelf_get", map[string]any{"library_id": library.ID, "node_id": id, "as_of": detail.AsOf.Format(time.RFC3339Nano)}, &toolDetail)
			require.Equal(t, current, saleFactsByRegion(t, toolDetail))
			detail = read("?as_of=2025-02-01T00:00:00Z")
			require.Equal(t, original, saleFactsByRegion(t, detail))
		})
	}
}

// Medium regression, task 26: restoring a removed region is a new fact even
// when its value matches the earlier import, while other regions keep theirs.
func TestShelfSaleStatusEvidenceRestartsAfterRegionRemoval(t *testing.T) {
	f, library := newProductShelfFixture(t)
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": types.ShelfConfig{Regions: []types.ShelfOption{{Key: "region_a", Name: "Region A"}, {Key: "region_b", Name: "Region B"}}}}, http.StatusOK, nil)
	fileDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	statuses := []types.ShelfSaleStatus{{Region: "region_a", Status: "GA"}, {Region: "region_b", Status: "DEV"}}
	var imported types.ShelfChangeset
	changesets := "/libraries/" + library.ID + "/shelf/changesets"
	f.request(http.MethodPost, changesets, types.ShelfChangesetInput{Source: types.OperationSource{Type: "import", ImportBatchID: "synthetic-regions", FileDate: &fileDate}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "model", Type: "create_entity", Entity: &types.ManagedEntityInput{Kind: "model", Name: "Regional model", Attributes: map[string]any{"sale_statuses": statuses}}}}}, http.StatusCreated, &imported)
	id := shelfNode(imported, "model")
	path := "/libraries/" + library.ID + "/shelf/nodes/" + id
	read := func(query string) types.ShelfNodeDetail {
		var detail types.ShelfNodeDetail
		f.request(http.MethodGet, path+query, nil, http.StatusOK, &detail)
		return detail
	}
	original := saleFactsByRegion(t, read(""))
	for i, values := range [][]types.ShelfSaleStatus{{statuses[1]}, statuses} {
		at := fileDate.AddDate(0, i+1, 0)
		var saved types.ShelfChangeset
		f.request(http.MethodPost, changesets, types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "statuses", Type: "set_sale_statuses", EntityID: id, Revision: read("").Entity.Revision, EffectiveAt: &at, SaleStatuses: &values}}}, http.StatusCreated, &saved)
		current := saleFactsByRegion(t, read(""))
		require.Equal(t, original["region_b"].Evidence, current["region_b"].Evidence)
		if i == 0 {
			require.NotContains(t, current, "region_a")
		} else {
			require.Equal(t, saved.ID, current["region_a"].Evidence.ChangesetID)
		}
	}
	require.Equal(t, original, saleFactsByRegion(t, read("?as_of=2025-01-15T00:00:00Z")))
}
