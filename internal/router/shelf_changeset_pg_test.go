package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"
)

// Medium spec, task 03 / ADR 0023: proposing a change only persists a draft;
// confirmation makes it visible through the ordinary entity read API.
func TestShelfChangesetProposalDoesNotWriteCatalog(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft struct {
		ID       string
		Revision int64
		Status   string
		Items    []struct {
			ID        string
			EntityID  string `json:"entity_id"`
			Conflicts []struct{ Type string }
		}
	}
	f.request(http.MethodPost, path, map[string]any{
		"source":     map[string]any{"type": "agent", "agent_name": "Synthetic assistant"},
		"operations": []any{map[string]any{"id": "line", "type": "create_entity", "entity": map[string]any{"name": " Synthetic line ", "kind": "product_line"}}},
	}, http.StatusCreated, &draft)
	require.Equal(t, "pending", draft.Status)
	require.Len(t, draft.Items, 1)
	require.Equal(t, "new", draft.Items[0].Conflicts[0].Type)
	f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusNotFound, nil)
	f.request(http.MethodGet, path+"/"+draft.ID, nil, http.StatusOK, &draft)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "line", "action": "apply"}}}, http.StatusOK, &draft)
	require.Equal(t, "confirmed", draft.Status)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusOK, &detail)
	require.Equal(t, " Synthetic line ", detail.Entity.CanonicalName)
}

// Medium spec, task 03: a blocked item prevents the whole confirmation, and
// the corrected draft can be saved and resumed without writing catalog data.
func TestShelfChangesetRequiredFieldsBlockAtomicConfirmation(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{
		map[string]any{"id": "good", "type": "create_entity", "entity": map[string]any{"name": "Good line", "kind": "product_line"}},
		map[string]any{"id": "missing", "type": "create_entity", "entity": map[string]any{"kind": "part"}},
	}}, http.StatusCreated, &draft)
	require.Equal(t, "required_missing", draft.Items[1].Conflicts[0].Type)
	review := map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "good", "action": "apply"}, map[string]any{"item_id": "missing", "action": "apply"}}}
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", review, http.StatusConflict, nil)
	f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusNotFound, nil)
	review["decisions"] = []any{map[string]any{"item_id": "good", "action": "apply"}, map[string]any{"item_id": "missing", "action": "replace", "replacement": map[string]any{"type": "create_entity", "entity": map[string]any{"name": "Fixed part", "kind": "part"}}}}
	f.request(http.MethodPatch, path+"/"+draft.ID, review, http.StatusOK, &draft)
	f.request(http.MethodGet, "/entities/"+draft.Items[1].EntityID, nil, http.StatusNotFound, nil)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision}, http.StatusOK, &draft)
	for _, item := range draft.Items {
		f.request(http.MethodGet, "/entities/"+item.EntityID, nil, http.StatusOK, nil)
	}
}

// Medium spec, task 03: existing single-edit routes are one-operation,
// immediately confirmed changesets, with source attribution on their logs.
func TestShelfChangesetWebEditsUseChangesets(t *testing.T) {
	f, library := newProductShelfFixture(t)
	var entity types.Entity
	response := personalRequest(t, f.router, http.MethodPost, "/api/v1/libraries/"+library.ID+"/entities", `{"kind":"model","name":"Web model"}`, "", false)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var envelope struct{ Data types.Entity }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	entity = envelope.Data
	var drafts types.ShelfChangesetPage
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/changesets?status=confirmed", nil, http.StatusOK, &drafts)
	require.EqualValues(t, 1, drafts.Total)
	require.Equal(t, "web", drafts.Items[0].Source.Type)
	var history types.OperationLogPage
	f.request(http.MethodGet, "/entities/"+entity.ID+"/history", nil, http.StatusOK, &history)
	require.Len(t, history.Items, 1)
	require.Equal(t, drafts.Items[0].ID, history.Items[0].ChangesetID)
	require.Equal(t, "web", history.Items[0].Source.Type)
	f.request(http.MethodPatch, "/entities/"+entity.ID, map[string]any{"revision": entity.Revision, "description": "Explicit API edit"}, http.StatusOK, nil)
	f.request(http.MethodGet, "/entities/"+entity.ID+"/history?source_type=agent&agent_name=personal%20integration", nil, http.StatusOK, &history)
	require.EqualValues(t, 1, history.Total)
	require.Equal(t, "personal integration", history.Items[0].Source.AgentName)
}

