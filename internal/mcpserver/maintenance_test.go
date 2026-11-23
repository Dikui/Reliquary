package mcpserver

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
)

func TestMCPMaintenanceGateAdmitsReadsAndRejectsWriteTools(t *testing.T) {
	ctx := t.Context()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server"}, nil)
	registerTools(server, Services{Entities: &service.EntityService{}})
	server.AddReceivingMiddleware(maintenanceWriteGate(func(_ context.Context) error {
		return errors.New("资料库处于维护暂停")
	}))
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	for _, tool := range listed.Tools {
		if tool.Annotations.ReadOnlyHint {
			continue
		}
		t.Run(tool.Name, func(t *testing.T) {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: map[string]any{}})
			require.NoError(t, err)
			require.True(t, result.IsError)
			require.Equal(t, "资料库处于维护暂停", result.Content[0].(*mcp.TextContent).Text)
		})
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "reliquary_guide", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, result.IsError)
}
