package router

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Medium spec, task 05 / ADR 0023: certification proposals are inert until
// confirmed, and progress updates retain identity and original metadata.
func TestShelfCertificationConfirmedCreateAndProgressUpdate(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	record := map[string]any{"name": " Original certification ", "owner_id": shelfNode(nodes, "model"), "standard": "Customer spelling", "year": 2026, "certification_object": "Container", "cell_part_id": shelfNode(nodes, "a"), "region": "region_a", "agency": " Original agency ", "progress": "In progress", "start_date": "2026-01-02", "obtained_date": "2026-02-03", "issued_date": "2026-02-04", "validity": "Five years", "number": "SYN-001", "cloud_url": "https://example.com/synthetic", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{shelfNode(nodes, "a")}}}}
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "agent", "agent_name": "Synthetic assistant"}, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": record}}}, http.StatusCreated, &draft)
	require.Equal(t, "new", draft.Items[0].Conflicts[0].Type)
	id := draft.Items[0].EntityID
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusNotFound, nil)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "cert", "action": "apply"}}}, http.StatusOK, &draft)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &detail)
	require.Equal(t, " Original certification ", detail.Entity.CanonicalName)
	stored := detail.Entity.Attributes["certification"].(map[string]any)
	require.Equal(t, "standard_a", stored["standard"])
	require.Equal(t, " Original agency ", stored["agency"])
	require.Equal(t, "SYN-001", stored["number"])
	require.EqualValues(t, 2026, stored["year"])
	require.Equal(t, "Container", stored["certification_object"])
	require.Equal(t, "region_a", stored["region"])
	require.Equal(t, "2026-01-02", stored["start_date"])
	require.Equal(t, "2026-02-03", stored["obtained_date"])
	require.Equal(t, "2026-02-04", stored["issued_date"])
	require.Equal(t, "Five years", stored["validity"])
	require.Equal(t, "https://example.com/synthetic", stored["cloud_url"])
	require.Equal(t, shelfNode(nodes, "model"), stored["owner_id"])
	require.Equal(t, shelfNode(nodes, "a"), stored["cell_part_id"])
	record["progress"] = "Completed"
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "progress", "type": "update_certification", "revision": 1, "certification": record}}}, http.StatusCreated, &draft)
	require.Equal(t, id, draft.Items[0].EntityID)
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &detail)
	require.EqualValues(t, 2, detail.Entity.Revision)
	require.Equal(t, "Completed", detail.Entity.Attributes["certification"].(map[string]any)["progress"])
}

func configureCertificationStandards(f *managedNamesFixture, library *types.Library) {
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": map[string]any{
		"module_types": library.ShelfConfig.ModuleTypes, "regions": library.ShelfConfig.Regions,
		"standards": []any{map[string]any{"key": "standard_a", "name": "Synthetic Standard", "aliases": []string{"Customer spelling"}}},
	}}, http.StatusOK, library)
}

// Medium spec, task 05: a configured name or alias returns the same records;
// component and ancestor certificates retain their origin and conditions.
func TestShelfCertificationStandardAliasesAndInheritance(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	operations := []any{}
	for _, owner := range []string{"model", "fixed", "product", "platform"} {
		operations = append(operations, map[string]any{"id": owner, "type": "create_certification", "certification": map[string]any{"name": "Certificate " + owner, "owner_id": shelfNode(nodes, owner), "standard": "Synthetic Standard", "certification_object": "Synthetic object", "region": "region_a", "progress": "Completed"}})
	}
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": operations}, http.StatusCreated, &draft)
	for _, standard := range []string{"Synthetic+Standard", "Customer+spelling", "standard_a"} {
		var rows []map[string]any
		f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/certifications?standard="+standard, nil, http.StatusOK, &rows)
		require.Len(t, rows, 4)
		for _, row := range rows {
			require.Equal(t, "Synthetic Standard", row["standard_name"])
		}
	}
	var detail map[string]any
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model"), nil, http.StatusOK, &detail)
	rows := detail["shelf"].(map[string]any)["certifications"].([]any)
	require.Len(t, rows, 4)
	origins := map[string]int{}
	for _, row := range rows {
		origins[row.(map[string]any)["origin"].(string)]++
	}
	require.Equal(t, map[string]int{"self": 1, "component": 1, "ancestor": 2}, origins)
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "single"), nil, http.StatusOK, &detail)
	rows = detail["shelf"].(map[string]any)["certifications"].([]any)
	require.Len(t, rows, 3)
	origins = map[string]int{}
	for _, row := range rows {
		origins[row.(map[string]any)["origin"].(string)]++
	}
	require.Equal(t, map[string]int{"self": 1, "ancestor": 2}, origins)
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "platform"), nil, http.StatusOK, &detail)
	require.Len(t, detail["shelf"].(map[string]any)["certifications"], 1)
}