// Medium spec, task 03 / ADR 0023: import proposals expose hand-edited old
// values and need an item decision before replacing them.
func TestShelfChangesetImportProtectsManualValues(t *testing.T) {
	f, library := newProductShelfFixture(t)
	var entity types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "model", "name": "Manual model", "description": "Hand corrected"}, http.StatusCreated, &entity)
	var draft types.ShelfChangeset
	path := "/libraries/" + library.ID + "/shelf/changesets"
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "import", "import_batch_id": "synthetic-batch", "file_date": "2026-11-01T00:00:00Z"}, "operations": []any{map[string]any{"id": "edit", "type": "update_entity", "entity_id": entity.ID, "patch": map[string]any{"revision": entity.Revision, "description": "Spreadsheet value"}}}}, http.StatusCreated, &draft)
	require.Equal(t, "modified", draft.Items[0].Conflicts[0].Type)
	require.NotEmpty(t, draft.Items[0].Conflicts)
	require.Equal(t, "Hand corrected", draft.Items[0].Conflicts[0].ManualBefore["description"])
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision}, http.StatusConflict, nil)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+entity.ID, nil, http.StatusOK, &detail)
	require.Equal(t, "Hand corrected", detail.Entity.Description)
	f.request(http.MethodPatch, path+"/"+draft.ID, map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "edit", "action": "defer"}}}, http.StatusOK, &draft)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision}, http.StatusOK, &draft)
	f.request(http.MethodGet, path+"/"+draft.DeferredChangesetID, nil, http.StatusOK, &draft)
	require.NotEmpty(t, draft.Items[0].Conflicts)
	require.Equal(t, "Hand corrected", draft.Items[0].Conflicts[0].ManualBefore["description"])
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "edit", "action": "apply"}}}, http.StatusOK, nil)
	f.request(http.MethodGet, "/entities/"+entity.ID, nil, http.StatusOK, &detail)
	require.Equal(t, "Spreadsheet value", detail.Entity.Description)
	f.router.GET("/api/v1/operation-logs", handler.NewOperationLogHandler(service.NewOperationLogService(f.db)).List)
	var logs types.OperationLogPage
	f.request(http.MethodGet, "/operation-logs?source_type=import&import_batch_id=synthetic-batch", nil, http.StatusOK, &logs)
	require.EqualValues(t, 1, logs.Total)
	require.Equal(t, draft.ID, logs.Items[0].ChangesetID)
	require.Equal(t, "synthetic-batch", logs.Items[0].Source.ImportBatchID)
	f.request(http.MethodGet, "/operation-logs?source_type=unknown", nil, http.StatusBadRequest, nil)
}

// Medium spec, task 03: missing rows stay active until explicitly kept or
// retired; retirement preserves the entity and its history.
func TestShelfChangesetMissingRowsNeedRetireOrKeep(t *testing.T) {
	for _, action := range []string{"keep", "retire"} {
		t.Run(action, func(t *testing.T) {
			f, library := newProductShelfFixture(t)
			var entity types.Entity
			f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "model", "name": "Missing model"}, http.StatusCreated, &entity)
			path := "/libraries/" + library.ID + "/shelf/changesets"
			var draft types.ShelfChangeset
			f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "missing", "type": "missing_entity", "entity_id": entity.ID, "revision": entity.Revision}}}, http.StatusCreated, &draft)
			require.Equal(t, "disappeared", draft.Items[0].Conflicts[0].Type)
			f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "missing", "action": "apply"}}}, http.StatusConflict, nil)
			var detail types.EntityDetail
			f.request(http.MethodGet, "/entities/"+entity.ID, nil, http.StatusOK, &detail)
			require.Nil(t, detail.Entity.ArchivedAt)
			f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "missing", "action": action}}}, http.StatusOK, nil)
			f.request(http.MethodGet, "/entities/"+entity.ID, nil, http.StatusOK, &detail)
			require.Equal(t, action == "retire", detail.Entity.ArchivedAt != nil)
		})
	}
}

