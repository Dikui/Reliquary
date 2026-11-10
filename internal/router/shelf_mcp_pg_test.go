package router

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/skills"
)

// Medium spec, task 21 / ADR 0023: MCP find preserves candidate identities,
// category labels and the full count when its response is paginated.
func TestShelfMCPFindReturnsCandidatesAndTotal(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	client := f.connect(t, "2025-11-25")
	var result types.ShelfFindResult
	mcpCall(t, client, "shelf_find", map[string]any{"library_id": library.ID, "q": "Synthetic", "limit": 1}, &result)
	require.EqualValues(t, 8, result.Total)
	require.True(t, result.Truncated)
	require.Len(t, result.Items, 1)
	require.Equal(t, shelfNode(nodes, "a"), result.Items[0].ID)
	require.Equal(t, "part", result.Items[0].Kind)
	require.NotEmpty(t, result.Items[0].KindLabel)
	var expected types.ShelfFindResult
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/find?q=Synthetic&limit=1", nil, http.StatusOK, &expected)
	require.Equal(t, expected, result)
}

// Medium spec, task 21: tool discovery and the embedded guide deliver the
// shelf expert's answer rules before an external agent starts its work.
func TestShelfMCPExpertInstructionsAreDiscoverable(t *testing.T) {
	f := newMCPFixture(t)
	client := f.connect(t, "2025-11-25")
	listed, err := client.ListTools(context.Background(), nil)
	require.NoError(t, err)
	for _, tool := range listed.Tools {
		if !strings.HasPrefix(tool.Name, "shelf_") {
			continue
		}
		for _, rule := range []string{"来源", "时点", "适用条件", "来自部件", "来自上层", "GA", "可销售", "认证齐全", "总数", "截断", "同名且影响答案时先澄清", "货架中没有记录", "shelf-expert"} {
			require.Contains(t, tool.Description, rule, tool.Name)
		}
	}
	guide, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "reliquary_guide", Arguments: map[string]any{"workflow": "shelf-expert"}})
	require.NoError(t, err)
	require.False(t, guide.IsError)
	content := guide.Content[0].(*mcp.TextContent).Text
	for _, rule := range []string{"shelf_find", "shelf_get", "shelf_query", "shelf_guide", "shelf_submit", "来源", "时点", "适用条件", "来自部件", "来自上层", "GA", "可销售", "认证齐全", "总数", "截断", "同名", "货架中没有记录", "confirmed=true", "含义不确定"} {
		require.Contains(t, content, rule)
	}
	rules, err := skills.Files.ReadFile("reliquary/rules.md")
	require.NoError(t, err)
	require.Contains(t, string(rules), "货架专家")
	require.Contains(t, string(rules), "shelf_submit")
}

// Medium spec, task 21 / ADR 0023: a submit without confirmation persists its
// diff for the web reviewer, and leaves the proposed entity absent.
func TestShelfMCPSubmitWithoutConfirmationPersistsOnlyProposal(t *testing.T) {
	f, library := newProductShelfFixture(t)
	client := f.connect(t, "2025-11-25")
	for _, confirmed := range []any{nil, false} {
		input := map[string]any{"library_id": library.ID, "source": map[string]any{"type": "agent", "agent_name": "Synthetic assistant"}, "operations": []any{
			map[string]any{"id": "line", "type": "create_entity", "entity": map[string]any{"name": " Synthetic MCP line ", "kind": "product_line"}},
		}}
		if confirmed != nil {
			input["confirmed"] = confirmed
		}
		var draft types.ShelfChangeset
		mcpCall(t, client, "shelf_submit", input, &draft)
		require.Equal(t, "pending", draft.Status)
		require.NotEmpty(t, draft.ID)
		require.Len(t, draft.Items, 1)
		require.Equal(t, "new", draft.Items[0].Conflicts[0].Type)
		require.JSONEq(t, "null", string(draft.Items[0].Before))
		require.Contains(t, string(draft.Items[0].After), " Synthetic MCP line ")
		var saved types.ShelfChangeset
		f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID, nil, http.StatusOK, &saved)
		require.Equal(t, draft.ID, saved.ID)
		require.Equal(t, draft.Status, saved.Status)
		require.Equal(t, draft.Revision, saved.Revision)
		require.Equal(t, draft.Source, saved.Source)
		expectedItems, err := json.Marshal(draft.Items)
		require.NoError(t, err)
		savedItems, err := json.Marshal(saved.Items)
		require.NoError(t, err)
		require.JSONEq(t, string(expectedItems), string(savedItems))
		var found types.ShelfFindResult
		mcpCall(t, client, "shelf_find", map[string]any{"library_id": library.ID, "q": "Synthetic MCP line"}, &found)
		require.Zero(t, found.Total)
	}
}

