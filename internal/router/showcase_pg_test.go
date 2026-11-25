package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Medium spec, shelf-showcase task 01: selection uses the deployment setting
// or the only enabled shelf in the personal space.
func TestShowcaseShelfSelectsLibrary(t *testing.T) {
	f := &managedNamesFixture{mcpFixture: newMCPFixture(t), t: t}
	t.Setenv("RELIQUARY_SHOWCASE_LIBRARY", "")
	require.JSONEq(t, `{"error":"showcase_library_not_found"}`, showcaseRequest(t, f, http.StatusNotFound))

	var first, second types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{
		"name": "First shelf", "directory": "first-shelf", "entity_mode": "managed", "shelf_enabled": true,
	}, http.StatusCreated, &first)
	requireShowcaseLibrary(t, showcaseRequest(t, f, http.StatusOK), first)

	f.request(http.MethodPost, "/libraries", map[string]any{
		"name": "Second shelf", "directory": "second-shelf", "entity_mode": "managed", "shelf_enabled": true,
	}, http.StatusCreated, &second)
	require.JSONEq(t, `{"error":"showcase_library_ambiguous"}`, showcaseRequest(t, f, http.StatusConflict))

	t.Setenv("RELIQUARY_SHOWCASE_LIBRARY", second.Directory)
	requireShowcaseLibrary(t, showcaseRequest(t, f, http.StatusOK), second)

	t.Setenv("RELIQUARY_SHOWCASE_LIBRARY", first.ID)
	requireShowcaseLibrary(t, showcaseRequest(t, f, http.StatusOK), first)

	t.Setenv("RELIQUARY_SHOWCASE_LIBRARY", f.library.Directory)
	require.JSONEq(t, `{"error":"showcase_library_not_found"}`, showcaseRequest(t, f, http.StatusNotFound))
}

