package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Medium spec, task 06 / ADR 0023: find preserves ambiguous identities and
// resolves explicit names, aliases and codes without choosing a category.
func TestShelfQueryFindReturnsCandidatesWithCategories(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	var model types.Entity
	f.request(http.MethodPatch, "/entities/"+shelfNode(nodes, "model"), map[string]any{"revision": 1, "code": "SYN-06", "aliases": []string{"Shared lookup"}}, http.StatusOK, &model)
	var part types.Entity
	f.request(http.MethodPatch, "/entities/"+shelfNode(nodes, "a"), map[string]any{"revision": 1, "name": "Shared lookup", "name_confirmations": []any{map[string]any{"other_entity_id": model.ID, "other_kind": "model", "name": "Shared lookup"}}}, http.StatusOK, &part)
	path := "/libraries/" + library.ID + "/shelf/find?q="
	var result struct {
		Items     []types.Entity `json:"items"`
		Total     int64          `json:"total"`
		Truncated bool           `json:"truncated"`
	}
	f.request(http.MethodGet, path+"Shared+lookup", nil, http.StatusOK, &result)
	require.EqualValues(t, 2, result.Total)
	require.False(t, result.Truncated)
	require.Len(t, result.Items, 2)
	require.ElementsMatch(t, []string{"model", "part"}, []string{result.Items[0].Kind, result.Items[1].Kind})
	for _, item := range result.Items {
		require.NotEmpty(t, item.KindLabel)
	}
	f.request(http.MethodGet, path+"syn-06", nil, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.Equal(t, model.ID, result.Items[0].ID)
	f.request(http.MethodGet, path+"Shared+lookup&limit=1", nil, http.StatusOK, &result)
	require.EqualValues(t, 2, result.Total)
	require.Len(t, result.Items, 1)
	require.True(t, result.Truncated)
}

// Medium spec, task 06: filters count models before pagination and identify
// when only some optional configurations satisfy a regional state.
func TestShelfQueryFiltersCountAndPartialOptions(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	statuses := []types.ShelfSaleStatus{{Region: "region_a", Status: "GA", Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "a")}}}}, {Region: "region_a", Status: "DEV", Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "b")}}}}}
	var saved types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{
		{ID: "model", Type: "set_sale_statuses", EntityID: shelfNode(nodes, "model"), Revision: 1, SaleStatuses: &statuses},
		{ID: "single", Type: "set_sale_statuses", EntityID: shelfNode(nodes, "single"), Revision: 1, SaleStatuses: &[]types.ShelfSaleStatus{{Region: "region_a", Status: "GA"}}},
	}}, http.StatusCreated, &saved)
	path := "/libraries/" + library.ID + "/shelf/query"
	var result struct {
		Total     int64 `json:"total"`
		Truncated bool  `json:"truncated"`
		Items     []struct {
			Entity             types.Entity             `json:"entity"`
			PartialOptions     bool                     `json:"partial_options"`
			MatchingConditions [][]types.ShelfCondition `json:"matching_conditions"`
		} `json:"items"`
	}
	input := map[string]any{"ancestor_id": shelfNode(nodes, "platform"), "sale_statuses": []any{map[string]any{"region": "region_a", "status": "GA"}}, "limit": 1}
	f.request(http.MethodPost, path, input, http.StatusOK, &result)
	require.EqualValues(t, 2, result.Total)
	require.Len(t, result.Items, 1)
	require.True(t, result.Truncated)
	require.Equal(t, shelfNode(nodes, "model"), result.Items[0].Entity.ID)
	require.True(t, result.Items[0].PartialOptions)
	require.NotEmpty(t, result.Items[0].MatchingConditions)
	input["offset"] = 1
	f.request(http.MethodPost, path, input, http.StatusOK, &result)
	require.EqualValues(t, 2, result.Total)
	require.False(t, result.Items[0].PartialOptions)
	require.True(t, result.Truncated)
	input["offset"] = 5
	f.request(http.MethodPost, path, input, http.StatusOK, &result)
	require.EqualValues(t, 2, result.Total)
	require.Empty(t, result.Items)
	require.True(t, result.Truncated)
	f.request(http.MethodPost, path, map[string]any{"as_of": "2000-01-01T00:00:00Z"}, http.StatusOK, &result)
	require.Zero(t, result.Total)
	require.Empty(t, result.Items)
	require.False(t, result.Truncated)
}