// Medium spec, task 05: ambiguous/absent keys and unidentifiable owners block
// the whole changeset, even when a submitter simply accepts the proposal.
func TestShelfCertificationUnmatchedKeysReturnToSubmitter(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	submit := func(record map[string]any, kind string, confirmed bool) types.ShelfChangeset {
		var draft types.ShelfChangeset
		f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": confirmed, "operations": []any{map[string]any{"id": "cert", "type": kind, "revision": 1, "certification": record}}}, http.StatusCreated, &draft)
		return draft
	}
	record := map[string]any{"name": "Synthetic cert one", "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "BMS", "region": "region_a"}
	for _, owner := range []string{"BMS 7.0", "", shelfNode(nodes, "line")} {
		record["owner_id"] = owner
		blocked := submit(record, "create_certification", false)
		require.Equal(t, "uncertain", blocked.Items[0].Conflicts[0].Type)
		f.request(http.MethodPost, path+"/"+blocked.ID+"/confirm", map[string]any{"revision": blocked.Revision, "decisions": []any{map[string]any{"item_id": "cert", "action": "apply"}}}, http.StatusConflict, nil)
	}
	record["owner_id"] = shelfNode(nodes, "model")
	blocked := submit(record, "update_certification", false)
	require.Equal(t, "uncertain", blocked.Items[0].Conflicts[0].Type)
	first := submit(record, "create_certification", true)
	record["name"] = "Synthetic cert two"
	second := submit(record, "create_certification", true)
	record["name"] = ""
	blocked = submit(record, "update_certification", false)
	require.Equal(t, "uncertain", blocked.Items[0].Conflicts[0].Type)
	f.request(http.MethodPost, path+"/"+blocked.ID+"/confirm", map[string]any{"revision": blocked.Revision, "decisions": []any{map[string]any{"item_id": "cert", "action": "apply"}}}, http.StatusConflict, nil)
	for _, id := range []string{first.Items[0].EntityID, second.Items[0].EntityID} {
		var detail types.EntityDetail
		f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &detail)
		require.EqualValues(t, 1, detail.Entity.Revision)
	}

	f.request(http.MethodPost, path+"/"+blocked.ID+"/confirm", map[string]any{"revision": blocked.Revision, "decisions": []any{map[string]any{"item_id": "cert", "action": "apply", "target_entity_id": first.Items[0].EntityID, "target_revision": 1}}}, http.StatusOK, nil)
	var chosen types.EntityDetail
	f.request(http.MethodGet, "/entities/"+first.Items[0].EntityID, nil, http.StatusOK, &chosen)
	require.EqualValues(t, 2, chosen.Entity.Revision)
	f.request(http.MethodGet, "/entities/"+second.Items[0].EntityID, nil, http.StatusOK, &chosen)
	require.EqualValues(t, 1, chosen.Entity.Revision)
	record["region"] = "Different country"
	blocked = submit(record, "update_certification", false)
	require.Equal(t, "uncertain", blocked.Items[0].Conflicts[0].Type)
}

