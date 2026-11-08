package router

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Medium spec, task 23: missing object/region columns are reviewable row
// conflicts. Discarding the rows never creates a certification or a default.
func TestShelfCertImportMissingColumnsRemainReviewable(t *testing.T) {
	for _, kind := range []string{"certification_progress", "certification_report"} {
		t.Run(kind, func(t *testing.T) {
			f, l, n := shelfModelSetup(t)
			configureCertificationStandards(f, &l)
			importRecordPlan(f, l, kind, []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}}, map[string]any{"progress": "Completed"})
			draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Synthetic model", "standard_a"), ""), http.StatusCreated)
			require.Len(t, draft.Items, 1)
			require.NotNil(t, draft.Items[0].Operation.Certification)
			require.Equal(t, "required_missing", draft.Items[0].Conflicts[0].Type)
			f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "apply"}}}, http.StatusConflict, nil)
			f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "discard"}}}, http.StatusOK, nil)
			var model types.EntityDetail
			f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &model)
			require.Empty(t, model.Shelf.Certifications)
		})
	}
}

// Medium spec, task 23: standard years resolve separately and never become
// aliases. Ambiguous spellings and conflicting explicit years remain blocked.
func TestShelfCertImportStandardYears(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	f.request(http.MethodGet, "/libraries/"+l.ID, nil, http.StatusOK, &l)
	config := l.ShelfConfig
	config.Standards = []types.ShelfStandard{{Key: "ul_a", Name: "UL9540A"}, {Key: "iec_a", Name: "IEC 62619"}, {Key: "ul_b", Name: "UL1973"}}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "standards", "type": "configure_shelf", "library_updated_at": l.UpdatedAt, "library": map[string]any{"shelf_config": config}}}}, http.StatusCreated, nil)
	importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}}, nil)
	for _, sample := range []struct {
		raw, key string
		year     int
	}{{"UL9540A:2019", "ul_a", 2019}, {"IEC 62619:2022", "iec_a", 2022}, {"UL1973：2022", "ul_b", 2022}} {
		t.Run(sample.raw, func(t *testing.T) {
			draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Synthetic model", sample.raw, "Container", "Country"), ""), http.StatusCreated)
			op := draft.Items[0].Operation
			require.NotNil(t, op.Certification)
			require.Equal(t, sample.raw, op.Certification.Name)
			require.Equal(t, sample.key, op.Certification.Standard)
			require.Equal(t, sample.year, op.Certification.Year)
			op.Type = "create_certification"
			f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: op.ID, Action: "replace", Replacement: &op}}}, http.StatusOK, &draft)
			var cert types.EntityDetail
			f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusOK, &cert)
			require.EqualValues(t, sample.year, cert.Entity.Attributes["certification"].(map[string]any)["year"])
		})

	}
	for _, raw := range []string{"UL9540A:2019:2022", "UL9540A edition unknown"} {
		f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Ambiguous", "owner_id": shelfNode(n, "model"), "standard": raw, "certification_object": "Container", "region": "Country"}}}}, http.StatusConflict, nil)
	}
}