// Medium spec, task 06: numeric intervals and regional states must be true for
// the same combination, and component reverse lookup retains option limits.
func TestShelfQueryParameterAndCompositionUseSameCombination(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	library.EntitySchema.Fields = append(library.EntitySchema.Fields, types.EntityFieldDefinition{Key: "energy", Name: "Energy", Type: "number", Kinds: []string{"model"}, ProductLineIDs: []string{shelfNode(nodes, "line")}})
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_schema": library.EntitySchema}, http.StatusOK, nil)
	a := []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "a")}}}
	b := []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "b")}}}
	statuses := []types.ShelfSaleStatus{{Region: "region_a", Status: "GA", Conditions: a}, {Region: "region_a", Status: "DEV", Conditions: b}}
	parameters := map[string][]types.ShelfParameterValue{"energy": {{Value: 4, Conditions: a}, {Value: map[string]any{"min": 9, "max": 54}, Conditions: b}}}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{
		{ID: "status", Type: "set_sale_statuses", EntityID: shelfNode(nodes, "model"), Revision: 1, SaleStatuses: &statuses},
		{ID: "parameter", Type: "set_parameter_values", EntityID: shelfNode(nodes, "model"), Revision: 2, ParameterValues: &parameters},
	}}, http.StatusCreated, nil)
	path := "/libraries/" + library.ID + "/shelf/query"
	var result types.ShelfQueryResult
	input := map[string]any{"parameters": []any{map[string]any{"field": "energy", "min": 5}}, "sale_statuses": []any{map[string]any{"region": "region_a", "status": "GA"}}}
	f.request(http.MethodPost, path, input, http.StatusOK, &result)
	require.Zero(t, result.Total, "different option facts cannot be combined")
	input["sale_statuses"] = []any{map[string]any{"region": "region_a", "status": "DEV"}}
	f.request(http.MethodPost, path, input, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.True(t, result.Items[0].PartialOptions)
	// Both filters are met by part b; the group reports that condition once.
	require.Equal(t, [][]types.ShelfCondition{b}, result.Items[0].MatchingConditions)
	// The default summary keeps identity and only the filtered facts, without
	// the per-model checklist, documents or unrelated attributes.
	require.Equal(t, types.ShelfQueryDetailSummary, result.Detail)
	require.Nil(t, result.Items[0].Shelf)
	require.Empty(t, result.Items[0].Entity.Attributes)
	fields := map[string]int{}
	for _, fact := range result.Items[0].Facts {
		fields[fact.Field]++
		require.NotEmpty(t, fact.Evidence.OperationLogID)
	}
	require.Equal(t, map[string]int{"sale_statuses": 2, "energy": 2}, fields)
	var raw map[string]any
	f.request(http.MethodPost, path, input, http.StatusOK, &raw)
	item := raw["items"].([]any)[0].(map[string]any)
	require.NotContains(t, item, "shelf")
	input["detail"] = "full"
	f.request(http.MethodPost, path, input, http.StatusOK, &result)
	require.Equal(t, types.ShelfQueryDetailFull, result.Detail)
	require.NotNil(t, result.Items[0].Shelf)
	require.Greater(t, len(result.Items[0].Facts), len(fields))
	input["detail"] = "everything"
	f.request(http.MethodPost, path, input, http.StatusBadRequest, nil)
	delete(input, "detail")
	input["parameters"] = []any{map[string]any{"field": "energy", "min": 55}}
	f.request(http.MethodPost, path, input, http.StatusOK, &result)
	require.Zero(t, result.Total)
	for _, tc := range []struct {
		max   int
		total int
	}{{8, 0}, {9, 1}} {
		input["parameters"] = []any{map[string]any{"field": "energy", "max": tc.max}}
		f.request(http.MethodPost, path, input, http.StatusOK, &result)
		require.EqualValues(t, tc.total, result.Total)
	}
	for _, tc := range []struct {
		part    string
		count   int
		partial bool
	}{{"a", 1, true}, {"fixed", 2, false}} {
		f.request(http.MethodPost, path, map[string]any{"part_id": shelfNode(nodes, tc.part)}, http.StatusOK, &result)
		require.EqualValues(t, tc.count, result.Total)
		for _, item := range result.Items {
			require.Equal(t, tc.partial, item.PartialOptions)
		}
	}
	f.request(http.MethodPost, path, map[string]any{"composition": a}, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.True(t, result.Items[0].PartialOptions)
}

// Medium spec, task 06 / ADR 0023: certification reverse lookup resolves
// standard aliases, owner and cell restrictions and preserves inherited origin.
func TestShelfQueryCertificationsReverseLookupAndOrigins(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	operations := []types.ShelfOperation{}
	for _, owner := range []string{"a", "fixed", "platform"} {
		operations = append(operations, types.ShelfOperation{ID: owner, Type: "create_certification", Certification: &types.ShelfCertificationInput{Name: "Query certificate " + owner, ShelfCertification: types.ShelfCertification{OwnerID: shelfNode(nodes, owner), Standard: "Customer spelling", CertificationObject: "Synthetic object", Region: "region_a", Progress: owner}}})
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: operations}, http.StatusCreated, nil)
	path := "/libraries/" + library.ID + "/shelf/query"
	var result types.ShelfQueryResult
	f.request(http.MethodPost, path, map[string]any{"certification": map[string]any{"standard": "Customer spelling"}}, http.StatusOK, &result)
	require.EqualValues(t, 2, result.Total)
	for _, item := range result.Items {
		require.False(t, item.PartialOptions, "the fixed component certificate covers every option")
	}
	f.request(http.MethodPost, path, map[string]any{"detail": "full", "certification": map[string]any{"standard": "Customer spelling", "progress": "a", "region": "region_a"}}, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.True(t, result.Items[0].PartialOptions)
	labels := map[string]string{}
	for _, fact := range result.Items[0].Facts {
		if fact.Field != "certification" {
			continue
		}
		labels[fact.Origin] = fact.OriginLabel
		if fact.PartialOptions {
			require.Equal(t, "component", fact.Origin)
			require.NotEmpty(t, fact.Conditions)
		}
		require.NotEmpty(t, fact.Evidence.OperationLogID)
	}
	require.Equal(t, "来自部件", labels["component"])
	require.Equal(t, "来自上层", labels["ancestor"])
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/nodes/"+shelfNode(nodes, "single"), nil, http.StatusOK, &result.Items[0].ShelfNodeDetail)
	origins := map[string]int{}
	for _, cert := range result.Items[0].Shelf.Certifications {
		origins[cert.Origin]++
	}
	require.Equal(t, map[string]int{"self": 1, "ancestor": 1}, origins)
	f.request(http.MethodPost, path, map[string]any{"certification": map[string]any{"progress": "a", "region": "Wrong country"}}, http.StatusBadRequest, nil)
	f.request(http.MethodPost, path, map[string]any{"certification": map[string]any{"progress": "a"}, "composition": []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "b")}}}}, http.StatusOK, &result)
	require.Zero(t, result.Total, "a component certificate cannot apply to another optional component")
	f.request(http.MethodPost, path, map[string]any{"certification": map[string]any{"standard": "Unknown standard"}}, http.StatusOK, &result)
	require.Zero(t, result.Total)
}

