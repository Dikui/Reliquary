package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

// This stub replaces the external generation model at the chat boundary.
type shelfSuggestChat struct {
	mcpFixtureChat
	respond func(context.Context, []chat.Message) (*types.ChatResponse, error)
}

func (m shelfSuggestChat) Chat(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	return m.respond(ctx, messages)
}

func shelfSuggestModel(f *managedNamesFixture, library *types.Library) {
	f.t.Helper()
	var model types.Model
	f.request(http.MethodPost, "/models", map[string]any{"name": "Synthetic suggestion model", "type": "KnowledgeQA", "source": "remote"}, http.StatusCreated, &model)
	tiers := library.ModelTiers
	tiers.Generation = model.ID
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"model_tiers": tiers}, http.StatusOK, library)
}

func shelfSuggestPlanUpload(t *testing.T, f *managedNamesFixture, libraryID string, fields map[string]string, data []byte, status int) types.ShelfChangeset {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range fields {
		require.NoError(t, form.WriteField(key, value))
	}
	part, err := form.CreateFormFile("file", "synthetic.xlsx")
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, form.Close())
	r := httptest.NewRequest(http.MethodPost, "/api/v1/libraries/"+libraryID+"/shelf/import-plans/suggest", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-API-Key", f.key)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	require.Equal(t, status, w.Code, w.Body.String())
	var result struct{ Data types.ShelfChangeset }
	if status == http.StatusCreated {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	}
	return result.Data
}

// Medium spec, task 13 / ADR 0023: first-file suggestions stay editable and
// absent from confirmed plans until a person confirms the changeset.
func TestShelfImportSuggestPlanRequiresEditableConfirmation(t *testing.T) {
	model := shelfSuggestChat{respond: func(ctx context.Context, messages []chat.Message) (*types.ChatResponse, error) {
		purpose, _ := types.LLMCallMetadataFromContext(ctx)
		require.Equal(t, "shelf_import_suggest", purpose)
		require.Equal(t, types.ModelTierGeneration, types.ModelTierFromContext(ctx))
		require.Contains(t, messages[1].Content, "Header group")
		require.Contains(t, messages[1].Content, "Model A;Model B")
		return &types.ChatResponse{Content: `{"plan":{"name":" Suggested map ","sheets":[{"worksheet":"Map","header_rows":2,"columns":[{"column":"A","field":"platform"},{"column":"B","field":"product"},{"column":"C","field":"model"}]}],"model_separators":[";"]},"uncertainties":["Choose the product line"]}`}, nil
	}}
	f, library := newProductShelfFixture(t, mcpProcessingFixture{chat: model})
	shelfSuggestModel(f, &library)
	data := importWorkbook(t, importRow(3, "Platform", "Product", "Model A;Model B", "GA", "12"), "")
	draft := shelfSuggestPlanUpload(t, f, library.ID, map[string]string{"plan_key": "map", "file_kind": "product_map"}, data, http.StatusCreated)
	require.Equal(t, "pending", draft.Status)
	require.Len(t, draft.Items, 1)
	op := draft.Items[0].Operation
	require.Equal(t, "save_import_plan", op.Type)
	require.Equal(t, " Suggested map ", op.ImportPlan.Name)
	require.Equal(t, "map", op.ImportPlan.Key)
	require.Equal(t, int64(0), op.ImportPlan.Revision)
	require.Equal(t, []string{";"}, op.ImportPlan.ModelSeparators)
	require.NotEmpty(t, draft.Items[0].Conflicts)
	var plans []types.ShelfImportPlan
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/import-plans", nil, http.StatusOK, &plans)
	require.Empty(t, plans)
	var line types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "product_line", "name": "Chosen line"}, http.StatusCreated, &line)
	op.ImportPlan.Sheets[0].ProductLineID = line.ID
	op.Issues = nil
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: op.ID, Action: "replace", Replacement: &op}}}, http.StatusOK, nil)
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/import-plans", nil, http.StatusOK, &plans)
	require.Len(t, plans, 1)
	require.Equal(t, line.ID, plans[0].Definition.Sheets[0].ProductLineID)
}