// Medium spec, task 23: delimiter splitting is a suggestion until an explicit
// replace decision expands the row. Each resulting proposal stays reviewable.
func TestShelfCertImportSplitRequiresExplicitReplacement(t *testing.T) {
	for _, separator := range []string{"、", "，", ",", ";", "；", "\n", "; ;"} {
		t.Run(separator, func(t *testing.T) {
			f, l, n := shelfModelSetup(t)
			configureCertificationStandards(f, &l)
			importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}}, nil)
			raw := "Synthetic Standard:2019" + separator + "Customer spelling:2022"
			draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Synthetic model", raw, "Container", "Country"), ""), http.StatusCreated)
			require.Len(t, draft.Items, 1)
			require.NotNil(t, draft.Items[0].Operation.Certification)
			details, ok := draft.Items[0].Conflicts[0].Details.(map[string]any)
			require.True(t, ok)
			suggestions, ok := details["split_suggestions"].([]any)
			require.True(t, ok)
			require.Len(t, suggestions, 2)
			path := "/libraries/" + l.ID + "/shelf/changesets/" + draft.ID
			f.request(http.MethodPost, path+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "apply"}}}, http.StatusConflict, nil)
			replacements := []any{}
			for i, rawSuggestion := range suggestions {
				record := rawSuggestion.(map[string]any)
				require.Equal(t, []string{"Synthetic Standard:2019", "Customer spelling:2022"}[i], record["name"])
				replacements = append(replacements, map[string]any{"id": []string{draft.Items[0].ID, "split_second"}[i], "type": "create_certification", "certification": record, "certification_owner": map[string]any{"name": "Synthetic model", "kind": "model"}})
			}
			f.request(http.MethodPatch, path, map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": draft.Items[0].ID, "action": "replace", "replacements": replacements}}}, http.StatusOK, &draft)
			require.Len(t, draft.Items, 2)
			for _, item := range draft.Items {
				require.Equal(t, "Map", item.Operation.ImportLocation.Worksheet)
			}
			var model types.EntityDetail
			f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &model)
			require.Empty(t, model.Shelf.Certifications)
			f.request(http.MethodPost, path+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "apply"}, {ItemID: draft.Items[1].ID, Action: "discard"}}}, http.StatusOK, &draft)
			f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &model)
			require.Len(t, model.Shelf.Certifications, 1)
			require.Equal(t, 2019, model.Shelf.Certifications[0].Certification.Year)
		})
	}
}

// Medium spec, task 23: exact corrections, explicit existing-node mapping and
// separate-node creation all reuse replace. Aliases wait for final confirmation.
func TestShelfCertImportOwnerChoices(t *testing.T) {
	for _, choice := range []string{"rewrite", "existing", "alias", "new", "discard"} {
		t.Run(choice, func(t *testing.T) {
			f, l, n := shelfModelSetup(t)
			configureCertificationStandards(f, &l)
			importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}}, nil)
			draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, " Unmatched edition ", "standard_a", "Container", "Country"), ""), http.StatusCreated)
			require.Len(t, draft.Items, 1)
			require.NotNil(t, draft.Items[0].Operation.Certification)
			require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
			path := "/libraries/" + l.ID + "/shelf/changesets/" + draft.ID
			if choice == "discard" {
				f.request(http.MethodPost, path+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "discard"}}}, http.StatusOK, nil)
				return
			}
			record := map[string]any{"name": "Chosen certification", "owner_id": shelfNode(n, "model"), "standard": "standard_a", "certification_object": "Container", "region": "Country"}
			owner := map[string]any{"name": " Unmatched edition ", "kind": "model"}
			ops := []any{}
			id := draft.Items[0].ID
			if choice == "rewrite" {
				record["owner_id"] = ""
				owner["name"] = "Synthetic model"
			}
			if choice == "alias" {
				ops = append(ops, map[string]any{"id": id, "type": "add_alias", "entity_id": shelfNode(n, "model"), "revision": 1, "alias": " Unmatched edition "})
			}
			if choice == "new" {
				// A new model without its product parent is not a valid correction.
				bad := []any{map[string]any{"id": id, "type": "create_entity", "entity": map[string]any{"kind": "model", "name": " Unmatched edition "}}}
				f.request(http.MethodPatch, path, map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": id, "action": "replace", "replacements": bad}}}, http.StatusBadRequest, nil)
				ops = append(ops, map[string]any{"id": id, "type": "create_entity", "parent_id": shelfNode(n, "product"), "entity": map[string]any{"kind": "model", "name": " Unmatched edition "}})
				record["owner_id"] = "$" + id
			}
			certID := id
			if len(ops) > 0 {
				certID = "corrected_cert"
			}
			ops = append(ops, map[string]any{"id": certID, "type": "create_certification", "certification": record, "certification_owner": owner})
			f.request(http.MethodPatch, path, map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": id, "action": "replace", "replacements": ops}}}, http.StatusOK, &draft)
			var model types.EntityDetail
			f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &model)
			require.NotContains(t, model.Entity.Aliases, " Unmatched edition ")
			require.Empty(t, model.Shelf.Certifications)
			confirmImport(t, f, l.ID, draft)
			f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &model)
			if choice == "alias" {
				require.Contains(t, model.Entity.Aliases, " Unmatched edition ")
			} else {
				require.NotContains(t, model.Entity.Aliases, " Unmatched edition ")
			}
			if choice == "new" {
				require.Empty(t, model.Shelf.Certifications)
				f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusOK, &model)
				require.Equal(t, " Unmatched edition ", model.Entity.CanonicalName)
			}
			require.Len(t, model.Shelf.Certifications, 1)
			if choice == "alias" {
				next := importUpload(t, f, l.ID, "cert_20261029.xlsx", "", importWorkbook(t, importRecordRow(3, " Unmatched edition ", "standard_a", "Container", "Country"), ""), http.StatusCreated)
				require.Equal(t, "modified", next.Items[0].Conflicts[0].Type)
			}
		})
	}
}