// Medium spec, task 06: several conditional records can collectively cover
// every option; a conditional cell key must still narrow a model certificate.
func TestShelfQueryCertificationCoverageIncludesAllOptions(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	operations := []types.ShelfOperation{}
	for _, part := range []string{"a", "b"} {
		operations = append(operations, types.ShelfOperation{ID: part, Type: "create_certification", Certification: &types.ShelfCertificationInput{Name: "Coverage " + part, ShelfCertification: types.ShelfCertification{OwnerID: shelfNode(nodes, "model"), Standard: "standard_a", CertificationObject: "Object", CellPartID: shelfNode(nodes, part), Region: "region_a", Progress: "Complete"}}})
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: operations}, http.StatusCreated, nil)
	var result types.ShelfQueryResult
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/query", map[string]any{"certification": map[string]any{"standard": "standard_a", "progress": "Complete"}}, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.False(t, result.Items[0].PartialOptions)
	for _, fact := range result.Items[0].Facts {
		if fact.Field == "certification" {
			require.True(t, fact.PartialOptions)
			require.NotEmpty(t, fact.Conditions)
		}
	}
	combination, _ := json.Marshal(map[string]string{"module_a": shelfNode(nodes, "a")})
	var selected types.ShelfNodeDetail
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/nodes/"+shelfNode(nodes, "model")+"?combination="+url.QueryEscape(string(combination)), nil, http.StatusOK, &selected)
	require.Len(t, selected.Shelf.Certifications, 1)
	links := 0
	for _, fact := range selected.Facts {
		if fact.Field == "has_certification" {
			links++
			require.NotEmpty(t, fact.Conditions)
			require.False(t, fact.PartialOptions)
		}
	}
	require.Equal(t, 1, links, "certification links must follow the same combination as their records")
}