// Medium spec, task 25: the MCP operation contract accepts the same nonblocking
// column feedback as HTTP and requires an explicit acknowledgement decision.
func TestShelfMCPSubmitAcknowledgesUnmappedColumnNotice(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	client := f.connect(t, "2025-11-25")
	var before, after types.EntityDetail
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model"), nil, http.StatusOK, &before)
	var result types.ShelfChangeset
	mcpCall(t, client, "shelf_submit", map[string]any{
		"library_id": library.ID, "source": map[string]any{"type": "agent", "agent_name": "Synthetic importer"}, "confirmed": true,
		"operations": []any{map[string]any{"id": "column", "type": "import_notice", "issues": []any{map[string]any{"type": "unmapped_column", "details": map[string]any{"worksheet": "Map", "column": "F", "header": "Original notes", "nonempty_rows": 1, "examples": []string{"Original content"}}}}}},
		"decisions":  []any{map[string]any{"item_id": "column", "action": "apply"}},
	}, &result)
	require.Equal(t, "confirmed", result.Status)
	require.Equal(t, "unmapped_column", result.Items[0].Conflicts[0].Type)
	f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model"), nil, http.StatusOK, &after)
	require.Equal(t, before, after)
}

func shelfMCPError(t *testing.T, client *mcp.ClientSession, name string, input any) *mcp.CallToolResult {
	t.Helper()
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: input})
	require.NoError(t, err)
	require.True(t, result.IsError, "expected %s to fail", name)
	return result
}

// Medium spec, task 21: confirmed submit applies a previously displayed draft
// once, retaining its identity, source and revision protection.
func TestShelfMCPSubmitConfirmsReviewedProposal(t *testing.T) {
	f, library := newProductShelfFixture(t)
	client := f.connect(t, "2025-11-25")
	var draft types.ShelfChangeset
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": library.ID, "source": map[string]any{"type": "agent", "agent_name": "Synthetic assistant"}, "operations": []any{
		map[string]any{"id": "line", "type": "create_entity", "entity": map[string]any{"name": " Reviewed MCP line ", "kind": "product_line"}},
	}}, &draft)
	id := draft.Items[0].EntityID
	input := map[string]any{"library_id": library.ID, "changeset_id": draft.ID, "revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "line", "action": "apply"}}}
	var reviewed types.ShelfChangeset
	mcpCall(t, client, "shelf_submit", input, &reviewed)
	require.Equal(t, "pending", reviewed.Status)
	require.Greater(t, reviewed.Revision, draft.Revision)
	var found types.ShelfFindResult
	mcpCall(t, client, "shelf_find", map[string]any{"library_id": library.ID, "q": "Reviewed MCP line"}, &found)
	require.Zero(t, found.Total)
	input["confirmed"] = true
	shelfMCPError(t, client, "shelf_submit", input)
	input["revision"] = reviewed.Revision
	mcpCall(t, client, "shelf_submit", input, &reviewed)
	require.Equal(t, draft.ID, reviewed.ID)
	require.Equal(t, "confirmed", reviewed.Status)
	var detail types.ShelfNodeDetail
	mcpCall(t, client, "shelf_get", map[string]any{"library_id": library.ID, "node_id": id}, &detail)
	require.Equal(t, " Reviewed MCP line ", detail.Entity.CanonicalName)
	var history types.OperationLogPage
	f.request(http.MethodGet, "/entities/"+id+"/history", nil, http.StatusOK, &history)
	require.Len(t, history.Items, 1)
	require.Equal(t, draft.ID, history.Items[0].ChangesetID)
	require.Equal(t, "agent", history.Items[0].Source.Type)
	require.Equal(t, "Synthetic assistant", history.Items[0].Source.AgentName)
	require.Contains(t, history.Items[0].Actor, "api_key:")
	input["revision"] = reviewed.Revision
	shelfMCPError(t, client, "shelf_submit", input)
}