// Medium spec, task 03: a submitter can identify a possible existing entity;
// accepting that identity saves the submitted spelling as an alias.
func TestShelfChangesetSuspectedIdentityBecomesAlias(t *testing.T) {
	f, library := newProductShelfFixture(t)
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": types.ShelfConfig{ModuleTypes: []types.ShelfOption{{Key: "module_a", Name: "Synthetic module"}}}}, http.StatusOK, nil)
	var entity types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "part", "name": "Synthetic cell"}, http.StatusCreated, &entity)
	var model types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "model", "name": "Alias model"}, http.StatusCreated, &model)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "agent", "agent_name": "Import helper"}, "operations": []any{
		map[string]any{"id": "alias", "type": "create_entity", "candidate_entity_id": entity.ID, "entity": map[string]any{"kind": "part", "name": " Synthetic cell variant "}},
		map[string]any{"id": "composition", "type": "save_relation", "relation": map[string]any{"subject_id": model.ID, "object_id": "$alias", "relation_type": "composed_of", "attributes": map[string]any{"module_type": "module_a"}}},
	}}, http.StatusCreated, &draft)
	require.Equal(t, "suspected_same", draft.Items[0].Conflicts[0].Type)
	require.Equal(t, entity.ID, draft.Items[0].Conflicts[0].Candidates[0].OtherEntityID)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "alias", "action": "same_entity", "target_entity_id": entity.ID, "target_revision": entity.Revision}, map[string]any{"item_id": "composition", "action": "apply"}}}, http.StatusOK, &draft)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+entity.ID, nil, http.StatusOK, &detail)
	require.Contains(t, detail.Entity.Aliases, " Synthetic cell variant ")
	require.Equal(t, "Synthetic cell", detail.Entity.CanonicalName)
	var groups []service.RelationGroup
	f.request(http.MethodGet, "/entities/"+model.ID+"/relations", nil, http.StatusOK, &groups)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Relations, 1)
	require.Equal(t, entity.ID, groups[0].Relations[0].ObjectID)
}

// Medium spec, task 03: a deferred issue is transferred once, without guessing
// its meaning or losing saved decisions when the other items are confirmed.
func TestShelfChangesetDeferredItemsRemainPending(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "agent", "agent_name": "Synthetic assistant"}, "operations": []any{
		map[string]any{"id": "good", "type": "create_entity", "entity": map[string]any{"name": "Known model", "kind": "model"}},
		map[string]any{"id": "uncertain", "type": "create_entity", "entity": map[string]any{"name": "Unknown model", "kind": "model"}, "issues": []any{map[string]any{"type": "uncertain", "message": "Submitter must clarify the empty cell"}}},
	}}, http.StatusCreated, &draft)
	f.request(http.MethodPatch, path+"/"+draft.ID, map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "good", "action": "apply"}, map[string]any{"item_id": "uncertain", "action": "defer"}}}, http.StatusOK, &draft)
	f.request(http.MethodGet, path+"/"+draft.ID, nil, http.StatusOK, &draft)
	require.Equal(t, "defer", draft.Items[1].Decision.Action)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision}, http.StatusOK, &draft)
	require.NotEmpty(t, draft.DeferredChangesetID)
	var pending types.ShelfChangeset
	f.request(http.MethodGet, path+"/"+draft.DeferredChangesetID, nil, http.StatusOK, &pending)
	require.Len(t, pending.Items, 1)
	require.Equal(t, "uncertain", pending.Items[0].Conflicts[0].Type)
	require.Nil(t, pending.Items[0].Decision)
	f.request(http.MethodGet, "/entities/"+pending.Items[0].EntityID, nil, http.StatusNotFound, nil)
	f.request(http.MethodPost, path+"/"+pending.ID+"/confirm", map[string]any{"revision": pending.Revision, "decisions": []any{map[string]any{"item_id": "uncertain", "action": "apply"}}}, http.StatusConflict, nil)
}