// Medium spec, task 23: all missing-field rows survive import independently.
// Uniform or per-row values use the same replace contract and can be refused.
func TestShelfCertImportMissingRowsCanBeFilledAndRejected(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "progress"}}, nil)
	draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Synthetic model", "standard_a", "Planning")+importRecordRow(4, "Synthetic model", "standard_a", "Completed"), ""), http.StatusCreated)
	require.Len(t, draft.Items, 2)
	path := "/libraries/" + l.ID + "/shelf/changesets/" + draft.ID
	decisions := []types.ShelfDecision{}
	for i, item := range draft.Items {
		require.NotNil(t, item.Operation.Certification)
		require.Equal(t, "required_missing", item.Conflicts[0].Type)
		op := item.Operation
		record := *op.Certification
		op.Certification = &record
		record.CertificationObject = "Container"
		record.Region = "Country"
		record.Name = []string{"First row", "Second row"}[i]
		op.Type = "create_certification"
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: "replace", Replacement: &op})
	}
	f.request(http.MethodPatch, path, types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, &draft)
	f.request(http.MethodPost, path+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "apply"}, {ItemID: draft.Items[1].ID, Action: "discard"}}}, http.StatusOK, nil)
	var model types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &model)
	require.Len(t, model.Shelf.Certifications, 1)
	require.Equal(t, "Planning", model.Shelf.Certifications[0].Certification.Progress)
}