// Medium regression, task 06 / ADR 0022: archiving a component hides its catalog
// entry but retains recorded model usage for detail and reverse lookup.
func TestShelfQueryArchivedComponentsKeepRecordedUsage(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	archived := true
	f.request(http.MethodPatch, "/entities/"+shelfNode(nodes, "fixed"), types.ManagedEntityPatch{Revision: 1, Archived: &archived}, http.StatusOK, nil)
	var detail types.ShelfNodeDetail
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/nodes/"+shelfNode(nodes, "model"), nil, http.StatusOK, &detail)
	require.Len(t, detail.Shelf.Composition, 2)
	var result types.ShelfQueryResult
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/query", map[string]any{"part_id": shelfNode(nodes, "fixed")}, http.StatusOK, &result)
	require.EqualValues(t, 2, result.Total)
}

// Medium regression, task 06: a successor composition relation cites the edit
// that created it, while the prior interval retains its original source.
func TestShelfQueryRelationSuccessorsKeepTheirSources(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	var original types.Relation
	for _, item := range nodes.Items {
		if item.ID == "model_a" {
			require.NoError(t, json.Unmarshal(item.After, &original))
		}
	}
	require.NotEmpty(t, original.ID)
	before := time.Now().UTC().Format(time.RFC3339Nano)
	var saved types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "agent", AgentName: "Composition editor"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "edit", Type: "save_relation", RelationID: original.ID, Relation: &types.ManagedRelationInput{Version: original.Version, SubjectID: original.SubjectID, ObjectID: original.ObjectID, RelationType: original.RelationType, Attributes: original.Attributes}}}}, http.StatusCreated, &saved)
	var successor types.Relation
	require.NoError(t, json.Unmarshal(saved.Items[0].After, &successor))
	require.NotEqual(t, original.ID, successor.ID)
	var detail types.ShelfNodeDetail
	path := "/libraries/" + library.ID + "/shelf/nodes/" + shelfNode(nodes, "model")
	for _, tc := range []struct{ suffix, relationID, sourceID string }{{"", successor.ID, saved.ID}, {"?as_of=" + url.QueryEscape(before), original.ID, nodes.ID}} {
		f.request(http.MethodGet, path+tc.suffix, nil, http.StatusOK, &detail)
		found := false
		for _, fact := range detail.Facts {
			if fact.RelationID == tc.relationID {
				found = true
				require.Equal(t, tc.sourceID, fact.Evidence.ChangesetID)
				require.NotEmpty(t, fact.Evidence.OperationLogID)
			}
		}
		require.True(t, found)
	}
}