// Medium spec, task 03: revision checks reject the later writer, while log
// filters distinguish the submitter from the person confirming a draft.
func TestShelfChangesetStaleRevisionsAndSourceQueries(t *testing.T) {
	f, library := newProductShelfFixture(t)
	var entity types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "model", "name": "Concurrent model"}, http.StatusCreated, &entity)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	propose := func(name string) types.ShelfChangeset {
		t.Helper()
		var draft types.ShelfChangeset
		f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "agent", "agent_name": name}, "operations": []any{map[string]any{"id": "edit", "type": "update_entity", "entity_id": entity.ID, "patch": map[string]any{"revision": entity.Revision, "description": name}}}}, http.StatusCreated, &draft)
		return draft
	}
	first, second := propose("First assistant"), propose("Second assistant")
	for i, draft := range []types.ShelfChangeset{first, second} {
		status := http.StatusOK
		if i == 1 {
			status = http.StatusConflict
		}
		f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "edit", "action": "apply"}}}, status, nil)
	}
	var history types.OperationLogPage
	f.request(http.MethodGet, "/entities/"+entity.ID+"/history?source_type=agent&agent_name=First%20assistant", nil, http.StatusOK, &history)
	require.EqualValues(t, 1, history.Total)
	require.Equal(t, "First assistant", history.Items[0].Source.AgentName)
	require.Equal(t, first.ID, history.Items[0].ChangesetID)
	f.request(http.MethodGet, "/entities/"+entity.ID+"/history?source_type=agent&agent_name=Second%20assistant", nil, http.StatusOK, &history)
	require.Zero(t, history.Total)
	f.request(http.MethodPatch, path+"/"+second.ID, map[string]any{"revision": second.Revision, "decisions": []any{map[string]any{"item_id": "edit", "action": "defer"}}}, http.StatusOK, nil)
	f.request(http.MethodPatch, path+"/"+second.ID, map[string]any{"revision": second.Revision}, http.StatusConflict, nil)
}

// Medium spec, task 03: a real database failure after a preceding write rolls
// back entities, attribute history, operation logs and confirmation together.
// A temporary trigger replaces only the failing PostgreSQL system boundary.
func TestShelfChangesetDatabaseFailureRollsBackEveryOperation(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{
		map[string]any{"id": "first", "type": "create_entity", "entity": map[string]any{"kind": "part", "name": "First atomic part"}},
		map[string]any{"id": "second", "type": "create_entity", "entity": map[string]any{"kind": "part", "name": "Second atomic part", "description": "Injected failure"}},
	}}, http.StatusCreated, &draft)
	require.NoError(t, f.db.Exec(`CREATE SEQUENCE shelf_failure_attempt;
 CREATE FUNCTION shelf_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.description='Injected failure' AND nextval('shelf_failure_attempt')=2 THEN RAISE EXCEPTION 'injected confirmation failure'; END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER shelf_failure BEFORE INSERT ON entities FOR EACH ROW EXECUTE FUNCTION shelf_failure()`).Error)
	review := map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "first", "action": "apply"}, map[string]any{"item_id": "second", "action": "apply"}}}
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", review, http.StatusInternalServerError, nil)
	for _, item := range draft.Items {
		f.request(http.MethodGet, "/entities/"+item.EntityID, nil, http.StatusNotFound, nil)
	}
	var pending types.ShelfChangeset
	f.request(http.MethodGet, path+"/"+draft.ID, nil, http.StatusOK, &pending)
	require.Equal(t, "pending", pending.Status)
	require.Equal(t, draft.Revision, pending.Revision)
	require.NoError(t, f.db.Exec("DROP TRIGGER shelf_failure ON entities; DROP FUNCTION shelf_failure(); DROP SEQUENCE shelf_failure_attempt").Error)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", review, http.StatusOK, &draft)
	for _, item := range draft.Items {
		var history types.OperationLogPage
		f.request(http.MethodGet, "/entities/"+item.EntityID+"/history", nil, http.StatusOK, &history)
		require.EqualValues(t, 1, history.Total)
	}
}

