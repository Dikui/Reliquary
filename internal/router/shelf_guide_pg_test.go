package router

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

type guideGapResult struct {
	Gaps struct {
		ModelCount int64 `json:"model_count"`
		Documents  struct {
			Items []struct {
				ModelID string `json:"model_id"`
				types.ShelfChecklistItem
			} `json:"items"`
			Total     int64 `json:"total"`
			Truncated bool  `json:"truncated"`
		} `json:"documents"`
		SaleStatuses struct {
			Items []struct {
				ModelID string `json:"model_id"`
				Region  string `json:"region"`
			} `json:"items"`
			Total     int64 `json:"total"`
			Truncated bool  `json:"truncated"`
		} `json:"sale_statuses"`
		PendingChangesets struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			Total     int64 `json:"total"`
			Truncated bool  `json:"truncated"`
		} `json:"pending_changesets"`
	} `json:"gaps"`
}

// Medium spec, task 31: a guide rejects foreign nodes and ordinary libraries,
// and an empty shelf keeps the normal effective attachment limit.
func TestShelfGuideScopesReadsToEnabledLibrary(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	var other types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Other synthetic shelf", "directory": "other-synthetic-shelf", "entity_mode": "managed", "shelf_enabled": true}, http.StatusCreated, &other)
	client := f.connect(t, "2025-11-25")
	var empty struct {
		MaxFileBytes int64 `json:"max_file_bytes"`
		Values       struct {
			ProductLines []types.Entity `json:"product_lines"`
		} `json:"values"`
	}
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": other.ID}, &empty)
	require.EqualValues(t, 32<<20, empty.MaxFileBytes)
	require.Empty(t, empty.Values.ProductLines)
	for _, input := range []map[string]any{
		{"library_id": other.ID, "node_id": shelfNode(n, "model")},
		{"library_id": l.ID, "node_id": "missing"},
		{"library_id": "missing"},
		{"library_id": f.library.ID},
	} {
		shelfMCPError(t, client, "shelf_guide", input)
	}
}

// Medium regression, task 31: operation labels and opaque issue details are
// not references to shelf nodes, even if their text equals a node's ID.
func TestShelfGuidePendingReferencesUseIdentityFields(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	part := shelfNode(n, "a")
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "agent", AgentName: "Synthetic supplier"}, Operations: []types.ShelfOperation{
		{ID: part, Type: "add_alias", EntityID: shelfNode(n, "b"), Revision: 1, Alias: "Explicit unrelated alias"},
		{ID: "notice", Type: "import_notice", Issues: []types.ShelfConflict{{Type: "cross_table_contradiction", Details: map[string]any{"entity_id": part}}}},
	}}, http.StatusCreated, nil)
	client := f.connect(t, "2025-11-25")
	var result guideGapResult
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID, "node_id": part}, &result)
	require.Zero(t, result.Gaps.PendingChangesets.Total)
}

// Medium regression, task 31: reviewing an older upload does not make it the
// most recently uploaded file when its deferred items become a new changeset.
func TestShelfGuideLatestImportIgnoresDeferredReviewTime(t *testing.T) {
	f, l := importSetup(t)
	old := importUpload(t, f, l.ID, "older_20261101.xlsx", "", importWorkbook(t, importRow(3, "Platform", "Product", "Model", "GA", "9"), ""), http.StatusCreated)
	importUpload(t, f, l.ID, "newer_20261102.xlsx", "", importWorkbook(t, importRow(3, "Platform", "Product", "Model", "DEV", "10"), ""), http.StatusCreated)
	decisions := []types.ShelfDecision{}
	for _, item := range old.Items {
		decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: "defer"})
	}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets/"+old.ID+"/confirm", types.ShelfChangesetReview{Revision: old.Revision, Decisions: decisions}, http.StatusOK, &old)
	require.NotEmpty(t, old.DeferredChangesetID)
	client := f.connect(t, "2025-11-25")
	var guide struct {
		ImportPlans []struct {
			LastFileName string `json:"last_file_name"`
			LastFileDate string `json:"last_file_date"`
		} `json:"import_plans"`
	}
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID}, &guide)
	require.Equal(t, "newer_20261102.xlsx", guide.ImportPlans[0].LastFileName)
	require.Equal(t, "2026-11-02T00:00:00Z", guide.ImportPlans[0].LastFileDate)
}