// Medium regression, task 05: removing a standard definition or spelling must
// not strand historical records or cause an existing standard query to miss them.
func TestShelfCertificationStandardConfigurationPreservesIdentity(t *testing.T) {
	f, library, _ := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	config := map[string]any{"module_types": library.ShelfConfig.ModuleTypes, "regions": library.ShelfConfig.Regions, "standards": []any{}}
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": config}, http.StatusBadRequest, nil)
	config["standards"] = []any{map[string]any{"key": "standard_a", "name": "Renamed standard", "aliases": []string{"Customer spelling"}}}
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": config}, http.StatusBadRequest, nil)
	config["standards"] = []any{map[string]any{"key": "standard_a", "name": "Renamed standard", "aliases": []string{"Customer spelling", "Synthetic Standard"}}}
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": config}, http.StatusOK, nil)
	config["standards"] = []any{map[string]any{"key": "standard_a", "name": "Renamed standard", "aliases": []string{"Customer spelling", "Synthetic Standard"}}, map[string]any{"key": "other", "name": "Customer spelling"}}
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": config}, http.StatusBadRequest, nil)
}

// Medium spec, task 05 / ADR 0023: cell identity and conjunctive conditions
// filter certificates, and composition edits cannot strand a recorded condition.
func TestShelfCertificationConditionsSurviveCompositionEdits(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Conditional cert", "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "Container", "cell_part_id": shelfNode(nodes, "a"), "region": "region_a", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{shelfNode(nodes, "a")}}}}}}}, http.StatusCreated, &draft)
	for _, choice := range []struct {
		part  string
		count int
	}{{"a", 1}, {"b", 0}} {
		var detail map[string]any
		f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model")+"?combination=%7B%22module_a%22:%22"+shelfNode(nodes, choice.part)+"%22%7D", nil, http.StatusOK, &detail)
		require.Len(t, detail["shelf"].(map[string]any)["certifications"], choice.count)
	}
	var relation types.Relation
	for _, item := range nodes.Items {
		if item.ID == "model_a" {
			require.NoError(t, types.DecodeShelfValue(item.After, &relation))
		}
	}
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "end", "type": "end_relation", "relation_id": relation.ID, "revision": relation.Version}}}, http.StatusCreated, &draft)
	require.NotEqual(t, "modified", draft.Items[0].Conflicts[0].Type)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "end", "action": "apply"}}}, http.StatusConflict, nil)
}

// Medium spec, task 05: imported certifications use file time, preserve manual
// changes as explicit conflicts, and reject stale progress updates atomically.
func TestShelfCertificationHistoryAndManualProgressProtection(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	record := map[string]any{"name": "Historical cert", "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "BMS", "region": "region_a", "progress": "Started"}
	source := map[string]any{"type": "import", "import_batch_id": "synthetic-certificates", "file_date": "2025-01-01T00:00:00Z"}
	var created types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": source, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": record}}}, http.StatusCreated, &created)
	id := created.Items[0].EntityID
	var detail map[string]any
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model")+"?as_of=2024-01-01T00:00:00Z", nil, http.StatusOK, &detail)
	require.Empty(t, detail["shelf"].(map[string]any)["certifications"])
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model")+"?as_of=2025-02-01T00:00:00Z", nil, http.StatusOK, &detail)
	require.Len(t, detail["shelf"].(map[string]any)["certifications"], 1)
	record["progress"] = "Manually confirmed"
	var first, second types.ShelfChangeset
	for _, draft := range []*types.ShelfChangeset{&first, &second} {
		f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "progress", "type": "update_certification", "revision": 1, "certification": record}}}, http.StatusCreated, draft)
	}
	review := func(draft types.ShelfChangeset, code int) {
		f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "progress", "action": "apply"}}}, code, nil)
	}
	review(first, http.StatusOK)
	review(second, http.StatusConflict)
	record["progress"] = "Spreadsheet update"
	fileTime := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	source["file_date"] = fileTime
	var imported types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": source, "operations": []any{map[string]any{"id": "progress", "type": "update_certification", "revision": 2, "certification": record}}}, http.StatusCreated, &imported)
	require.NotEmpty(t, imported.Items[0].Conflicts[0].ManualBefore)
	protected := imported.Items[0].Conflicts[0].ManualBefore["attributes"].(map[string]any)["certification"].(map[string]any)
	require.Equal(t, "Manually confirmed", protected["progress"])
	f.request(http.MethodPost, path, map[string]any{"source": source, "confirmed": true, "operations": []any{map[string]any{"id": "progress", "type": "update_certification", "revision": 2, "certification": record}}}, http.StatusConflict, nil)
	review(imported, http.StatusOK)
	var scheduled types.EntityDetail
	f.request(http.MethodGet, "/entities/"+id+"?as_of="+url.QueryEscape(fileTime), nil, http.StatusOK, &scheduled)
	require.Equal(t, "Spreadsheet update", scheduled.Entity.Attributes["certification"].(map[string]any)["progress"])
	var current types.EntityDetail
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &current)
	require.Equal(t, "Manually confirmed", current.Entity.Attributes["certification"].(map[string]any)["progress"])
}