// Medium spec, task 03 / ADR 0023: a batch can reference newly proposed nodes;
// cross-kind names still need an explicit identity-specific acknowledgement.
func TestShelfChangesetCrossKindConfirmationAndHierarchy(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{
		map[string]any{"id": "line", "type": "create_entity", "entity": map[string]any{"kind": "product_line", "name": "Shared shelf name"}},
		map[string]any{"id": "platform", "type": "create_entity", "entity": map[string]any{"kind": "platform", "name": "Shared shelf name"}},
		map[string]any{"id": "parent", "type": "save_relation", "relation": map[string]any{"subject_id": "$platform", "object_id": "$line", "relation_type": "belongs_to"}},
	}}, http.StatusCreated, &draft)
	require.Equal(t, "cross_kind_same_name", draft.Items[1].Conflicts[0].Type)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "line", "action": "apply"}, map[string]any{"item_id": "platform", "action": "apply"}, map[string]any{"item_id": "parent", "action": "apply"}}}, http.StatusConflict, nil)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{
		map[string]any{"item_id": "line", "action": "apply"},
		map[string]any{"item_id": "platform", "action": "apply", "name_confirmations": []any{map[string]any{"name": "Shared shelf name", "other_entity_id": draft.Items[0].EntityID, "other_kind": "product_line"}}},
		map[string]any{"item_id": "parent", "action": "apply"},
	}}, http.StatusOK, &draft)
	var relations []service.RelationGroup
	f.request(http.MethodGet, "/entities/"+draft.Items[1].EntityID+"/relations", nil, http.StatusOK, &relations)
	require.Len(t, relations, 1)
	require.Equal(t, draft.Items[0].EntityID, relations[0].Relations[0].ObjectID)
	// Existing confirmation survives an ordinary webpage edit.
	f.request(http.MethodPatch, "/entities/"+draft.Items[1].EntityID, map[string]any{"revision": 1, "description": "Confirmed platform"}, http.StatusOK, nil)
}

// Medium spec, task 03: unparseable, uncertain and contradictory input stays
// blocked until replaced, discarded or deferred; apply never guesses a value.
func TestShelfChangesetUncertainInputCannotBeAcceptedUnchanged(t *testing.T) {
	for _, kind := range []string{"unparseable", "uncertain", "required_missing", "cross_table_contradiction"} {
		t.Run(kind, func(t *testing.T) {
			f, library := newProductShelfFixture(t)
			path := "/libraries/" + library.ID + "/shelf/changesets"
			var draft types.ShelfChangeset
			f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "agent", "agent_name": "Synthetic submitter"}, "operations": []any{map[string]any{"id": "blocked", "type": "create_entity", "entity": map[string]any{"kind": "model", "name": "Ambiguous model"}, "issues": []any{map[string]any{"type": kind, "message": "Submitter clarification required"}}}}}, http.StatusCreated, &draft)
			require.Equal(t, kind, draft.Items[0].Conflicts[0].Type)
			f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "blocked", "action": "apply"}}}, http.StatusConflict, nil)
			f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusNotFound, nil)
		})
	}
}