// Medium spec, task 31: pagination does not lose descendant models or pending
// batches, and a batch involving a part twice still counts as a single batch.
func TestShelfGuideCountsBeyondFirstPage(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	ops := []types.ShelfOperation{}
	for i := 0; i < 101; i++ {
		id := fmt.Sprintf("model%d", i)
		ops = append(ops, types.ShelfOperation{ID: id, Type: "create_entity", Entity: &types.ManagedEntityInput{Name: fmt.Sprintf("Synthetic extra model %03d", i), Kind: "model"}}, types.ShelfOperation{ID: id + "_parent", Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: "$" + id, ObjectID: shelfNode(n, "product"), RelationType: "belongs_to"}})
	}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: ops}, http.StatusCreated, nil)
	for i := 0; i < 101; i++ {
		f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "agent", AgentName: "Synthetic supplier"}, Operations: []types.ShelfOperation{
			{ID: "alias", Type: "add_alias", EntityID: shelfNode(n, "a"), Revision: 1, Alias: fmt.Sprintf("Explicit part alias %03d", i)},
			{ID: "model", Type: "add_alias", EntityID: shelfNode(n, "model"), Revision: 1, Alias: fmt.Sprintf("Explicit model alias %03d", i)},
		}}, http.StatusCreated, nil)
	}
	client := f.connect(t, "2025-11-25")
	var result guideGapResult
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID, "node_id": shelfNode(n, "line")}, &result)
	require.EqualValues(t, 103, result.Gaps.ModelCount)
	require.EqualValues(t, 103, result.Gaps.SaleStatuses.Total)
	require.Len(t, result.Gaps.SaleStatuses.Items, 100)
	require.True(t, result.Gaps.SaleStatuses.Truncated)
	require.EqualValues(t, 101, result.Gaps.PendingChangesets.Total)
	require.Len(t, result.Gaps.PendingChangesets.Items, 100)
	require.True(t, result.Gaps.PendingChangesets.Truncated)
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID, "node_id": shelfNode(n, "a")}, &result)
	require.EqualValues(t, 101, result.Gaps.PendingChangesets.Total)
	require.True(t, result.Gaps.PendingChangesets.Truncated)
}