// Medium spec, task 21: explicit confirmation may create and atomically apply
// a fully specified changeset without a separate saved review.
func TestShelfMCPSubmitExplicitConfirmationAppliesChanges(t *testing.T) {
	f, library := newProductShelfFixture(t)
	client := f.connect(t, "2025-11-25")
	var saved types.ShelfChangeset
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": library.ID, "source": map[string]any{"type": "agent", "agent_name": "Synthetic assistant"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "line", "type": "create_entity", "entity": map[string]any{"name": "Confirmed MCP line", "kind": "product_line"}},
	}}, &saved)
	require.Equal(t, "confirmed", saved.Status)
	var detail types.ShelfNodeDetail
	mcpCall(t, client, "shelf_get", map[string]any{"library_id": library.ID, "node_id": saved.Items[0].EntityID}, &detail)
	require.Equal(t, "Confirmed MCP line", detail.Entity.CanonicalName)
}

// Medium spec, task 21 / ADR 0023: confirmation cannot infer an uncertain
// meaning, exposes the conflict, and rolls back the whole proposed batch.
func TestShelfMCPSubmitUncertainMeaningRequiresDecision(t *testing.T) {
	f, library := newProductShelfFixture(t)
	client := f.connect(t, "2025-11-25")
	var draft types.ShelfChangeset
	mcpCall(t, client, "shelf_submit", map[string]any{"library_id": library.ID, "source": map[string]any{"type": "agent", "agent_name": "Synthetic assistant"}, "operations": []any{
		map[string]any{"id": "good", "type": "create_entity", "entity": map[string]any{"name": "Atomic MCP line", "kind": "product_line"}},
		map[string]any{"id": "uncertain", "type": "create_entity", "entity": map[string]any{"name": "Uncertain MCP line", "kind": "product_line"}, "issues": []any{map[string]any{"type": "uncertain", "message": "Submitter must choose the meaning"}}},
	}}, &draft)
	input := map[string]any{"library_id": library.ID, "changeset_id": draft.ID, "revision": draft.Revision, "confirmed": true, "decisions": []any{
		map[string]any{"item_id": "good", "action": "apply"}, map[string]any{"item_id": "uncertain", "action": "apply"},
	}}
	result := shelfMCPError(t, client, "shelf_submit", input)
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var failure struct {
		Error struct {
			Code    int
			Details types.ShelfChangeset
		}
	}
	require.NoError(t, json.Unmarshal(data, &failure))
	require.Equal(t, 1005, failure.Error.Code)
	require.Equal(t, draft.ID, failure.Error.Details.ID)
	require.Contains(t, string(data), "uncertain")
	var found types.ShelfFindResult
	mcpCall(t, client, "shelf_find", map[string]any{"library_id": library.ID, "q": "MCP line"}, &found)
	require.Zero(t, found.Total)
	input["decisions"] = []any{map[string]any{"item_id": "good", "action": "apply"}, map[string]any{"item_id": "uncertain", "action": "discard"}}
	var confirmed types.ShelfChangeset
	mcpCall(t, client, "shelf_submit", input, &confirmed)
	require.Equal(t, "confirmed", confirmed.Status)
	mcpCall(t, client, "shelf_find", map[string]any{"library_id": library.ID, "q": "MCP line"}, &found)
	require.EqualValues(t, 1, found.Total)
	require.Equal(t, "Atomic MCP line", found.Items[0].CanonicalName)
}

// Medium spec, task 21: misspelled payload fields must not disappear during
// decoding and turn a different operation into a confirmed write.
func TestShelfMCPSubmitRejectsUnknownPayloadFields(t *testing.T) {
	f, library := newProductShelfFixture(t)
	client := f.connect(t, "2025-11-25")
	shelfMCPError(t, client, "shelf_submit", map[string]any{"library_id": library.ID, "source": map[string]any{"type": "agent", "agent_name": "Synthetic assistant"}, "confirmed": true, "operations": []any{
		map[string]any{"type": "configure_shelf", "library_updated_at": library.UpdatedAt, "library": map[string]any{"description": "Unexpected edit", "unknown_setting": true}},
	}})
	var result types.Library
	f.request(http.MethodGet, "/libraries/"+library.ID, nil, http.StatusOK, &result)
	require.Equal(t, library.Description, result.Description)
	var drafts types.ShelfChangesetPage
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/changesets", nil, http.StatusOK, &drafts)
	require.Zero(t, drafts.Total)
}

