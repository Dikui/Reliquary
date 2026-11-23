package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestShelfQueryNormalizesOptionalStrings(testRun *testing.T) {
	ctx, cancel := context.WithTimeout(testRun.Context(), 5*time.Second)
	defer cancel()
	implementation := &mcp.Implementation{Name: "shelf test", Version: "1"}
	server := mcp.NewServer(implementation, nil)
	requests := make(chan types.ShelfQueryRequest, 1)
	registerShelfQueryTool(server, func(_ context.Context, _ string, request types.ShelfQueryRequest) (*types.ShelfQueryResult, error) {
		requests <- request
		return &types.ShelfQueryResult{}, nil
	})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(testRun, err)
	defer serverSession.Close()
	client, err := mcp.NewClient(implementation, nil).Connect(ctx, clientTransport, nil)
	require.NoError(testRun, err)
	defer client.Close()

	call := func(testRun *testing.T, input string) *mcp.CallToolResult {
		testRun.Helper()
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "shelf_query", Arguments: json.RawMessage(input)})
		require.NoError(testRun, err)
		return result
	}
	query := func(testRun *testing.T, input string) types.ShelfQueryRequest {
		testRun.Helper()
		result := call(testRun, input)
		require.False(testRun, result.IsError, "%+v", result.Content)
		require.NotNil(testRun, result.StructuredContent)
		require.Len(testRun, requests, 1)
		return <-requests
	}
	testRun.Run("client fills every optional field", func(testRun *testing.T) {
		expected := query(testRun, `{"library_id":"library","detail":"summary","limit":1,"certification":{"status":"delayed"}}`)
		require.Equal(testRun, types.ShelfQueryRequest{Detail: "summary", Limit: 1, Certification: &types.ShelfCertificationFilter{Status: "delayed"}}, expected)
		require.Equal(testRun, expected, query(testRun, `{"ancestor_id":"","as_of":"","certification":{"progress":"","region":"","standard":"","status":"delayed"},"composition":[],"detail":"summary","document_gaps":false,"facets":false,"library_id":"library","limit":1,"offset":0,"parameters":[],"part_id":"","sale_statuses":[]}`))
	})
	testRun.Run("empty filter entries are omitted", func(testRun *testing.T) {
		require.Equal(testRun, types.ShelfQueryRequest{}, query(testRun, `{"library_id":"library","as_of":" \t ","detail":"\n","ancestor_id":"  ","part_id":" ","certification":{"standard":" ","status":" ","region":" ","progress":" ","no_region":false},"sale_statuses":[{},{"region":" ","status":"","no_record":false}],"parameters":[{},{"field":" ","missing":false}],"composition":[{},{"module_type":" ","part_ids":[" "]}]}`))
	})
	testRun.Run("nonempty filters and explicit false or zero are kept", func(testRun *testing.T) {
		request := query(testRun, `{"library_id":"library","as_of":" 2026-11-07T00:00:00.123456789Z ","detail":" full ","ancestor_id":" line ","part_id":" part ","certification":{"standard":" standard ","status":" delayed ","region":" region_a ","progress":" in progress ","no_region":false},"sale_statuses":[{"region":" ","status":" GA "},{"planned_eom":false}],"parameters":[{"field":" capacity ","min":0},{"field":" ","max":0}],"composition":[{"module_type":" module_a ","part_ids":[" part "]},{"module_type":" module_b ","part_ids":[]}]}`)
		at := time.Date(2026, time.November, 7, 0, 0, 0, 123456789, time.UTC)
		require.Equal(testRun, types.ShelfQueryRequest{
			AsOf: &at, Detail: "full", AncestorID: "line", PartID: "part",
			Certification: &types.ShelfCertificationFilter{Standard: "standard", Status: "delayed", Progress: new("in progress"), Region: new("region_a")},
			SaleStatuses:  []types.ShelfStatusFilter{{Status: "GA"}, {PlannedEOM: new(false)}},
			Parameters:    []types.ShelfParameterFilter{{Field: "capacity", Min: new(float64(0))}, {Max: new(float64(0))}},
			Composition:   []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{"part"}}, {ModuleType: "module_b", PartIDs: []string{}}},
		}, request)
	})
	testRun.Run("no_region selects an empty region", func(testRun *testing.T) {
		request := query(testRun, `{"library_id":"library","certification":{"no_region":true,"region":"  "}}`)
		require.Equal(testRun, &types.ShelfCertificationFilter{Region: new("")}, request.Certification)
	})
	for name, test := range map[string]struct{ input, message string }{
		"no_region conflicts with a region key": {`{"library_id":"library","certification":{"no_region":true,"region":" region_a "}}`, "certification.no_region cannot be combined"},
		"invalid nonempty as_of":                {`{"library_id":"library","as_of":"2026-11-07"}`, "invalid as_of"},
	} {
		testRun.Run(name, func(testRun *testing.T) {
			result := call(testRun, test.input)
			require.True(testRun, result.IsError)
			require.Contains(testRun, result.Content[0].(*mcp.TextContent).Text, test.message)
			require.Empty(testRun, requests)
		})
	}
}