// Medium spec, task 13: description parsing returns a blocking proposal,
// retains the exact text and source cell, and writes only after explicit review.
func TestShelfImportSuggestParametersStayPendingUntilReviewed(t *testing.T) {
	model := shelfSuggestChat{respond: func(_ context.Context, messages []chat.Message) (*types.ChatResponse, error) {
		require.Contains(t, messages[1].Content, "Energy between 9 and 54 kWh")
		require.Contains(t, messages[1].Content, `"energy"`)
		return &types.ChatResponse{Content: `{"parameter_values":{"energy":[{"value":{"min":9,"max":54},"conditions":[]}]},"uncertainties":[]}`}, nil
	}}
	f, library, nodes := shelfModelSetup(t, mcpProcessingFixture{chat: model})
	shelfSuggestModel(f, &library)
	fields := append(library.EntitySchema.Fields, types.EntityFieldDefinition{Key: "energy", Name: "Energy", Type: "number", Kinds: []string{"model"}, ProductLineIDs: []string{shelfNode(nodes, "line")}})
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_schema": types.EntitySchema{Kinds: library.EntitySchema.Kinds, Fields: fields}}, http.StatusOK, &library)
	text := " Energy between 9 and 54 kWh "
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/imports/suggest-text", map[string]any{
		"kind": "parameters", "text": text,
		"operation":       map[string]any{"type": "set_parameter_values", "entity_id": shelfNode(nodes, "model"), "revision": 1},
		"import_location": map[string]any{"worksheet": "Map", "cells": []string{"F3"}},
	}, http.StatusCreated, &draft)
	require.Equal(t, "pending", draft.Status)
	require.Len(t, draft.Items, 1)
	item := draft.Items[0]
	require.Equal(t, "uncertain", item.Conflicts[0].Type)
	require.Equal(t, []string{"F3"}, item.Operation.ImportLocation.Cells)
	require.Contains(t, string(shelfJSONForTest(t, item.Conflicts)), text)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model"), nil, http.StatusOK, &detail)
	require.Empty(t, detail.Shelf.Parameters)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: item.ID, Action: "apply"}}}, http.StatusConflict, nil)
	op := item.Operation
	op.Issues = nil
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: item.ID, Action: "replace", Replacement: &op}}}, http.StatusOK, nil)
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model"), nil, http.StatusOK, &detail)
	require.Equal(t, map[string]any{"min": float64(9), "max": float64(54)}, detail.Shelf.Parameters["energy"][0].Value)
}

func shelfJSONForTest(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

// Medium spec, task 13 / ADR 0023: sample descriptions propose conditions
// on explicitly supplied certification records without changing their identity.
func TestShelfImportSuggestSampleConditionsRequireReview(t *testing.T) {
	partID := ""
	model := shelfSuggestChat{respond: func(_ context.Context, messages []chat.Message) (*types.ChatResponse, error) {
		require.Contains(t, messages[1].Content, "Sample uses Synthetic a")
		require.Contains(t, messages[1].Content, partID)
		return &types.ChatResponse{Content: string(shelfJSONForTest(t, map[string]any{"conditions": []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{partID}}}, "uncertainties": []string{}}))}, nil
	}}
	f, library, nodes := shelfModelSetup(t, mcpProcessingFixture{chat: model})
	shelfSuggestModel(f, &library)
	partID = shelfNode(nodes, "a")
	config := library.ShelfConfig
	config.Standards = []types.ShelfStandard{{Key: "standard_a", Name: "Standard A"}}
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"shelf_config": config}, http.StatusOK, &library)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/imports/suggest-text", map[string]any{
		"kind": "conditions", "text": "Sample uses Synthetic a",
		"operation": map[string]any{"type": "create_certification", "certification": map[string]any{"name": " Original certificate ", "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "Synthetic object", "region": "Original region", "progress": "Original progress"}},
	}, http.StatusCreated, &draft)
	require.Len(t, draft.Items, 1)
	op := draft.Items[0].Operation
	require.Equal(t, []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{partID}}}, op.Certification.Conditions)
	require.Equal(t, " Original certificate ", op.Certification.Name)
	require.Equal(t, "Original progress", op.Certification.Progress)
	var certs []types.ShelfCertificationView
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/certifications", nil, http.StatusOK, &certs)
	require.Empty(t, certs)
	op.Issues = nil
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: op.ID, Action: "replace", Replacement: &op}}}, http.StatusOK, nil)
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/certifications", nil, http.StatusOK, &certs)
	require.Len(t, certs, 1)
	require.Equal(t, partID, certs[0].Certification.Conditions[0].PartIDs[0])
}