// Medium spec, task 21 / ADR 0023: get passes the selected combination and
// business time to the shared reader and retains fact evidence.
func TestShelfMCPGetReturnsSelectedFactsAndEvidence(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	statuses := []types.ShelfSaleStatus{
		{Region: "region_a", Status: "GA", Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "a")}}}},
		{Region: "region_a", Status: "DEV", Conditions: []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "b")}}}},
	}
	var saved types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "agent", AgentName: "Synthetic specialist"}, Confirmed: true, Operations: []types.ShelfOperation{
		{ID: "status", Type: "set_sale_statuses", EntityID: shelfNode(nodes, "model"), Revision: 1, SaleStatuses: &statuses},
	}}, http.StatusCreated, &saved)
	client := f.connect(t, "2025-11-25")
	var detail types.ShelfNodeDetail
	mcpCall(t, client, "shelf_get", map[string]any{"library_id": library.ID, "node_id": shelfNode(nodes, "model"), "combination": map[string]string{"module_a": shelfNode(nodes, "a")}}, &detail)
	require.True(t, detail.Recorded)
	require.Equal(t, shelfNode(nodes, "model"), detail.Entity.ID)
	require.Len(t, detail.Shelf.SaleStatuses, 1)
	require.Equal(t, "GA", detail.Shelf.SaleStatuses[0].Status)
	require.False(t, detail.AsOf.IsZero())
	found := false
	for _, fact := range detail.Facts {
		if fact.Evidence.ChangesetID == saved.ID {
			found = true
			require.Equal(t, "agent", fact.Evidence.Source.Type)
			require.Equal(t, "Synthetic specialist", fact.Evidence.Source.AgentName)
			require.NotEmpty(t, fact.Evidence.OperationLogID)
			require.False(t, fact.Evidence.EffectiveAt.IsZero())
			require.False(t, fact.Evidence.RecordedAt.IsZero())
		}
	}
	require.True(t, found, "selected facts must retain their submitting source")
	mcpCall(t, client, "shelf_get", map[string]any{"library_id": library.ID, "node_id": detail.Entity.ID, "as_of": "2000-01-01T00:00:00Z"}, &detail)
	require.False(t, detail.Recorded)
	require.Empty(t, detail.Facts)
	require.Equal(t, 2000, detail.AsOf.Year())
}

// Medium spec, task 21: query returns the full count and matching conditions
// while retaining the requested business time and partial option marker.
func TestShelfMCPQueryReturnsCountAndMatchingConditions(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	client := f.connect(t, "2025-11-25")
	var result types.ShelfQueryResult
	mcpCall(t, client, "shelf_query", map[string]any{"library_id": library.ID, "ancestor_id": shelfNode(nodes, "line"), "limit": 1}, &result)
	require.EqualValues(t, 2, result.Total)
	require.True(t, result.Truncated)
	require.Len(t, result.Items, 1)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	mcpCall(t, client, "shelf_query", map[string]any{"library_id": library.ID, "part_id": shelfNode(nodes, "a"), "as_of": at}, &result)
	require.EqualValues(t, 1, result.Total)
	require.False(t, result.Truncated)
	require.Equal(t, shelfNode(nodes, "model"), result.Items[0].Entity.ID)
	require.True(t, result.Items[0].PartialOptions)
	require.NotEmpty(t, result.Items[0].MatchingConditions)
	require.Equal(t, at, result.AsOf.Format(time.RFC3339Nano))
}

// Status questions over MCP: any region (region omitted), planned retirement and
// missing records use the same filters as the status table; a display name in
// place of a region key gets an error naming the configured keys.
func TestShelfMCPQueryStatusFilters(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	statuses := []types.ShelfSaleStatus{{Region: "region_a", Status: "GA", PlannedEOM: true}}
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", types.ShelfChangesetInput{Source: types.OperationSource{Type: "web"}, Confirmed: true, Operations: []types.ShelfOperation{{ID: "status", Type: "set_sale_statuses", EntityID: shelfNode(nodes, "model"), Revision: 1, SaleStatuses: &statuses}}}, http.StatusCreated, nil)
	client := f.connect(t, "2025-11-25")
	for name, filter := range map[string]map[string]any{
		"any region":         {"status": "GA"},
		"planned retirement": {"planned_eom": true},
		"no record anywhere": {"no_record": true},
	} {
		var result types.ShelfQueryResult
		mcpCall(t, client, "shelf_query", map[string]any{"library_id": library.ID, "sale_statuses": []any{filter}}, &result)
		require.EqualValues(t, 1, result.Total, name)
		want := shelfNode(nodes, "model")
		if filter["no_record"] == true {
			want = shelfNode(nodes, "single")
		}
		require.Equal(t, want, result.Items[0].Entity.ID, name)
	}
	response, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "shelf_query", Arguments: map[string]any{"library_id": library.ID, "sale_statuses": []any{map[string]any{"region": "Region A", "status": "GA"}}}})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content[0].(*mcp.TextContent).Text, "region_a")
}