// Medium spec, task 03: importing another attribute does not erase the manual
// provenance of a previously corrected value on the same entity.
func TestShelfChangesetImportMarksEachManualAttribute(t *testing.T) {
	f, library := newProductShelfFixture(t)
	fields := append(library.EntitySchema.Fields, types.EntityFieldDefinition{Key: "capacity", Name: "Capacity", Type: "number", Kinds: []string{"model"}}, types.EntityFieldDefinition{Key: "note", Name: "Note", Type: "text", Kinds: []string{"model"}})
	schema := library.EntitySchema
	schema.Fields = fields
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_schema": schema}, http.StatusOK, nil)
	var entity types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "model", "name": "Attribute model", "attributes": map[string]any{"capacity": 100}}, http.StatusCreated, &entity)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	submit := func(capacity int, confirmed bool) types.ShelfChangeset {
		t.Helper()
		var draft types.ShelfChangeset
		f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "import", "import_batch_id": "attribute-batch", "file_date": "2026-11-01T00:00:00Z"}, "confirmed": confirmed, "operations": []any{map[string]any{"id": "edit", "type": "update_entity", "entity_id": entity.ID, "patch": map[string]any{"revision": entity.Revision, "attributes": map[string]any{"capacity": capacity, "note": "Imported note"}}}}}, http.StatusCreated, &draft)
		return draft
	}
	submit(100, true)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+entity.ID, nil, http.StatusOK, &detail)
	entity = detail.Entity
	draft := submit(50, false)
	require.Equal(t, map[string]any{"capacity": float64(100)}, draft.Items[0].Conflicts[0].ManualBefore["attributes"])
	require.NotContains(t, draft.Items[0].Conflicts[0].ManualBefore, "note")
}

// Medium property, task 03: the review state machine follows this independent
// transition table, including stale writes and repeated confirmation.
func TestShelfChangesetReviewStateModel(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "part", "type": "create_entity", "entity": map[string]any{"kind": "part", "name": "State model part"}}}}, http.StatusCreated, &draft)
	for _, step := range []struct {
		method, suffix string
		revision       int
		status         int
		wantState      string
		wantRevision   int
	}{
		{http.MethodPatch, "", 1, http.StatusOK, "pending", 2},
		{http.MethodPatch, "", 1, http.StatusConflict, "pending", 2},
		{http.MethodPost, "/confirm", 1, http.StatusConflict, "pending", 2},
		{http.MethodPost, "/confirm", 2, http.StatusOK, "confirmed", 3},
		{http.MethodPost, "/confirm", 3, http.StatusConflict, "confirmed", 3},
		{http.MethodPatch, "", 3, http.StatusConflict, "confirmed", 3},
	} {
		f.request(step.method, path+"/"+draft.ID+step.suffix, map[string]any{"revision": step.revision, "decisions": []any{map[string]any{"item_id": "part", "action": "apply"}}}, step.status, nil)
		var saved types.ShelfChangeset
		f.request(http.MethodGet, path+"/"+draft.ID, nil, http.StatusOK, &saved)
		require.Equal(t, step.wantState, saved.Status)
		require.EqualValues(t, step.wantRevision, saved.Revision)
		entityStatus := http.StatusNotFound
		if step.wantState == "confirmed" {
			entityStatus = http.StatusOK
		}
		f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, entityStatus, nil)
	}
}

// Medium spec, task 03: a draft cannot read or change another shelf, even
// when the caller supplies a valid entity or draft id from that other shelf.
func TestShelfChangesetStaysWithinItsLibrary(t *testing.T) {
	f, library := newProductShelfFixture(t)
	var other types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Other shelf", "directory": "other-shelf", "entity_mode": "managed", "shelf_enabled": true}, http.StatusCreated, &other)
	var entity types.Entity
	f.request(http.MethodPost, "/libraries/"+other.ID+"/entities", map[string]any{"kind": "part", "name": "Other part"}, http.StatusCreated, &entity)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "edit", "type": "archive_entity", "entity_id": entity.ID, "revision": entity.Revision}}}, http.StatusCreated, &draft)
	f.request(http.MethodGet, "/libraries/"+other.ID+"/shelf/changesets/"+draft.ID, nil, http.StatusNotFound, nil)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "edit", "action": "apply"}}}, http.StatusConflict, nil)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+entity.ID, nil, http.StatusOK, &detail)
	require.Nil(t, detail.Entity.ArchivedAt)
}