// Medium spec, task 06: overlapping certificate conditions can cover three of
// four combinations; filling the remaining combination removes the partial tag.
func TestShelfQueryCoverageAcrossOptionalModules(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var saved types.ShelfChangeset
	f.request(http.MethodPost, path, types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{
		{ID: "alternative", Type: "create_entity", Entity: &types.ManagedEntityInput{Kind: "part", Name: "Alternative module B"}},
		{ID: "composition", Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: shelfNode(nodes, "model"), ObjectID: "$alternative", RelationType: "composed_of", Attributes: map[string]any{"module_type": "module_b"}}},
	}}, http.StatusCreated, &saved)
	alternative := shelfNode(saved, "alternative")
	create := func(name string, conditions []types.ShelfCondition) types.ShelfOperation {
		return types.ShelfOperation{ID: name, Type: "create_certification", Certification: &types.ShelfCertificationInput{Name: name, ShelfCertification: types.ShelfCertification{OwnerID: shelfNode(nodes, "model"), Standard: "standard_a", CertificationObject: "Object", Region: "region_a", Conditions: conditions}}}
	}
	f.request(http.MethodPost, path, types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{
		create("First coverage", []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "a")}}}),
		create("Second coverage", []types.ShelfCondition{{ModuleType: "module_b", PartIDs: []string{shelfNode(nodes, "fixed")}}}),
	}}, http.StatusCreated, nil)
	var result types.ShelfQueryResult
	query := "/libraries/" + library.ID + "/shelf/query"
	input := map[string]any{"certification": map[string]any{"standard": "standard_a"}}
	f.request(http.MethodPost, query, input, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.True(t, result.Items[0].PartialOptions)
	f.request(http.MethodPost, path, types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{
		create("Remaining coverage", []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "b")}}, {ModuleType: "module_b", PartIDs: []string{alternative}}}),
	}}, http.StatusCreated, nil)
	f.request(http.MethodPost, query, input, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.False(t, result.Items[0].PartialOptions)
}

// Medium spec, task 06 / ADR 0022: unrelated and scheduled edits must not
// rewrite the source of an existing fact or leak future values into history.
func TestShelfQueryHistoricalFactsKeepTheirSources(t *testing.T) {
	f, library := newProductShelfFixture(t)
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": types.ShelfConfig{Regions: []types.ShelfOption{{Key: "region_a", Name: "Region A"}}}}, http.StatusOK, nil)
	fileDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	var saved types.ShelfChangeset
	statuses := []types.ShelfSaleStatus{{Region: "region_a", Status: "GA"}}
	path := "/libraries/" + library.ID + "/shelf/changesets"
	f.request(http.MethodPost, path, types.ShelfChangesetInput{Source: types.OperationSource{Type: "import", ImportBatchID: "synthetic-history", FileDate: &fileDate}, Confirmed: true, Operations: []types.ShelfOperation{
		{ID: "model", Type: "create_entity", Entity: &types.ManagedEntityInput{Name: "Historical query model", Kind: "model"}},
		{ID: "status", Type: "set_sale_statuses", EntityID: "$model", Revision: 1, SaleStatuses: &statuses},
	}}, http.StatusCreated, &saved)
	id, sourceID := shelfNode(saved, "model"), saved.ID
	var detail types.ShelfNodeDetail
	nodePath := "/libraries/" + library.ID + "/shelf/nodes/" + id
	f.request(http.MethodGet, nodePath, nil, http.StatusOK, &detail)
	attributes := detail.Entity.Attributes
	attributes["combination_rules"] = []any{}
	f.request(http.MethodPost, path, types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "other", Type: "update_entity", EntityID: id, Patch: &types.ManagedEntityPatch{Revision: 2, Attributes: &attributes}}}}, http.StatusCreated, nil)
	future := time.Date(2090, 1, 1, 0, 0, 0, 0, time.UTC)
	f.request(http.MethodPost, path, types.ShelfChangesetInput{Source: types.OperationSource{Type: "agent", AgentName: "Schedule editor"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "scheduled", Type: "set_sale_statuses", EntityID: id, Revision: 3, EffectiveAt: &future, SaleStatuses: &[]types.ShelfSaleStatus{{Region: "region_a", Status: "EOM"}}}}}, http.StatusCreated, nil)
	for _, at := range []string{"2025-02-01T00:00:00Z", "2027-01-01T00:00:00Z"} {
		f.request(http.MethodGet, nodePath+"?as_of="+at, nil, http.StatusOK, &detail)
		require.Equal(t, "GA", detail.Shelf.SaleStatuses[0].Status)
		for _, fact := range detail.Facts {
			if fact.Field == "sale_statuses" {
				require.Equal(t, sourceID, fact.Evidence.ChangesetID)
				require.Equal(t, "synthetic-history", fact.Evidence.Source.ImportBatchID)
				require.True(t, fileDate.Equal(fact.Evidence.EffectiveAt))
			}
		}
	}
	f.request(http.MethodGet, nodePath+"?as_of=2090-02-01T00:00:00Z", nil, http.StatusOK, &detail)
	require.Equal(t, "EOM", detail.Shelf.SaleStatuses[0].Status)
	for _, fact := range detail.Facts {
		if fact.Field == "sale_statuses" {
			require.Equal(t, "Schedule editor", fact.Evidence.Source.AgentName)
			require.True(t, future.Equal(fact.Evidence.EffectiveAt))
		}
	}
	var result types.ShelfQueryResult
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/query", map[string]any{"as_of": "2024-01-01T00:00:00Z"}, http.StatusOK, &result)
	require.Zero(t, result.Total)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/query", map[string]any{"as_of": "2025-02-01T00:00:00Z", "sale_statuses": []types.ShelfStatusFilter{{Region: "region_a", Status: "GA"}}}, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
}

