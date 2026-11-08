package router

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func compositionReportPlan(f *managedNamesFixture, library types.Library) {
	configureCertificationStandards(f, &library)
	importRecordPlan(f, library, "certification_report", []types.ShelfImportColumn{
		{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"},
		{Column: "D", Field: "region"}, {Column: "E", Field: "cell_part"}, {Column: "F", Field: "pack_model"},
		{Column: "G", Field: "cluster_model"}, {Column: "H", Field: "container_model"},
	}, map[string]any{"module_type": "module_a", "progress": "Completed"})
}

func seedReportComposition(t *testing.T, f *managedNamesFixture, library types.Library, nodes types.ShelfChangeset) string {
	t.Helper()
	var saved types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "cell", "type": "create_entity", "entity": map[string]any{"kind": "part", "name": "Brand / 300Ah", "effective_at": "2026-10-01T00:00:00Z", "attributes": map[string]any{"module_type": "module_a", "pack_model": "Pack"}}},
		map[string]any{"id": "composition", "type": "save_relation", "relation": map[string]any{"subject_id": shelfNode(nodes, "model"), "object_id": "$cell", "relation_type": "composed_of", "valid_from": "2026-10-01T00:00:00Z", "attributes": map[string]any{"module_type": "module_a", "cluster_model": "Cluster", "container_model": "Container"}}},
	}}, http.StatusCreated, &saved)
	return shelfNode(saved, "cell")
}

// Medium regression, task 28: the first row can equal an existing composition;
// a second row must not silently propose a different cluster for the same pair.
func TestShelfReportExistingCompositionStillDetectsRepeatedRowConflict(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	seedReportComposition(t, f, library, nodes)
	compositionReportPlan(f, library)
	rows := importRecordRow(3, "Synthetic model", "standard_a", "Container", "Country", "Brand / 300Ah", "Pack", "Cluster", "Container") +
		importRecordRow(4, "Synthetic model", "standard_a", "Container", "Other country", "Brand / 300Ah", "Pack", "Other cluster", "Container")
	draft := importUpload(t, f, library.ID, "report_20261028.xlsx", "", importWorkbook(t, rows, ""), http.StatusCreated)
	last := draft.Items[len(draft.Items)-1]
	require.Equal(t, "uncertain", last.Conflicts[0].Type)
	require.Contains(t, last.Conflicts[0].Message, "different attributes")
}

// Medium spec, task 28: manual composition edits retain the previous time
// interval and reject stale writers through the existing changeset boundary.
func TestShelfCompositionModelsEditingPreservesHistory(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	partID := seedReportComposition(t, f, library, nodes)
	path := "/libraries/" + library.ID + "/shelf/nodes/" + shelfNode(nodes, "model")
	var before types.ShelfNodeDetail
	f.request(http.MethodGet, path, nil, http.StatusOK, &before)
	var relation types.Relation
	for _, row := range before.Shelf.Composition[0].Relations {
		if row.ObjectID == partID {
			relation = row
		}
	}
	require.NotEmpty(t, relation.ID)
	op := types.ShelfOperation{ID: "edit", Type: "save_relation", RelationID: relation.ID, Revision: relation.Version, Relation: &types.ManagedRelationInput{SubjectID: relation.SubjectID, ObjectID: partID, RelationType: "composed_of", Version: relation.Version, Attributes: map[string]any{"module_type": "module_a", "cluster_model": " Edited cluster ", "container_model": ""}}}
	input := types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Operations: []types.ShelfOperation{op}, Confirmed: true}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", input, http.StatusCreated, nil)
	var after types.ShelfNodeDetail
	f.request(http.MethodGet, path, nil, http.StatusOK, &after)
	for _, row := range after.Shelf.Composition[0].Relations {
		if row.ObjectID == partID {
			require.Equal(t, " Edited cluster ", row.Attributes["cluster_model"])
			require.Equal(t, "", row.Attributes["container_model"])
			require.NotEqual(t, relation.ID, row.ID)
		}
	}
	f.request(http.MethodGet, path+"?as_of="+url.QueryEscape(before.AsOf.Format(time.RFC3339Nano)), nil, http.StatusOK, &after)
	for _, row := range after.Shelf.Composition[0].Relations {
		if row.ObjectID == partID {
			require.Equal(t, "Cluster", row.Attributes["cluster_model"])
		}
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", input, http.StatusConflict, nil)
}