// Medium spec, task 13: multiple model names are editable proposals beneath
// an explicit product; names keep their spaces and no combined model is saved.
func TestShelfImportSuggestModelsPreserveNamesUntilConfirmation(t *testing.T) {
	model := shelfSuggestChat{respond: func(_ context.Context, _ []chat.Message) (*types.ChatResponse, error) {
		return &types.ChatResponse{Content: `{"models":[" Model A","Model B "],"uncertainties":[]}`}, nil
	}}
	f, library, nodes := shelfModelSetup(t, mcpProcessingFixture{chat: model})
	shelfSuggestModel(f, &library)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/imports/suggest-text", map[string]any{"kind": "models", "text": " Model A;Model B ", "parent_id": shelfNode(nodes, "product")}, http.StatusCreated, &draft)
	require.Len(t, draft.Items, 4)
	decisions := []types.ShelfDecision{}
	for _, item := range draft.Items {
		require.Equal(t, "uncertain", item.Conflicts[0].Type)
		op := item.Operation
		op.Issues = nil
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: "replace", Replacement: &op})
	}
	var found types.ShelfFindResult
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/find?q=Model+A", nil, http.StatusOK, &found)
	require.Empty(t, found.Items)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, &draft)
	for index, name := range []string{" Model A", "Model B "} {
		var detail types.EntityDetail
		f.request(http.MethodGet, "/entities/"+draft.Items[index*2].EntityID, nil, http.StatusOK, &detail)
		require.Equal(t, name, detail.Entity.CanonicalName)
	}
}

// Medium spec, task 13: failed or incomplete external responses do not leave
// a saved plan or draft that could later be mistaken for a valid suggestion.
func TestShelfImportSuggestInvalidModelResponsesLeaveNoDraft(t *testing.T) {
	var response *types.ChatResponse
	var modelErr error
	model := shelfSuggestChat{respond: func(context.Context, []chat.Message) (*types.ChatResponse, error) { return response, modelErr }}
	f, library := newProductShelfFixture(t, mcpProcessingFixture{chat: model})
	shelfSuggestModel(f, &library)
	data := importWorkbook(t, importRow(3, "Platform", "Product", "Model", "GA", "1"), "")
	var before, after types.ShelfChangesetPage
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/changesets", nil, http.StatusOK, &before)
	for _, tc := range []struct {
		name     string
		response *types.ChatResponse
		err      error
		status   int
	}{
		{"unavailable", nil, fmt.Errorf("synthetic model unavailable"), http.StatusInternalServerError},
		{"nil", nil, nil, http.StatusBadRequest},
		{"truncated", &types.ChatResponse{Content: `{"plan":`, FinishReason: "length"}, nil, http.StatusBadRequest},
		{"malformed", &types.ChatResponse{Content: "not JSON"}, nil, http.StatusBadRequest},
		{"trailing", &types.ChatResponse{Content: `{"plan":null} {"plan":null}`}, nil, http.StatusBadRequest},
		{"unknown member", &types.ChatResponse{Content: `{"plan":null,"confirmed":true}`}, nil, http.StatusBadRequest},
		{"no plan", &types.ChatResponse{Content: `{"plan":null,"uncertainties":["Unknown worksheet"]}`}, nil, http.StatusBadRequest},
		{"filtered", &types.ChatResponse{Content: `{"plan":{"name":"Incomplete","sheets":[{"worksheet":"Map","ignore":true}]}}`, FinishReason: "content_filter"}, nil, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, modelErr = tc.response, tc.err
			shelfSuggestPlanUpload(t, f, library.ID, map[string]string{"plan_key": "map", "file_kind": "product_map"}, data, tc.status)
		})
	}
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/changesets", nil, http.StatusOK, &after)
	require.Equal(t, before.Total, after.Total)
	var plans []types.ShelfImportPlan
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/import-plans", nil, http.StatusOK, &plans)
	require.Empty(t, plans)
}