// Medium regression, task 06: an omitted or null condition list is the same
// unconditional fact as an empty list, including values written by entity edit.
func TestShelfQueryNullConditionsRemainUnconditional(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	attributes := map[string]any{"sale_statuses": []types.ShelfSaleStatus{{Region: "region_a", Status: "GA"}}}
	f.request(http.MethodPatch, "/entities/"+shelfNode(nodes, "model"), types.ManagedEntityPatch{Revision: 1, Attributes: &attributes}, http.StatusOK, nil)
	var result types.ShelfQueryResult
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/query", map[string]any{"sale_statuses": []types.ShelfStatusFilter{{Region: "region_a", Status: "GA"}}}, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.False(t, result.Items[0].PartialOptions)
}

// Medium spec, task 06: malformed filters are rejected and library-scoped node
// reads cannot expose an entity from another shelf or from an ordinary library.
func TestShelfQueryValidatesFiltersAndLibraryScope(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	path := "/libraries/" + library.ID + "/shelf/query"
	for _, input := range []any{
		map[string]any{"limit": -1}, map[string]any{"limit": 101}, map[string]any{"offset": -1}, map[string]any{"as_of": "bad"}, map[string]any{"as_of": "0001-01-01T00:00:00Z"},
		map[string]any{"sale_statuses": []any{map[string]any{"region": "unknown", "status": "GA"}}},
		map[string]any{"parameters": []any{map[string]any{"field": "unknown", "min": 1}}},
		map[string]any{"composition": []types.ShelfCondition{{ModuleType: "unknown", PartIDs: []string{shelfNode(nodes, "a")}}}},
		map[string]any{"certification": map[string]any{}}, map[string]any{"typo": true},
	} {
		f.request(http.MethodPost, path, input, http.StatusBadRequest, nil)
	}
	var found types.ShelfFindResult
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/find?q=%25", nil, http.StatusOK, &found)
	require.Zero(t, found.Total)
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/find?q=x&limit=invalid", nil, http.StatusBadRequest, nil)
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/nodes/missing", nil, http.StatusNotFound, nil)
	var other types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"directory": "another-shelf", "name": "Another shelf", "entity_mode": "managed", "shelf_enabled": true}, http.StatusCreated, &other)
	f.request(http.MethodGet, "/libraries/"+other.ID+"/shelf/nodes/"+shelfNode(nodes, "model"), nil, http.StatusNotFound, nil)
	var result types.ShelfQueryResult
	f.request(http.MethodPost, "/libraries/"+other.ID+"/shelf/query", map[string]any{"part_id": shelfNode(nodes, "a")}, http.StatusOK, &result)
	require.Zero(t, result.Total)
	f.request(http.MethodGet, "/libraries/"+f.library.ID+"/shelf/find?q=x", nil, http.StatusBadRequest, nil)
}