// Medium spec, task 31: supplement instructions reflect this library's saved
// configuration and imports, and reading them never creates a proposal.
func TestShelfGuideUsesCurrentLibraryValues(t *testing.T) {
	f, l := importSetup(t)
	l.ShelfConfig.Markets = []types.ShelfOption{{Key: "market_x", Name: " Original market "}}
	l.ShelfConfig.Stages = []types.ShelfOption{{Key: "later", Name: "Later"}, {Key: "earlier", Name: "Earlier"}}
	l.ShelfConfig.MaxFileBytes = 123456
	l.ShelfConfig.Standards = []types.ShelfStandard{{Key: "standard_x", Name: " Original standard ", Aliases: []string{"Explicit spelling"}}}
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, &l)
	var requirement types.Entity
	f.request(http.MethodPost, "/libraries/"+l.ID+"/entities", map[string]any{"kind": "document_requirement", "name": "Original requirement", "attributes": map[string]any{"requirement": types.ShelfRequirement{ListName: " Original list ", FinalDueStage: "later"}}}, http.StatusCreated, &requirement)
	batch := importUpload(t, f, l.ID, "synthetic_20261101.xlsx", "", importWorkbook(t, importRow(3, "Platform", "Product", "Model", "GA", "9"), ""), http.StatusCreated)
	client := f.connect(t, "2025-11-25")
	var guide struct {
		LibraryID    string `json:"library_id"`
		Rules        string `json:"rules"`
		MaxFileBytes int64  `json:"max_file_bytes"`
		Values       struct {
			Kinds            []types.EntityKind    `json:"kinds"`
			ProductLines     []types.Entity        `json:"product_lines"`
			Markets          []types.ShelfOption   `json:"markets"`
			Stages           []types.ShelfOption   `json:"stages"`
			Regions          []types.ShelfOption   `json:"regions"`
			ModuleTypes      []types.ShelfOption   `json:"module_types"`
			Standards        []types.ShelfStandard `json:"standards"`
			RequirementLists []string              `json:"requirement_lists"`
			Requirements     []types.Entity        `json:"requirements"`
			SaleStatuses     []string              `json:"sale_statuses"`
		} `json:"values"`
		ImportPlans []struct {
			Key          string                       `json:"key"`
			LastFileName string                       `json:"last_file_name"`
			LastFileDate string                       `json:"last_file_date"`
			LastHeaders  map[string]map[string]string `json:"last_headers"`
			Definition   types.ShelfImportDefinition  `json:"definition"`
		} `json:"import_plans"`
		Gaps any `json:"gaps"`
	}
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID}, &guide)
	require.Equal(t, l.ID, guide.LibraryID)
	require.EqualValues(t, 123456, guide.MaxFileBytes)
	require.Equal(t, l.EntitySchema.Kinds, guide.Values.Kinds)
	require.Len(t, guide.Values.ProductLines, 1)
	require.Equal(t, "Synthetic line", guide.Values.ProductLines[0].CanonicalName)
	require.Equal(t, l.ShelfConfig.Markets, guide.Values.Markets)
	require.Equal(t, l.ShelfConfig.Stages, guide.Values.Stages)
	require.Equal(t, l.ShelfConfig.Regions, guide.Values.Regions)
	require.Equal(t, l.ShelfConfig.ModuleTypes, guide.Values.ModuleTypes)
	require.Equal(t, l.ShelfConfig.Standards, guide.Values.Standards)
	require.Equal(t, []string{" Original list "}, guide.Values.RequirementLists)
	require.Equal(t, requirement.ID, guide.Values.Requirements[0].ID)
	require.Equal(t, []string{"GA", "DEV", "Planning", "EOM", "待定"}, guide.Values.SaleStatuses)
	require.Nil(t, guide.Gaps)
	require.Len(t, guide.ImportPlans, 1)
	require.Equal(t, "map", guide.ImportPlans[0].Key)
	require.Equal(t, "synthetic_20261101.xlsx", guide.ImportPlans[0].LastFileName)
	require.Equal(t, "2026-11-01T00:00:00Z", guide.ImportPlans[0].LastFileDate)
	require.Equal(t, "Header group\nHeader", guide.ImportPlans[0].LastHeaders["Map"]["A"])
	require.Len(t, guide.ImportPlans[0].Definition.Sheets[0].Columns, 5)
	for _, rule := range []string{"shelf_submit", "agent_name", "confirmed=true", "含义不确定", "entity-attachments", "MCP", "二进制", "add_document_version", "owner_id", "requirement_ids", "draft", "final", "version_label", "unassigned", "重复", "冲突", "别名", "所属清单", "电芯", "classify_document", "select_document_version"} {
		require.Contains(t, guide.Rules, rule)
	}
	var drafts types.ShelfChangesetPage
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/changesets?status=pending", nil, http.StatusOK, &drafts)
	require.EqualValues(t, 1, drafts.Total)
	require.Equal(t, batch.ID, drafts.Items[0].ID)
	l.ShelfConfig.Markets[0].Name = "Renamed market"
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, &l)
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID}, &guide)
	require.Equal(t, "Renamed market", guide.Values.Markets[0].Name)
}