// Medium spec, task 23 / ADR 0023: MCP shares the reviewed replacement expansion,
// optional alias, explicit worksheet values and standard-year resolution with HTTP.
func TestShelfMCPSubmitCertificationConflictDecisions(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	client := f.connect(t, "2025-11-25")
	var draft types.ShelfChangeset
	op := map[string]any{"id": "row", "type": "update_certification", "certification_owner": map[string]any{"name": "Alternate model", "kind": "model"}, "certification": map[string]any{"owner_id": "", "standard": "Synthetic Standard:2019、Customer spelling:2022", "certification_object": "", "region": ""}}
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": l.ID, "source": map[string]any{"type": "agent", "agent_name": "Synthetic importer"}, "operations": []any{op}}, &draft)
	require.Equal(t, "required_missing", draft.Items[0].Conflicts[0].Type)
	shelfMCPError(t, client, "shelf_submit", map[string]any{"library_id": l.ID, "changeset_id": draft.ID, "revision": draft.Revision, "confirmed": true, "decisions": []any{map[string]any{"item_id": "row", "action": "apply"}}})
	ops := []any{map[string]any{"id": "row", "type": "add_alias", "entity_id": shelfNode(n, "model"), "revision": 1, "alias": "Alternate model"}}
	for i, raw := range []string{"Synthetic Standard:2019", "Customer spelling:2022"} {
		ops = append(ops, map[string]any{"id": []string{"first_cert", "second_cert"}[i], "type": "create_certification", "certification_owner": map[string]any{"name": "Alternate model", "kind": "model"}, "certification": map[string]any{"name": []string{"MCP first", "MCP second"}[i], "owner_id": "", "standard": raw, "certification_object": "Container", "region": "Country"}})
	}
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": l.ID, "changeset_id": draft.ID, "revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "row", "action": "replace", "replacements": ops}}}, &draft)
	require.Len(t, draft.Items, 3)
	require.Equal(t, "pending", draft.Status)
	var before types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &before)
	require.NotContains(t, before.Entity.Aliases, "Alternate model")
	require.Empty(t, before.Shelf.Certifications)
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": l.ID, "changeset_id": draft.ID, "revision": draft.Revision, "confirmed": true, "decisions": []any{map[string]any{"item_id": "row", "action": "apply"}, map[string]any{"item_id": "first_cert", "action": "apply"}, map[string]any{"item_id": "second_cert", "action": "discard"}}}, &draft)
	f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &before)
	require.Contains(t, before.Entity.Aliases, "Alternate model")
	require.Len(t, before.Shelf.Certifications, 1)
	require.Equal(t, 2019, before.Shelf.Certifications[0].Certification.Year)
}

// Medium regression, task 23: report rows with unknown model spellings retain
// their complete certification, even when the report also proposes a new part.
func TestShelfCertImportReportUnknownOwnerRetainsRecord(t *testing.T) {
	f, l, _ := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	importRecordPlan(f, l, "certification_report", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "cell_part"}, {Column: "D", Field: "number"}}, map[string]any{"progress": "Completed", "module_type": "module_a"})
	draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Unknown model", "standard_a", "New synthetic cell", "REPORT-1"), ""), http.StatusCreated)
	found := false
	for _, item := range draft.Items {
		if item.Operation.Certification != nil {
			found = true
			require.Equal(t, "REPORT-1", item.Operation.Certification.Number)
			require.Equal(t, "Unknown model", item.Operation.CertificationOwner.Name)
		}
	}
	require.True(t, found, "the unknown owner must leave an editable certification proposal")
}

// Medium spec, task 23: correcting an owner revalidates the full key, captures
// the matched record revision and updates that record rather than duplicating it.
func TestShelfCertImportCorrectedOwnerRevalidatesExistingRecord(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	var seed types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "effective_at": "2026-10-01T00:00:00Z", "certification": map[string]any{"name": "Existing cert", "owner_id": shelfNode(n, "model"), "standard": "standard_a", "certification_object": "Container", "region": "Country", "progress": "Planning", "number": "KEEP-1", "year": 2018}}}}, http.StatusCreated, &seed)
	importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}, {Column: "E", Field: "progress"}}, nil)
	draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Wrong model", "standard_a", "Container", "Country", "Completed"), ""), http.StatusCreated)
	op := draft.Items[0].Operation
	op.CertificationOwner.Name = "Synthetic model"
	path := "/libraries/" + l.ID + "/shelf/changesets/" + draft.ID
	f.request(http.MethodPatch, path, types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: op.ID, Action: "replace", Replacement: &op}}}, http.StatusOK, &draft)
	require.Equal(t, "modified", draft.Items[0].Conflicts[0].Type)
	require.Equal(t, seed.Items[0].EntityID, draft.Items[0].EntityID)
	confirmImport(t, f, l.ID, draft)
	var model types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &model)
	require.Len(t, model.Shelf.Certifications, 1)
	require.Equal(t, "Completed", model.Shelf.Certifications[0].Certification.Progress)
	require.Equal(t, "KEEP-1", model.Shelf.Certifications[0].Certification.Number)
	require.Equal(t, 2018, model.Shelf.Certifications[0].Certification.Year)
}

