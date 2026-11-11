package router

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Medium spec, task 34: region spellings are user-maintained configuration,
// persisted through a changeset and exposed by the guide without deployment.
func TestShelfRegionAliasesPersistAndIdentifyOneRegion(t *testing.T) {
	f, library, _ := shelfModelSetup(t)
	f.request(http.MethodGet, "/libraries/"+library.ID, nil, http.StatusOK, &library)
	config := map[string]any{"module_types": library.ShelfConfig.ModuleTypes, "regions": []any{
		map[string]any{"key": "region_a", "name": " Region original ", "aliases": []string{"  Customer spelling  "}},
		map[string]any{"key": "region_b", "name": "Other region"},
	}}
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]string{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "settings", "type": "configure_shelf", "library_updated_at": library.UpdatedAt, "library": map[string]any{"shelf_config": config}}}}, http.StatusCreated, &draft)
	require.Equal(t, "confirmed", draft.Status)
	var saved map[string]any
	f.request(http.MethodGet, "/libraries/"+library.ID, nil, http.StatusOK, &saved)
	regions := saved["shelf_config"].(map[string]any)["regions"].([]any)
	require.Equal(t, " Region original ", regions[0].(map[string]any)["name"])
	require.Equal(t, []any{"Customer spelling"}, regions[0].(map[string]any)["aliases"])
	var guide struct {
		Values struct {
			Regions []struct {
				Aliases []string `json:"aliases"`
			} `json:"regions"`
		} `json:"values"`
	}
	mcpCall(t, f.connect(t, "2025-11-25"), "shelf_guide", map[string]any{"library_id": library.ID}, &guide)
	require.Equal(t, []string{"Customer spelling"}, guide.Values.Regions[0].Aliases)
	for _, spelling := range []string{"REGION_A", " region original ", "customer SPELLING"} {
		config["regions"] = []any{
			map[string]any{"key": "region_a", "name": " Region original ", "aliases": []string{"Customer spelling"}},
			map[string]any{"key": "region_b", "name": "Other region", "aliases": []string{spelling}},
		}
		f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": config}, http.StatusBadRequest, nil)
	}
}

// Medium spec, task 34: both certification imports normalize only configured
// spellings; unknown text is returned verbatim for the submitter to correct.
func TestShelfImportCertificationRegionsResolveConfiguration(t *testing.T) {
	for _, kind := range []string{"certification_progress", "certification_report"} {
		t.Run(kind, func(t *testing.T) {
			f, library, _ := shelfModelSetup(t)
			configureCertificationStandards(f, &library)
			config := library.ShelfConfig
			config.Regions[0].Aliases = []string{"Customer region"}
			f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": config}, http.StatusOK, &library)
			importRecordPlan(f, library, kind, []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}}, map[string]any{"progress": "Completed"})
			var rows strings.Builder
			for i, region := range []string{"region_a", " Region A ", " cUsToMeR REGION ", "", " Unknown original "} {
				rows.WriteString(importRecordRow(i+3, "Synthetic model", "standard_a", fmt.Sprintf("Object %d", i), region))
			}
			draft := importUpload(t, f, library.ID, "cert_20261028.xlsx", "", importWorkbook(t, rows.String(), ""), http.StatusCreated)
			require.Len(t, draft.Items, 5)
			for i, region := range []string{"region_a", "region_a", "region_a", ""} {
				require.Equal(t, "new", draft.Items[i].Conflicts[0].Type)
				require.Equal(t, region, draft.Items[i].Operation.Certification.Region)
				if region != "" {
					require.Contains(t, draft.Items[i].Operation.Certification.Name, " / Region A")
				}
			}
			require.Equal(t, "uncertain", draft.Items[4].Conflicts[0].Type)
			require.Equal(t, " Unknown original ", draft.Items[4].Conflicts[0].Details.(map[string]any)["D7"])
			decisions := []types.ShelfDecision{}
			for i, item := range draft.Items {
				action := "apply"
				if i == 4 {
					action = "discard"
				}
				decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: action})
			}
			f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, &draft)
			var certs []types.ShelfCertificationView
			f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/certifications", nil, http.StatusOK, &certs)
			require.Len(t, certs, 4)
		})
	}
}