// Medium spec, task 31 / ADR 0024: the guide keeps the existing checklist's
// conditional gaps and draft states, and does not infer an empty region status.
func TestShelfGuideModelGapsReuseChecklist(t *testing.T) {
	f, l, n := checklistSetup(t)
	model := shelfNode(n, "model")
	l.ShelfConfig.Regions = append(l.ShelfConfig.Regions, types.ShelfOption{Key: "region_b", Name: "Region B"})
	f.request(http.MethodPut, "/libraries/"+l.ID, map[string]any{"shelf_config": l.ShelfConfig}, http.StatusOK, &l)
	f.request(http.MethodPatch, "/entities/"+shelfNode(n, "product"), map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "draft_due"}}, http.StatusOK, nil)
	missing := checklistRequirement(t, f, l, "Missing on alternative", "draft_due", "final_due")
	draft := checklistRequirement(t, f, l, "Draft supplied", "", "final_due")
	later := checklistRequirement(t, f, l, "Not due yet", "", "final_due")
	for _, r := range []types.Entity{missing, draft, later} {
		checklistBind(f, l, r, shelfNode(n, "line"))
	}
	checklistFile(f, l, shelfNode(n, "a"), missing.ID, "final")
	checklistFile(f, l, shelfNode(n, "platform"), draft.ID, "draft")
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"type": "set_sale_statuses", "entity_id": model, "revision": 1, "sale_statuses": []types.ShelfSaleStatus{{Region: "region_a", Status: "GA", Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(n, "a")}}}}}}}}, http.StatusCreated, nil)
	var pending types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "agent", "agent_name": "Synthetic supplier"}, "operations": []any{map[string]any{"type": "add_document_version", "document": map[string]any{"name": "Pending file", "owner_id": model, "requirement_ids": []string{missing.ID}, "cloud_url": "https://example.invalid/pending", "stage": "final"}}}}, http.StatusCreated, &pending)
	client := f.connect(t, "2025-11-25")
	var result guideGapResult
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID, "node_id": model}, &result)
	require.EqualValues(t, 1, result.Gaps.ModelCount)
	require.EqualValues(t, 2, result.Gaps.Documents.Total)
	require.False(t, result.Gaps.Documents.Truncated)
	states := map[string]string{}
	for _, item := range result.Gaps.Documents.Items {
		require.Equal(t, model, item.ModelID)
		states[item.Requirement.ID] = item.Status
		var record types.ShelfRequirement
		require.NoError(t, types.DecodeShelfValue(item.Requirement.Attributes["requirement"], &record))
		require.Equal(t, "final_due", record.FinalDueStage)
		if item.Requirement.ID == missing.ID {
			require.Equal(t, []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(n, "b")}}}, item.Conditions)
		}
	}
	require.Equal(t, map[string]string{missing.ID: "missing", draft.ID: "draft_only"}, states)
	require.EqualValues(t, 1, result.Gaps.SaleStatuses.Total)
	require.Equal(t, "region_b", result.Gaps.SaleStatuses.Items[0].Region)
	require.Equal(t, model, result.Gaps.SaleStatuses.Items[0].ModelID)
	require.EqualValues(t, 1, result.Gaps.PendingChangesets.Total)
	require.Equal(t, pending.ID, result.Gaps.PendingChangesets.Items[0].ID)
	for _, kind := range []string{"line", "platform", "product"} {
		mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID, "node_id": shelfNode(n, kind)}, &result)
		require.EqualValues(t, 2, result.Gaps.ModelCount, kind)
		require.EqualValues(t, 4, result.Gaps.Documents.Total, kind)
		require.EqualValues(t, 3, result.Gaps.SaleStatuses.Total, kind)
		require.EqualValues(t, 1, result.Gaps.PendingChangesets.Total, kind)
	}
}