// Medium spec, task 06: get resolves the requested combination while keeping
// evidence and business time, and reports no record before the first snapshot.
func TestShelfQueryGetConditionsEvidenceAndHistory(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	id := shelfNode(nodes, "model")
	var saved types.ShelfChangeset
	statuses := []types.ShelfSaleStatus{{Region: "region_a", Status: "GA", Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "a")}}}}}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "agent", AgentName: "Synthetic editor"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "status", Type: "set_sale_statuses", EntityID: id, Revision: 1, SaleStatuses: &statuses}}}, http.StatusCreated, &saved)
	path := "/libraries/" + library.ID + "/shelf/nodes/" + id
	var result struct {
		Recorded bool                    `json:"recorded"`
		Shelf    *types.ShelfModelDetail `json:"shelf"`
		Facts    []struct {
			Field          string                 `json:"field"`
			PartialOptions bool                   `json:"partial_options"`
			Conditions     []types.ShelfCondition `json:"conditions"`
			Evidence       struct {
				Source      types.OperationSource `json:"source"`
				ChangesetID string                `json:"changeset_id"`
				EffectiveAt string                `json:"effective_at"`
				RecordedAt  string                `json:"recorded_at"`
			} `json:"evidence"`
		} `json:"facts"`
	}
	f.request(http.MethodGet, path, nil, http.StatusOK, &result)
	require.True(t, result.Recorded)
	require.Len(t, result.Shelf.SaleStatuses, 1)
	found := false
	for _, fact := range result.Facts {
		if fact.Field != "sale_statuses" {
			continue
		}
		found = true
		require.True(t, fact.PartialOptions)
		require.Len(t, fact.Conditions, 1)
		require.Equal(t, "Synthetic editor", fact.Evidence.Source.AgentName)
		require.Equal(t, saved.ID, fact.Evidence.ChangesetID)
		require.NotEmpty(t, fact.Evidence.EffectiveAt)
		require.NotEmpty(t, fact.Evidence.RecordedAt)
	}
	require.True(t, found)
	combination, _ := json.Marshal(map[string]string{"module_a": shelfNode(nodes, "b")})
	f.request(http.MethodGet, path+"?combination="+url.QueryEscape(string(combination)), nil, http.StatusOK, &result)
	require.Empty(t, result.Shelf.SaleStatuses)
	f.request(http.MethodGet, path+"?as_of=2000-01-01T00:00:00Z", nil, http.StatusOK, &result)
	require.False(t, result.Recorded)
	require.Empty(t, result.Facts)
	f.request(http.MethodGet, path+"?combination=null", nil, http.StatusBadRequest, nil)
	f.request(http.MethodGet, path+"?as_of=bad", nil, http.StatusBadRequest, nil)
}

// Medium spec, task 14: the status table can filter any region, a region with
// any state, planned retirement and genuinely absent records without inventing TBD.
func TestShelfStatusTableRegionalFilters(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	statuses := []types.ShelfSaleStatus{{Region: "region_a", Status: "GA", PlannedEOM: true, Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "a")}}}}, {Region: "region_a", Status: "待定", Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "b")}}}}}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "status", Type: "set_sale_statuses", EntityID: shelfNode(nodes, "model"), Revision: 1, SaleStatuses: &statuses}}}, http.StatusCreated, nil)
	for _, tc := range []struct {
		name    string
		filter  map[string]any
		total   int
		partial bool
	}{
		{"any region", map[string]any{"status": "GA"}, 1, true},
		{"any state", map[string]any{"region": "region_a"}, 1, false},
		{"planned retirement", map[string]any{"planned_eom": true}, 1, true},
		{"no record", map[string]any{"region": "region_a", "no_record": true}, 1, false},
		{"no records anywhere", map[string]any{"no_record": true}, 1, false},
		{"explicit TBD", map[string]any{"status": "待定"}, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var result types.ShelfQueryResult
			f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/query", map[string]any{"sale_statuses": []any{tc.filter}}, http.StatusOK, &result)
			require.EqualValues(t, tc.total, result.Total)
			require.Equal(t, tc.partial, result.Items[0].PartialOptions)
			if tc.filter["no_record"] == true {
				require.Equal(t, shelfNode(nodes, "single"), result.Items[0].Entity.ID)
			}
		})
	}
	for _, filter := range []map[string]any{{}, {"region": "unknown"}, {"status": "unknown"}, {"no_record": true, "status": "GA"}, {"no_record": true, "planned_eom": true}} {
		f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/query", map[string]any{"sale_statuses": []any{filter}}, http.StatusBadRequest, nil)
	}
}