// Medium spec, task 23: malformed replacement expansions never change the
// pending state or catalog. A model and its invalid parent roll back together.
func TestShelfCertImportReplacementRejectsInvalidExpansion(t *testing.T) {
	f, l, _ := shelfModelSetup(t)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "original", "type": "create_entity", "entity": map[string]any{"kind": "product_line", "name": "Untouched original"}}}}, http.StatusCreated, &draft)
	path := "/libraries/" + l.ID + "/shelf/changesets/" + draft.ID
	for _, payload := range []map[string]any{
		{"replacements": []any{}},
		{"replacement": map[string]any{"type": "create_entity", "entity": map[string]any{"kind": "product_line", "name": "One"}}, "replacements": []any{map[string]any{"type": "create_entity", "entity": map[string]any{"kind": "product_line", "name": "Two"}}}},
		{"replacements": []any{map[string]any{"type": "create_entity", "entity": map[string]any{"kind": "product_line", "name": "One"}}, map[string]any{"id": "original", "type": "create_entity", "entity": map[string]any{"kind": "product_line", "name": "Two"}}}},
		{"replacement": map[string]any{"type": "create_entity", "parent_id": "invalid-parent"}},
	} {
		payload["item_id"] = "original"
		payload["action"] = "replace"
		f.request(http.MethodPatch, path, map[string]any{"revision": draft.Revision, "decisions": []any{payload}}, http.StatusBadRequest, nil)
		var current types.ShelfChangeset
		f.request(http.MethodGet, path, nil, http.StatusOK, &current)
		require.Equal(t, draft.Revision, current.Revision)
		require.Equal(t, "Untouched original", current.Items[0].Operation.Entity.Name)
	}
	// Parent failure must not leave the new identity behind in the preview or confirmation.
	replacement := map[string]any{"type": "create_entity", "parent_id": "invalid-parent", "entity": map[string]any{"kind": "model", "name": "Invalid parent model"}}
	f.request(http.MethodPost, path+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "original", "action": "replace", "replacement": replacement}}}, http.StatusConflict, nil)
	var found types.ShelfFindResult
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/find?q=Invalid%20parent%20model", nil, http.StatusOK, &found)
	require.Zero(t, found.Total)
}

// Medium spec, task 23: an explicit year disagreement stays editable. Resolving
// it does not add an edition to canonical standard names or aliases.
func TestShelfCertImportConflictingYearsNeedCorrection(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	importRecordPlan(f, l, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}, {Column: "E", Field: "year"}}, nil)
	draft := importUpload(t, f, l.ID, "cert_20261028.xlsx", "", importWorkbook(t, importRecordRow(3, "Synthetic model", "Synthetic Standard:2022", "Container", "Country", "2019"), ""), http.StatusCreated)
	require.NotNil(t, draft.Items[0].Operation.Certification)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
	path := "/libraries/" + l.ID + "/shelf/changesets/" + draft.ID
	f.request(http.MethodPost, path+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "apply"}}}, http.StatusConflict, nil)
	op := draft.Items[0].Operation
	op.Type = "create_certification"
	op.Certification.Name = "Chosen year"
	op.Certification.Standard = "standard_a"
	f.request(http.MethodPost, path+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: op.ID, Action: "replace", Replacement: &op}}}, http.StatusOK, nil)
	var model types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(n, "model"), nil, http.StatusOK, &model)
	require.Equal(t, 2019, model.Shelf.Certifications[0].Certification.Year)
	for _, standard := range []types.ShelfStandard{{Key: "edition_name", Name: "Synthetic Standard:2022"}, {Key: "edition_alias", Name: "Plain standard", Aliases: []string{"Plain standard：2022"}}} {
		config := l.ShelfConfig
		config.Standards = append(append([]types.ShelfStandard{}, l.ShelfConfig.Standards...), standard)
		f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": config}, http.StatusBadRequest, nil)
	}
}