// Medium spec, task 34: writes accept exact configured keys or an empty region;
// names, aliases, whitespace and unknown values remain blocking proposals.
func TestShelfCertificationWritesRequireRegionKeys(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	library.ShelfConfig.Regions[0].Aliases = []string{"Customer region"}
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": library.ShelfConfig}, http.StatusOK, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	for _, region := range []string{"Unknown original", "Region A", "Customer region", " REGION_A ", "region_a", ""} {
		record := map[string]any{"name": "Certification " + region, "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "Container", "region": region}
		var draft types.ShelfChangeset
		f.request(http.MethodPost, path, map[string]any{"source": map[string]string{"type": "web"}, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": record}}}, http.StatusCreated, &draft)
		if region != "region_a" && region != "" {
			require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type, region)
			f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]string{"item_id": "cert", "action": "apply"}}}, http.StatusConflict, nil)
			continue
		}
		f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]string{"item_id": "cert", "action": "apply"}}}, http.StatusOK, &draft)
		record["progress"] = "Completed"
		f.request(http.MethodPost, path, map[string]any{"source": map[string]string{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "update", "type": "update_certification", "revision": 1, "certification": record}}}, http.StatusCreated, &draft)
		require.Equal(t, "confirmed", draft.Status)
	}
}

// Medium spec, task 34: query and MCP reads keep keys for identity and provide
// display names separately; an explicit empty filter still selects empty regions.
func TestShelfCertificationRegionQueriesAndDisplayNames(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	for _, region := range []string{"region_a", ""} {
		f.request(http.MethodPost, path, map[string]any{"source": map[string]string{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Cert " + region, "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "Container", "region": region}}}}, http.StatusCreated, nil)
	}
	var result types.ShelfQueryResult
	for _, region := range []string{"region_a", ""} {
		f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/query", map[string]any{"certification": map[string]string{"region": region}}, http.StatusOK, &result)
		require.EqualValues(t, 1, result.Total)
	}
	for _, region := range []string{"Region A", "REGION_A", "Unknown"} {
		f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/query", map[string]any{"certification": map[string]string{"region": region}}, http.StatusBadRequest, nil)
	}
	var detail struct {
		Shelf struct {
			Certifications []struct {
				RegionName    string                   `json:"region_name"`
				Certification types.ShelfCertification `json:"certification"`
			} `json:"certifications"`
		} `json:"shelf"`
		Facts []struct {
			Field      string `json:"field"`
			RegionName string `json:"region_name"`
		} `json:"facts"`
	}
	mcpCall(t, f.connect(t, "2025-11-25"), "shelf_get", map[string]any{"library_id": library.ID, "node_id": shelfNode(nodes, "model")}, &detail)
	require.Len(t, detail.Shelf.Certifications, 2)
	for _, cert := range detail.Shelf.Certifications {
		if cert.Certification.Region == "region_a" {
			require.Equal(t, "Region A", cert.RegionName)
		}
	}
	nameFound := false
	for _, fact := range detail.Facts {
		if fact.Field == "certification" && fact.RegionName == "Region A" {
			nameFound = true
		}
	}
	require.True(t, nameFound, "certification facts must include the configured display name")
}