// Facets count every matched model, not the page, so an agent can offer
// options with model counts before asking a vague question's conditions.
func TestShelfQueryFacetsCountMatchedModels(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	library.EntitySchema.Fields = append(library.EntitySchema.Fields, types.EntityFieldDefinition{Key: "energy", Name: "Energy", Type: "number", Kinds: []string{"model"}, ProductLineIDs: []string{shelfNode(nodes, "line")}})
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_schema": library.EntitySchema}, http.StatusOK, nil)
	a := []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "a")}}}
	b := []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "b")}}}
	statuses := []types.ShelfSaleStatus{{Region: "region_a", Status: "GA", Conditions: a}, {Region: "region_a", Status: "DEV", Conditions: b}}
	parameters := map[string][]types.ShelfParameterValue{"energy": {{Value: 4, Conditions: a}, {Value: map[string]any{"min": 9, "max": 54}, Conditions: b}}}
	operations := []types.ShelfOperation{
		{ID: "status", Type: "set_sale_statuses", EntityID: shelfNode(nodes, "model"), Revision: 1, SaleStatuses: &statuses},
		{ID: "parameter", Type: "set_parameter_values", EntityID: shelfNode(nodes, "model"), Revision: 2, ParameterValues: &parameters},
	}
	for owner, progress := range map[string]string{"platform": "granted", "a": "testing"} {
		operations = append(operations, types.ShelfOperation{ID: "cert_" + owner, Type: "create_certification", Certification: &types.ShelfCertificationInput{Name: "Facet certificate " + owner, ShelfCertification: types.ShelfCertification{OwnerID: shelfNode(nodes, owner), Standard: "Customer spelling", CertificationObject: "Synthetic object", Region: "region_a", Progress: progress}}})
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: operations}, http.StatusCreated, nil)
	path := "/libraries/" + library.ID + "/shelf/query"

	var raw map[string]any
	f.request(http.MethodPost, path, map[string]any{"limit": 1}, http.StatusOK, &raw)
	require.NotContains(t, raw, "facets")

	var result types.ShelfQueryResult
	f.request(http.MethodPost, path, map[string]any{"limit": 1, "facets": true}, http.StatusOK, &result)
	require.EqualValues(t, 2, result.Total)
	require.Len(t, result.Items, 1)
	require.NotNil(t, result.Facets)
	require.ElementsMatch(t, []types.ShelfStatusFacet{
		{Region: "region_a", RegionName: "Region A", Status: "DEV", Models: 1, ConditionalModels: 1},
		{Region: "region_a", RegionName: "Region A", Status: "GA", Models: 1, ConditionalModels: 1},
	}, result.Facets.SaleStatuses)
	parts := map[string]int64{}
	for _, facet := range result.Facets.Composition {
		parts[facet.PartName] = facet.Models
	}
	require.Equal(t, map[string]int64{"Synthetic a": 1, "Synthetic b": 1, "Synthetic fixed": 2}, parts)
	min, max := 4.0, 54.0
	require.Equal(t, []types.ShelfParameterFacet{{Field: "energy", Name: "Energy", Models: 1, Missing: 1, Min: &min, Max: &max}}, result.Facets.Parameters)
	require.ElementsMatch(t, []types.ShelfCertificationFacet{
		{Standard: "standard_a", StandardName: "Synthetic Standard", Progress: "granted", Region: "region_a", Models: 2},
		{Standard: "standard_a", StandardName: "Synthetic Standard", Progress: "testing", Region: "region_a", Models: 1},
	}, result.Facets.Certifications)

	// The models counted as missing can be listed without full details.
	f.request(http.MethodPost, path, map[string]any{"parameters": []any{map[string]any{"field": "energy", "missing": true}}}, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.Equal(t, shelfNode(nodes, "single"), result.Items[0].Entity.ID)
	f.request(http.MethodPost, path, map[string]any{"parameters": []any{map[string]any{"field": "energy", "missing": true, "min": 1}}}, http.StatusBadRequest, nil)

	// Facets follow the filters: only the model with a GA record remains.
	f.request(http.MethodPost, path, map[string]any{"limit": 1, "facets": true, "sale_statuses": []any{map[string]any{"region": "region_a", "status": "GA"}}}, http.StatusOK, &result)
	require.EqualValues(t, 1, result.Total)
	require.Equal(t, int64(0), result.Facets.Parameters[0].Missing)
	require.Equal(t, int64(1), result.Facets.Parameters[0].Models)
	for _, facet := range result.Facets.Composition {
		require.Equal(t, int64(1), facet.Models)
	}
}