// Medium spec, shelf-showcase task 01: the full HTTP response exposes only
// the display fields, even when internal owners, sources and files exist.
func TestShowcaseShelfReturnsOnlyDisplayFields(t *testing.T) {
	t.Setenv("RELIQUARY_SHOWCASE_LIBRARY", "")
	f, library, nodes := checklistSetup(t)
	library.ShelfConfig.Regions[0].Aliases = []string{"Internal region alias"}
	library.ShelfConfig.Standards = []types.ShelfStandard{{Key: "iec", Name: "IEC 62619"}}
	path := "/libraries/" + library.ID
	f.request(http.MethodPut, path, map[string]any{"shelf_config": library.ShelfConfig}, http.StatusOK, &library)

	lineID, modelID := shelfNode(nodes, "line"), shelfNode(nodes, "model")
	f.request(http.MethodPatch, "/entities/"+shelfNode(nodes, "product"), map[string]any{
		"revision": 1, "attributes": map[string]string{"current_stage": "final_due"},
	}, http.StatusOK, nil)

	requirement := checklistRequirement(t, f, library, "系统规格书", "", "final_due")
	checklistBind(f, library, requirement, lineID)
	checklistFile(f, library, modelID, requirement.ID, "final")
	f.request(http.MethodPost, path+"/shelf/changesets", types.ShelfChangesetInput{
		Source: types.OperationSource{Type: "web"}, Confirmed: true,
		Operations: []types.ShelfOperation{
			{ID: "statuses", Type: "set_sale_statuses", EntityID: modelID, Revision: 1, SaleStatuses: &[]types.ShelfSaleStatus{
				{Region: "region_a", Status: "GA", Raw: "GA (original)", Notes: "Regional note", PlannedEOM: true,
					Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "a")}}}},
			}},
			{ID: "certificate", Type: "create_certification", Certification: &types.ShelfCertificationInput{
				Name: "Synthetic certificate",
				ShelfCertification: types.ShelfCertification{
					OwnerID: shelfNode(nodes, "platform"), Standard: "iec", CertificationObject: "Container",
					Region: "region_a", Progress: "已取证", Number: "Internal report number",
				},
			}},
		},
	}, http.StatusCreated, nil)

	before := time.Now()
	body := showcaseRequest(t, f, http.StatusOK)
	var timestamp struct {
		AsOf time.Time `json:"as_of"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &timestamp))
	require.WithinRange(t, timestamp.AsOf, before, time.Now())
	expected := fmt.Sprintf(`{
		"library": {"id": %q, "name": %q},
		"as_of": %q,
		"regions": [{"key":"region_a","name":"Region A"}],
		"product_lines": [{"id":%q,"name":"Synthetic line"}],
		"models": [
			{
				"id":%q,"name":"Synthetic model","product_name":"Synthetic product","platform":"Synthetic platform",
				"product_line":{"id":%q,"name":"Synthetic line"},"current_stage":"final_due",
				"sale_statuses":[{"region":"region_a","region_name":"Region A","status":"GA","raw":"GA (original)","notes":"Regional note","planned_eom":true,"conditional":true}],
				"checklist":[{"name":"系统规格书","status":"delivered","status_label":"已交","gap":false,"has_file":true}],
				"certifications":[{"standard_name":"IEC 62619","status":"obtained","region_name":"Region A","origin":"ancestor"}]
			},
			{
				"id":%q,"name":"Synthetic single","product_name":"Synthetic product","platform":"Synthetic platform",
				"product_line":{"id":%q,"name":"Synthetic line"},"current_stage":"final_due",
				"sale_statuses":[],
				"checklist":[{"name":"系统规格书","status":"missing","status_label":"缺","gap":true,"has_file":false}],
				"certifications":[{"standard_name":"IEC 62619","status":"obtained","region_name":"Region A","origin":"ancestor"}]
			}
		]
	}`, library.ID, library.Name, timestamp.AsOf.Format(time.RFC3339Nano), lineID, modelID, lineID, shelfNode(nodes, "single"), lineID)
	require.JSONEq(t, expected, body)
}

// Medium spec, shelf-showcase task 01: a snapshot returns every model, and
// missing optional values serialize as empty arrays and strings.
func TestShowcaseShelfReturnsModelsAcrossPages(t *testing.T) {
	const modelCount = 101
	t.Setenv("RELIQUARY_SHOWCASE_LIBRARY", "")
	f, library := newProductShelfFixture(t)
	operations := make([]types.ShelfOperation, 0, modelCount)
	for i := range modelCount {
		name := fmt.Sprintf("Model %03d", i)
		operations = append(operations, types.ShelfOperation{
			ID: fmt.Sprintf("model-%03d", i), Type: "create_entity",
			Entity: &types.ManagedEntityInput{Kind: "model", Name: name},
		})
	}
	var saved types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{
		Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: operations,
	}, http.StatusCreated, &saved)

	var snapshot types.ShowcaseShelf
	require.NoError(t, json.Unmarshal([]byte(showcaseRequest(t, f, http.StatusOK)), &snapshot))
	require.Equal(t, []types.ShowcaseRegion{}, snapshot.Regions)
	require.Equal(t, []types.ShowcaseNamedEntity{}, snapshot.ProductLines)
	require.Len(t, snapshot.Models, len(operations))
	for i, model := range snapshot.Models {
		require.Equal(t, types.ShowcaseModel{
			ID: shelfNode(saved, operations[i].ID), Name: operations[i].Entity.Name,
			SaleStatuses: []types.ShowcaseSaleStatus{}, Checklist: []types.ShowcaseChecklistItem{},
			Certifications: []types.ShowcaseCertification{},
		}, model)
	}
}

func showcaseRequest(t *testing.T, f *managedNamesFixture, status int) string {
	t.Helper()
	response := personalRequest(t, NewRouter(f.params), http.MethodGet, "/api/v1/showcase/shelf", "", "", false)
	require.Equal(t, status, response.Code, response.Body.String())
	return response.Body.String()
}

func requireShowcaseLibrary(t *testing.T, body string, library types.Library) {
	t.Helper()
	var snapshot types.ShowcaseShelf
	require.NoError(t, json.Unmarshal([]byte(body), &snapshot))
	require.Equal(t, types.ShowcaseNamedEntity{ID: library.ID, Name: library.Name}, snapshot.Library)
	require.Equal(t, []types.ShowcaseModel{}, snapshot.Models)
}