// Medium spec, task 13: suggestions use only this library's configured model
// and cannot replace an already confirmed import plan on their own.
func TestShelfImportSuggestRequiresLocalModelAndNewPlanKey(t *testing.T) {
	model := shelfSuggestChat{respond: func(context.Context, []chat.Message) (*types.ChatResponse, error) {
		t.Fatal("a rejected request must not call the external model")
		return nil, nil
	}}
	f, library := newProductShelfFixture(t, mcpProcessingFixture{chat: model})
	data := importWorkbook(t, importRow(3, "Platform", "Product", "Model", "GA", "1"), "")
	shelfSuggestPlanUpload(t, f, library.ID, map[string]string{"plan_key": "map", "file_kind": "product_map"}, data, http.StatusBadRequest)
	plan := types.ShelfImportPlanInput{Key: "map", Name: "Original plan", FileKind: "product_map", ShelfImportDefinition: types.ShelfImportDefinition{Sheets: []types.ShelfImportSheet{{Worksheet: "Map", Ignore: true}}}}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "plan", Type: "save_import_plan", ImportPlan: &plan}}}, http.StatusCreated, nil)
	shelfSuggestPlanUpload(t, f, library.ID, map[string]string{"plan_key": "map", "file_kind": "product_map"}, data, http.StatusConflict)
}

// Medium spec, task 13: ambiguous model cells stay visible as blocking
// conflicts and never become inferred records.
func TestShelfImportSuggestUncertainTextRemainsAConflict(t *testing.T) {
	model := shelfSuggestChat{respond: func(context.Context, []chat.Message) (*types.ChatResponse, error) {
		return &types.ChatResponse{Content: `{"models":[],"uncertainties":["The slash could be part of the model name"]}`}, nil
	}}
	f, library, nodes := shelfModelSetup(t, mcpProcessingFixture{chat: model})
	shelfSuggestModel(f, &library)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/imports/suggest-text", map[string]any{"kind": "models", "text": "Model A/B", "parent_id": shelfNode(nodes, "product")}, http.StatusCreated, &draft)
	require.Len(t, draft.Items, 1)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
	require.Contains(t, string(shelfJSONForTest(t, draft.Items[0].Conflicts)), "The slash could be part of the model name")
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "apply"}}}, http.StatusConflict, nil)
}

// Medium spec, task 13: mapped sample descriptions are preserved for review,
// and ordinary deterministic imports do not invoke the external model.
func TestShelfImportSampleDescriptionBlocksUnreviewedCertification(t *testing.T) {
	model := shelfSuggestChat{respond: func(context.Context, []chat.Message) (*types.ChatResponse, error) {
		t.Fatal("deterministic import must not call the external model")
		return nil, nil
	}}
	f, library, nodes := shelfModelSetup(t, mcpProcessingFixture{chat: model})
	configureCertificationStandards(f, &library)
	var seed types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "cert", Type: "create_certification", Certification: &types.ShelfCertificationInput{Name: "Sample certificate", ShelfCertification: types.ShelfCertification{OwnerID: shelfNode(nodes, "model"), Standard: "standard_a", CertificationObject: "Container", Region: "Country", Progress: "Original"}}}}}, http.StatusCreated, &seed)
	importRecordPlan(f, library, "certification_progress", []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}, {Column: "E", Field: "progress"}, {Column: "F", Field: "sample_description"}}, nil)
	draft := importUpload(t, f, library.ID, "sample.xlsx", time.Now().UTC().Format(time.RFC3339Nano), importWorkbook(t, importRecordRow(3, "Synthetic model", "Standard A", "Container", "Country", "Proposed", "Uses Synthetic a"), ""), http.StatusCreated)
	require.Len(t, draft.Items, 1)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
	require.Equal(t, "Uses Synthetic a", draft.Items[0].Operation.ImportLocation.Values["F3"])
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(seed, "cert"), nil, http.StatusOK, &detail)
	var record types.ShelfCertification
	require.NoError(t, types.DecodeShelfValue(detail.Entity.Attributes["certification"], &record))
	require.Equal(t, "Original", record.Progress)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: draft.Items[0].ID, Action: "apply"}}}, http.StatusConflict, nil)
}