// Medium regression, task 05: an ancestor certificate scoped to another model's
// component must not appear as an applicable fact on an unrelated descendant.
func TestShelfCertificationAncestorScopeExcludesOtherModels(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Scoped parent certificate", "owner_id": shelfNode(nodes, "product"), "standard": "standard_a", "certification_object": "Container", "cell_part_id": shelfNode(nodes, "a"), "region": "region_a", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{shelfNode(nodes, "a")}}}}}}}, http.StatusCreated, &draft)
	var detail map[string]any
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "single"), nil, http.StatusOK, &detail)
	require.Empty(t, detail["shelf"].(map[string]any)["certifications"])
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model"), nil, http.StatusOK, &detail)
	require.Len(t, detail["shelf"].(map[string]any)["certifications"], 1)
}

// Medium regression, task 05 / ADR 0023: typed certification writes obey the
// existing explicit cross-kind name confirmation instead of making it unsolvable.
func TestShelfCertificationCrossKindNameCanBeConfirmed(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Synthetic model", "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "Container", "region": "region_a"}}}}, http.StatusCreated, &draft)
	require.Equal(t, "cross_kind_same_name", draft.Items[0].Conflicts[0].Type)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "cert", "action": "apply", "name_confirmations": []any{map[string]any{"name": "Synthetic model", "other_entity_id": shelfNode(nodes, "model"), "other_kind": "model"}}}}}, http.StatusOK, &draft)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusOK, &detail)
	require.Equal(t, "Synthetic model", detail.Entity.CanonicalName)
	require.Equal(t, "certification", detail.Entity.Kind)
}

// Medium spec, task 05: malformed metadata and contradictory applicability
// remain blocked proposals, rather than partially saved certification facts.
func TestShelfCertificationRejectsInvalidRecords(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	for _, tc := range []struct {
		name, key string
		value     any
		conflict  string
	}{
		{"unknown standard", "standard", "Unmapped spelling", "uncertain"},
		{"invalid date", "issued_date", "2026-02-30", "unparseable"},
		{"invalid cloud link", "cloud_url", "javascript:example", "unparseable"},
		{"missing object", "certification_object", "", "required_missing"},
		{"contradictory cell condition", "conditions", []any{map[string]any{"module_type": "module_a", "part_ids": []string{shelfNode(nodes, "b")}}}, "uncertain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := map[string]any{"name": "Invalid certificate", "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "Container", "cell_part_id": shelfNode(nodes, "a"), "region": "region_a"}
			record[tc.key] = tc.value
			var draft types.ShelfChangeset
			f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": record}}}, http.StatusCreated, &draft)
			require.Equal(t, tc.conflict, draft.Items[0].Conflicts[0].Type)
			f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "cert", "action": "apply"}}}, http.StatusConflict, nil)
			f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusNotFound, nil)
		})
	}
}