// Medium spec, task 31: documented file operations can be submitted over MCP,
// including the explicit unassigned, reclassification and current-version flow.
func TestShelfGuideDocumentWorkflowCanBeSubmitted(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	client := f.connect(t, "2025-11-25")
	attachment := shelfUpload(t, f, l.ID, "%PDF synthetic guide original", http.StatusCreated)
	var draft types.ShelfChangeset
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": l.ID, "source": map[string]any{"type": "agent", "agent_name": "Synthetic supplier"}, "operations": []any{map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": "Unassigned guide file", "unassigned": true, "attachment_id": attachment.ID, "stage": "draft"}}}}, &draft)
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": l.ID, "changeset_id": draft.ID, "revision": draft.Revision, "confirmed": true, "decisions": []types.ShelfDecision{{ItemID: "file", Action: "apply"}}}, &draft)
	file := draft.Items[0].EntityID
	var versions []types.ShelfDocumentVersion
	f.request(http.MethodGet, "/entities/"+file+"/document-versions", nil, http.StatusOK, &versions)
	versionID := versions[0].ID
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": l.ID, "source": map[string]any{"type": "agent", "agent_name": "Synthetic supplier"}, "operations": []any{map[string]any{"id": "classify", "type": "classify_document", "entity_id": file, "revision": 1, "classification": map[string]any{"owner_id": shelfNode(n, "model"), "requirement_ids": []string{}, "conditions": []any{}}}}}, &draft)
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": l.ID, "changeset_id": draft.ID, "revision": draft.Revision, "confirmed": true, "decisions": []types.ShelfDecision{{ItemID: "classify", Action: "apply"}}}, &draft)
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": l.ID, "source": map[string]any{"type": "agent", "agent_name": "Synthetic supplier"}, "operations": []any{map[string]any{"id": "select", "type": "select_document_version", "entity_id": file, "revision": 2, "version_id": versionID}}}, &draft)
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": l.ID, "changeset_id": draft.ID, "revision": draft.Revision, "confirmed": true, "decisions": []types.ShelfDecision{{ItemID: "select", Action: "apply"}}}, &draft)
	f.request(http.MethodGet, "/entities/"+file+"/document-versions", nil, http.StatusOK, &versions)
	require.Equal(t, versionID, versions[0].ID)
	require.Equal(t, shelfNode(n, "model"), *versions[0].OwnerID)
	require.True(t, versions[0].IsCurrent)
}

// Medium spec, task 31: an upper node counts every descendant gap before
// bounding the response; a part has only its relevant pending proposals.
func TestShelfGuideTruncatesAfterCountingAllGaps(t *testing.T) {
	f, l, n := checklistSetup(t)
	f.request(http.MethodPatch, "/entities/"+shelfNode(n, "product"), map[string]any{"revision": 1, "attributes": map[string]any{"current_stage": "final_due"}}, http.StatusOK, nil)
	ops := []types.ShelfOperation{}
	for i := 0; i < 51; i++ {
		id := fmt.Sprintf("req%d", i)
		ops = append(ops, types.ShelfOperation{ID: id, Type: "create_entity", Entity: &types.ManagedEntityInput{Name: fmt.Sprintf("Synthetic requirement %02d", i), Kind: "document_requirement", Attributes: map[string]any{"requirement": types.ShelfRequirement{ListName: "Synthetic list", FinalDueStage: "final_due"}}}}, types.ShelfOperation{ID: id + "_line", Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: "$" + id, ObjectID: shelfNode(n, "line"), RelationType: "applies_to"}})
	}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: ops}, http.StatusCreated, nil)
	var pending types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "agent", AgentName: "Synthetic supplier"}, Operations: []types.ShelfOperation{{Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: shelfNode(n, "single"), ObjectID: shelfNode(n, "a"), RelationType: "composed_of", Attributes: map[string]any{"module_type": "module_a"}}}}}, http.StatusCreated, &pending)
	client := f.connect(t, "2025-11-25")
	var result guideGapResult
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID, "node_id": shelfNode(n, "line")}, &result)
	require.EqualValues(t, 2, result.Gaps.ModelCount)
	require.EqualValues(t, 102, result.Gaps.Documents.Total)
	require.Len(t, result.Gaps.Documents.Items, 100)
	require.True(t, result.Gaps.Documents.Truncated)
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID, "node_id": shelfNode(n, "a")}, &result)
	require.Zero(t, result.Gaps.ModelCount)
	require.Empty(t, result.Gaps.Documents.Items)
	require.Empty(t, result.Gaps.SaleStatuses.Items)
	require.EqualValues(t, 1, result.Gaps.PendingChangesets.Total)
	require.Equal(t, pending.ID, result.Gaps.PendingChangesets.Items[0].ID)
	mcpCall(t, client, "shelf_guide", map[string]any{"library_id": l.ID, "node_id": shelfNode(n, "b")}, &result)
	require.Zero(t, result.Gaps.PendingChangesets.Total)
}
