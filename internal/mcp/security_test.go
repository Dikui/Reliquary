package mcp

import (
	"testing"

	"reliquary/internal/types"
)

func TestNewMCPClientRejectsStoredUnsafeURL(t *testing.T) {
	serviceURL := "http://127.0.0.1:8080/mcp"
	_, err := NewMCPClient(&ClientConfig{Service: &types.MCPService{
		URL:           &serviceURL,
		TransportType: types.MCPTransportHTTPStreamable,
	}})
	if err == nil {
		t.Fatal("expected stored unsafe MCP URL to be rejected at client construction")
	}
}