// Medium spec, task 03: basic typed operations keep aliases and history when
// archiving a part; a future attribute edit remains scheduled at its own time.
func TestShelfChangesetTypedEditsPreserveAttributeTime(t *testing.T) {
	f, library := newProductShelfFixture(t)
	schema := library.EntitySchema
	schema.Fields = append(schema.Fields, types.EntityFieldDefinition{Key: "capacity", Name: "Capacity", Type: "number", Kinds: []string{"part"}})
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_schema": schema}, http.StatusOK, nil)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "import", "import_batch_id": "part-batch", "file_date": "2026-01-01T00:00:00Z"}, "confirmed": true, "operations": []any{map[string]any{"id": "part", "type": "create_entity", "entity": map[string]any{"kind": "part", "name": " Timed part ", "attributes": map[string]any{"capacity": 10}}}}}, http.StatusCreated, &draft)
	id := draft.Items[0].EntityID
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "schedule", "type": "update_entity", "entity_id": id, "patch": map[string]any{"revision": 1, "effective_at": "2090-01-01T00:00:00Z", "attributes": map[string]any{"capacity": 20}}}}}, http.StatusCreated, nil)
	var current types.EntityDetail
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &current)
	require.EqualValues(t, 10, current.Entity.Attributes["capacity"])
	var future service.EntityAttributesResult
	f.request(http.MethodGet, "/entities/"+id+"/attributes?at=2090-01-02", nil, http.StatusOK, &future)
	require.EqualValues(t, 20, future.Attributes["capacity"])
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "alias", "type": "add_alias", "entity_id": id, "revision": 2, "alias": " Alternate part "},
		map[string]any{"id": "archive", "type": "archive_entity", "entity_id": id, "revision": 3},
	}}, http.StatusCreated, nil)
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &current)
	require.NotNil(t, current.Entity.ArchivedAt)
	require.Contains(t, current.Entity.Aliases, " Alternate part ")
	var history types.OperationLogPage
	f.request(http.MethodGet, "/entities/"+id+"/history?source_type=import&import_batch_id=part-batch", nil, http.StatusOK, &history)
	require.EqualValues(t, 1, history.Total)
}

// Medium regression, task 03: deferred operations keep references to entities
// confirmed in the original batch, so the remaining draft can finish later.
func TestShelfChangesetDeferredReferencesSurviveTransfer(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{
		map[string]any{"id": "line", "type": "create_entity", "entity": map[string]any{"kind": "product_line", "name": "Deferred line"}},
		map[string]any{"id": "platform", "type": "create_entity", "entity": map[string]any{"kind": "platform", "name": "Deferred platform"}},
		map[string]any{"id": "parent", "type": "save_relation", "relation": map[string]any{"subject_id": "$platform", "object_id": "$line", "relation_type": "belongs_to"}},
	}}, http.StatusCreated, &draft)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "line", "action": "apply"}, map[string]any{"item_id": "platform", "action": "defer"}, map[string]any{"item_id": "parent", "action": "defer"}}}, http.StatusOK, &draft)
	lineID, platformID := draft.Items[0].EntityID, draft.Items[1].EntityID
	f.request(http.MethodGet, path+"/"+draft.DeferredChangesetID, nil, http.StatusOK, &draft)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "platform", "action": "apply"}, map[string]any{"item_id": "parent", "action": "apply"}}}, http.StatusOK, nil)
	var groups []service.RelationGroup
	f.request(http.MethodGet, "/entities/"+platformID+"/relations", nil, http.StatusOK, &groups)
	require.Len(t, groups, 1)
	require.Equal(t, lineID, groups[0].Relations[0].ObjectID)
}