// Medium spec, task 05: deferred certification creation freezes owner, cell and
// condition references to newly confirmed nodes and can be resumed on its own.
func TestShelfCertificationDeferredReferencesCanBeConfirmed(t *testing.T) {
	f, library, _ := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{
		map[string]any{"id": "new_model", "type": "create_entity", "entity": map[string]any{"name": "New synthetic model", "kind": "model"}},
		map[string]any{"id": "new_part", "type": "create_entity", "entity": map[string]any{"name": "New synthetic part", "kind": "part"}},
		map[string]any{"id": "composition", "type": "save_relation", "relation": map[string]any{"subject_id": "$new_model", "object_id": "$new_part", "relation_type": "composed_of", "attributes": map[string]any{"module_type": "module_a"}}},
		map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Deferred synthetic cert", "owner_id": "$new_model", "standard": "Customer spelling", "certification_object": "Container", "cell_part_id": "$new_part", "region": "region_a", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{"$new_part"}}}}},
	}}, http.StatusCreated, &draft)
	modelID, partID := shelfNode(draft, "new_model"), shelfNode(draft, "new_part")
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "new_model", "action": "apply"}, map[string]any{"item_id": "new_part", "action": "apply"}, map[string]any{"item_id": "composition", "action": "apply"}, map[string]any{"item_id": "cert", "action": "defer"}}}, http.StatusOK, &draft)
	var deferred types.ShelfChangeset
	f.request(http.MethodGet, path+"/"+draft.DeferredChangesetID, nil, http.StatusOK, &deferred)
	require.Equal(t, modelID, deferred.Items[0].Operation.Certification.OwnerID)
	require.Equal(t, partID, deferred.Items[0].Operation.Certification.CellPartID)
	require.Equal(t, []string{partID}, deferred.Items[0].Operation.Certification.Conditions[0].PartIDs)
	f.request(http.MethodPost, path+"/"+deferred.ID+"/confirm", map[string]any{"revision": deferred.Revision, "decisions": []any{map[string]any{"item_id": "cert", "action": "apply"}}}, http.StatusOK, &deferred)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+modelID, nil, http.StatusOK, &detail)
	require.Len(t, detail.Shelf.Certifications, 1)
	require.Equal(t, deferred.Items[0].EntityID, detail.Shelf.Certifications[0].Entity.ID)
}

// Medium regression, task 05 / ADR 0023: a future certification remains bound
// to its declared owner even before the first attribute snapshot becomes active.
func TestShelfCertificationFutureOwnerCannotBeChangedByRelation(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	var created types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "effective_at": future, "certification": map[string]any{"name": "Future certificate", "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "Container", "region": "region_a"}}}}, http.StatusCreated, &created)
	var proposed types.Entity
	require.NoError(t, types.DecodeShelfValue(created.Items[0].After, &proposed))
	require.Contains(t, proposed.Attributes, "certification")
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "wrong_owner", "type": "save_relation", "relation": map[string]any{"subject_id": shelfNode(nodes, "single"), "object_id": created.Items[0].EntityID, "relation_type": "has_certification"}}}}, http.StatusCreated, &draft)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "wrong_owner", "action": "apply"}}}, http.StatusConflict, nil)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model"), nil, http.StatusOK, &detail)
	require.Empty(t, detail.Shelf.Certifications)
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model")+"?as_of="+url.QueryEscape(future), nil, http.StatusOK, &detail)
	require.Len(t, detail.Shelf.Certifications, 1)
}

// Medium regression, task 05: an ancestor's conjunctive conditions must be
// satisfiable by one descendant, rather than by separate unrelated models.
func TestShelfCertificationAncestorConditionsNeedOnePossibleModel(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var part types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "part", "name": "Single-only part"}, http.StatusCreated, &part)
	var saved types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "composition", "type": "save_relation", "relation": map[string]any{"subject_id": shelfNode(nodes, "single"), "object_id": part.ID, "relation_type": "composed_of", "attributes": map[string]any{"module_type": "module_b"}}}}}, http.StatusCreated, &saved)
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Impossible parent certificate", "owner_id": shelfNode(nodes, "product"), "standard": "standard_a", "certification_object": "Container", "region": "region_a", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{shelfNode(nodes, "a")}}, map[string]any{"module_type": "module_b", "part_ids": []string{part.ID}}}}}}}, http.StatusCreated, &saved)
	require.Equal(t, "uncertain", saved.Items[0].Conflicts[0].Type)
	f.request(http.MethodPost, path+"/"+saved.ID+"/confirm", map[string]any{"revision": saved.Revision, "decisions": []any{map[string]any{"item_id": "cert", "action": "apply"}}}, http.StatusConflict, nil)
}