// Medium spec, task 13: a description suggestion cannot overwrite a model
// changed by another writer while the person is reviewing the suggestion.
func TestShelfImportSuggestPreservesRevisionConflicts(t *testing.T) {
	model := shelfSuggestChat{respond: func(context.Context, []chat.Message) (*types.ChatResponse, error) {
		return &types.ChatResponse{Content: `{"parameter_values":{"energy":[{"value":54,"conditions":[]}]}}`}, nil
	}}
	f, library, nodes := shelfModelSetup(t, mcpProcessingFixture{chat: model})
	shelfSuggestModel(f, &library)
	fields := append(library.EntitySchema.Fields, types.EntityFieldDefinition{Key: "energy", Name: "Energy", Type: "number", Kinds: []string{"model"}, ProductLineIDs: []string{shelfNode(nodes, "line")}})
	f.request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_schema": types.EntitySchema{Kinds: library.EntitySchema.Kinds, Fields: fields}}, http.StatusOK, &library)
	id := shelfNode(nodes, "model")
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/imports/suggest-text", map[string]any{"kind": "parameters", "text": "Energy 54", "operation": map[string]any{"type": "set_parameter_values", "entity_id": id, "revision": 1}}, http.StatusCreated, &draft)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "newer", "type": "set_parameter_values", "entity_id": id, "revision": 1, "parameter_values": map[string]any{"energy": []any{map[string]any{"value": 18}}}}}}, http.StatusCreated, nil)
	op := draft.Items[0].Operation
	op.Issues = nil
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: op.ID, Action: "replace", Replacement: &op}}}, http.StatusConflict, nil)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+id, nil, http.StatusOK, &detail)
	require.Equal(t, float64(18), detail.Shelf.Parameters["energy"][0].Value)
}

// Medium spec, task 13: even a model that invents a part cannot make its
// condition pass normal certification validation after review.
func TestShelfImportSuggestUnknownPartsCannotBeConfirmed(t *testing.T) {
	model := shelfSuggestChat{respond: func(context.Context, []chat.Message) (*types.ChatResponse, error) {
		return &types.ChatResponse{Content: `{"conditions":[{"module_type":"module_a","part_ids":["unidentified-part"]}]}`}, nil
	}}
	f, library, nodes := shelfModelSetup(t, mcpProcessingFixture{chat: model})
	shelfSuggestModel(f, &library)
	configureCertificationStandards(f, &library)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/imports/suggest-text", map[string]any{"kind": "conditions", "text": "An unidentified sample", "operation": map[string]any{"type": "create_certification", "certification": map[string]any{"name": "Uncertain certificate", "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "Object", "region": "Region"}}}, http.StatusCreated, &draft)
	op := draft.Items[0].Operation
	op.Issues = nil
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: op.ID, Action: "replace", Replacement: &op}}}, http.StatusConflict, nil)
	var certs []types.ShelfCertificationView
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/certifications", nil, http.StatusOK, &certs)
	require.Empty(t, certs)
}

// Medium spec, task 13 / ADR 0023: names invented by the model remain evidence
// in a conflict and are never prefilled as names that could be saved unchanged.
func TestShelfImportSuggestInventedNamesRequireOriginalNames(t *testing.T) {
	model := shelfSuggestChat{respond: func(context.Context, []chat.Message) (*types.ChatResponse, error) {
		return &types.ChatResponse{Content: `{"models":["Invented Model"]}`}, nil
	}}
	f, library, nodes := shelfModelSetup(t, mcpProcessingFixture{chat: model})
	shelfSuggestModel(f, &library)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/imports/suggest-text", map[string]any{"kind": "models", "text": "Model A/B", "parent_id": shelfNode(nodes, "product")}, http.StatusCreated, &draft)
	require.Equal(t, "unparseable", draft.Items[0].Conflicts[0].Type)
	require.Empty(t, draft.Items[0].Operation.Entity.Name)
	require.Contains(t, string(shelfJSONForTest(t, draft.Items[0].Conflicts)), "Invented Model")
}

// Medium spec, task 13: another effective_at cannot conceal a future file date.
func TestShelfImportSuggestRejectsFutureImportSource(t *testing.T) {
	model := shelfSuggestChat{respond: func(context.Context, []chat.Message) (*types.ChatResponse, error) {
		return &types.ChatResponse{Content: `{"models":["Model A"]}`}, nil
	}}
	f, library, nodes := shelfModelSetup(t, mcpProcessingFixture{chat: model})
	shelfSuggestModel(f, &library)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/imports/suggest-text", map[string]any{
		"kind": "models", "text": "Model A", "parent_id": shelfNode(nodes, "product"),
		"source":    map[string]any{"type": "import", "import_batch_id": "future-file", "file_date": "2999-01-01T00:00:00Z"},
		"operation": map[string]any{"effective_at": "2026-01-01T00:00:00Z"},
	}, http.StatusBadRequest, nil)
}
