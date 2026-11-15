package chat

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Customer installation: invalid tool types never reach the synthetic provider.
func TestAgentResponseRejectsHostedTools(t *testing.T) {
	called := false
	client, _ := newChatGPTTestServer(t, func(http.ResponseWriter, *http.Request) { called = true })
	_, err := client.OpenAgentResponse(context.Background(), []byte(`{"input":[{"role":"user","content":"test"}],"tools":[{"type":"web_search"}]}`))
	require.ErrorContains(t, err, "local function")
	require.False(t, called)
	_, err = client.OpenAgentResponse(context.Background(), []byte(`{"input":[{"type":"tool_search_call"}]}`))
	require.Error(t, err)
	require.False(t, called)
}