// Medium regression, task 34: this seed represents an old snapshot. Reads retain
// its raw region and only an explicit ID and revision can correct the old key.
func TestShelfLegacyCertificationRegionsRemainReadableAndCanBeCorrected(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	record := map[string]any{"name": "Legacy certification", "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "Container", "region": "region_a"}
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]string{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": record}}}, http.StatusCreated, &draft)
	id := draft.Items[0].EntityID
	require.NoError(t, f.db.Exec(`UPDATE entities SET attributes=jsonb_set(attributes,'{certification,region}','" Legacy original "') WHERE id=?`, id).Error)
	require.NoError(t, f.db.Exec(`UPDATE entity_attribute_versions SET attributes=jsonb_set(attributes,'{certification,region}','" Legacy original "') WHERE entity_id=?`, id).Error)
	var detail map[string]any
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/nodes/"+shelfNode(nodes, "model"), nil, http.StatusOK, &detail)
	cert := detail["shelf"].(map[string]any)["certifications"].([]any)[0].(map[string]any)
	require.Equal(t, " Legacy original ", cert["region_name"])
	require.Equal(t, " Legacy original ", cert["certification"].(map[string]any)["region"])
	// A legacy region does not change composition scope or require migration.
	f.request(http.MethodPost, path, map[string]any{"source": map[string]string{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "unrelated", "type": "save_relation", "relation": map[string]any{"subject_id": shelfNode(nodes, "single"), "object_id": shelfNode(nodes, "a"), "relation_type": "composed_of", "attributes": map[string]string{"module_type": "module_a"}}}}}, http.StatusCreated, nil)
	record["region"] = " Legacy original "
	f.request(http.MethodPost, path, map[string]any{"source": map[string]string{"type": "web"}, "operations": []any{map[string]any{"id": "update", "type": "update_certification", "entity_id": id, "revision": 1, "certification": record}}}, http.StatusCreated, &draft)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
	record["region"] = "region_a"
	f.request(http.MethodPost, path, map[string]any{"source": map[string]string{"type": "web"}, "operations": []any{map[string]any{"id": "update", "type": "update_certification", "revision": 1, "certification": record}}}, http.StatusCreated, &draft)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type, "legacy corrections require an explicit record ID")
	f.request(http.MethodPost, path, map[string]any{"source": map[string]string{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "update", "type": "update_certification", "entity_id": id, "revision": 1, "certification": record}}}, http.StatusCreated, &draft)
	require.Equal(t, "confirmed", draft.Status)
	var saved types.EntityDetail
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &saved)
	require.Equal(t, "region_a", saved.Entity.Attributes["certification"].(map[string]any)["region"])
	f.request(http.MethodPost, path, map[string]any{"source": map[string]string{"type": "web"}, "operations": []any{map[string]any{"id": "stale", "type": "update_certification", "entity_id": id, "revision": 1, "certification": record}}}, http.StatusCreated, &draft)
	require.Equal(t, "revision_conflict", draft.Items[0].Conflicts[0].Type)
}

// Medium spec, task 34: both overseas check formats resolve region cells and
// never turn an unknown spelling into a misleading contradiction or a write.
func TestShelfOverseasRegionCellsResolveWithoutWriting(t *testing.T) {
	for _, matrix := range []bool{false, true} {
		t.Run(fmt.Sprint(matrix), func(t *testing.T) {
			f, library, nodes := shelfModelSetup(t)
			configureCertificationStandards(f, &library)
			library.ShelfConfig.Regions[0].Aliases = []string{"Customer region"}
			f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": library.ShelfConfig}, http.StatusOK, &library)
			var seed types.ShelfChangeset
			at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "cert", Type: "create_certification", EffectiveAt: &at, Certification: &types.ShelfCertificationInput{Name: "Overseas certificate", ShelfCertification: types.ShelfCertification{OwnerID: shelfNode(nodes, "model"), Standard: "standard_a", CertificationObject: "Container", Region: "region_a", Progress: "Completed"}}}}}, http.StatusCreated, &seed)
			columns := []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "region"}}
			extra := map[string]any{}
			standardCell := "standard_a"
			if matrix {
				columns[1] = types.ShelfImportColumn{Column: "B", Field: "certification", Key: "standard_a"}
				extra["progress"], extra["check_values"], standardCell = "Completed", map[string]bool{"yes": true}, "yes"
			}
			importRecordPlan(f, library, "overseas_sales", columns, extra)
			for _, region := range []string{" cUsToMeR REGION ", "Unknown original"} {
				draft := importUpload(t, f, library.ID, "overseas_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Synthetic model", standardCell, region), ""), http.StatusCreated)
				require.Len(t, draft.Items, 1)
				if region == "Unknown original" {
					require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
					require.Equal(t, region, draft.Items[0].Conflicts[0].Details.(map[string]any)["C3"])
				} else {
					require.Equal(t, "import_notice", draft.Items[0].Operation.Type)
					require.Empty(t, draft.Items[0].Conflicts)
					confirmImport(t, f, library.ID, draft)
				}
			}
			var cert types.EntityDetail
			f.request(http.MethodGet, "/entities/"+seed.Items[0].EntityID, nil, http.StatusOK, &cert)
			require.EqualValues(t, 1, cert.Entity.Revision)
			require.Equal(t, "region_a", cert.Entity.Attributes["certification"].(map[string]any)["region"])
		})
	}
}