// Medium spec, task 28 / ADR 0023: a cell identity is shared, while each
// model records its own cluster and container identifiers on its composition.
func TestShelfReportCompositionModelsBelongToModelAndCell(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	compositionReportPlan(f, library)
	rows := importRecordRow(3, "Synthetic model", "standard_a", "Container", "Country", " Brand / 300Ah ", " Pack ", " Cluster A ", "Container A") +
		importRecordRow(4, "Synthetic single", "standard_a", "Container", "Country", " Brand / 300Ah ", " Pack ", "Cluster B", "Container B")
	draft := importUpload(t, f, library.ID, "report_20261028.xlsx", "", importWorkbook(t, rows, ""), http.StatusCreated)
	require.Len(t, draft.Items, 5)
	decisions := []types.ShelfDecision{}
	for _, item := range draft.Items {
		decision := types.ShelfDecision{ItemID: item.ID, Action: "apply"}
		if item.Operation.Type == "update_certification" {
			op := item.Operation
			op.Type = "create_certification"
			decision.Action, decision.Replacement = "replace", &op
		} else {
			for _, conflict := range item.Conflicts {
				require.Equal(t, "new", conflict.Type, conflict.Message)
			}
		}
		decisions = append(decisions, decision)
	}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, &draft)
	partID := draft.Items[0].EntityID
	var part types.EntityDetail
	f.request(http.MethodGet, "/entities/"+partID, nil, http.StatusOK, &part)
	require.Equal(t, " Brand / 300Ah ", part.Entity.CanonicalName)
	require.Equal(t, " Pack ", part.Entity.Attributes["pack_model"])
	require.NotContains(t, part.Entity.Attributes, "cluster_model")
	require.NotContains(t, part.Entity.Attributes, "container_model")
	for _, sample := range []struct{ node, cluster, container string }{{"model", " Cluster A ", "Container A"}, {"single", "Cluster B", "Container B"}} {
		var detail types.ShelfNodeDetail
		f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/nodes/"+shelfNode(nodes, sample.node), nil, http.StatusOK, &detail)
		found := false
		for _, fact := range detail.Facts {
			if fact.Field != "composed_of" {
				continue
			}
			relation := fact.Value.(map[string]any)
			if relation["object_id"] != partID {
				continue
			}
			found = true
			attrs := relation["attributes"].(map[string]any)
			require.Equal(t, sample.cluster, attrs["cluster_model"])
			require.Equal(t, sample.container, attrs["container_model"])
			require.Equal(t, draft.ID, fact.Evidence.ChangesetID)
		}
		require.True(t, found)
	}
}

// Medium regression, task 28: differing values for one model and cell, or
// differing Pack values for a shared cell, must still return to the submitter.
func TestShelfReportCompositionModelConflictsRemainScoped(t *testing.T) {
	for _, sample := range []struct{ name, owner, pack, cluster, container string }{
		{"cluster", "Synthetic model", "Pack", "Other cluster", "Container"},
		{"container", "Synthetic model", "Pack", "Cluster", "Other container"},
		{"pack", "Synthetic single", "Other pack", "Cluster", "Container"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			f, library, _ := shelfModelSetup(t)
			compositionReportPlan(f, library)
			rows := importRecordRow(3, "Synthetic model", "standard_a", "Container", "Country", "Brand / 300Ah", "Pack", "Cluster", "Container") +
				importRecordRow(4, sample.owner, "standard_a", "Container", "Other country", "Brand / 300Ah", sample.pack, sample.cluster, sample.container)
			draft := importUpload(t, f, library.ID, "report_20261028.xlsx", "", importWorkbook(t, rows, ""), http.StatusCreated)
			last := draft.Items[len(draft.Items)-1]
			require.Equal(t, "uncertain", last.Conflicts[0].Type)
			require.Contains(t, last.Conflicts[0].Message, "different")
		})
	}
}
