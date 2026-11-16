package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Customer installation: the real account manager and policy use an isolated
// database. Only the upstream model server is replaced by synthetic SSE.
func TestAgentModelRetainsToolsAndPausesOnQuota(t *testing.T) {
	h := newChatGPTGenerationHarness(t, func(chatGPTGenerationRequest) (string, string) { return "", "" })
	failed := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer fixture-owned-oauth-token", r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "fixture-account-model", body["model"])
		require.Equal(t, false, body["store"])
		require.Equal(t, true, body["stream"])
		require.NotNil(t, body["tools"])
		w.Header().Set("Content-Type", "text/event-stream")
		if failed {
			fmt.Fprint(w, "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"subscription_sharing_usage_limit_exceeded\"}}}\n\n")
			return
		}
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"name\":\"list_libraries\",\"arguments\":\"{}\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
	}))
	defer upstream.Close()
	endpoint, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	s := NewAgentModelService(h.models, h.models.chatGPT, h.policy)
	s.client = &http.Client{Transport: chatGPTGenerationTransport{endpoint, upstream.Client().Transport}}
	body := []byte(`{"model":"ignored","input":[{"role":"user","content":"读取资料库"}],"tools":[{"type":"namespace","name":"reliquary","tools":[{"type":"function","name":"list_libraries","parameters":{"type":"object"}}]}]}`)
	var output bytes.Buffer
	require.NoError(t, s.Respond(h.ctx, "chatgpt-generation", body, func(b []byte) error { _, err := output.Write(b); return err }))
	require.Contains(t, output.String(), "function_call")
	require.NotContains(t, output.String(), "fixture-owned-oauth-token")
	failed = true
	err = s.Respond(h.ctx, "chatgpt-generation", body, func([]byte) error { return nil })
	require.True(t, types.IsChatGPTQuotaError(err))
	policy, err := h.policy.Get(h.ctx)
	require.NoError(t, err)
	require.True(t, policy.Paused)
	require.ErrorIs(t, relayAgentResponse(strings.NewReader("data: {\"type\":\"response.created\"}\n\n"), func([]byte) error { return nil }), io.ErrUnexpectedEOF)
}

// Customer installation: a request the agent endpoint rejects never spends the
// subscription's daily allowance.
func TestAgentModelRejectsInvalidRequestBeforeReservingQuota(t *testing.T) {
	h := newChatGPTGenerationHarness(t, func(chatGPTGenerationRequest) (string, string) { return "", "" })
	before, err := h.policy.Get(h.ctx)
	require.NoError(t, err)
	s := NewAgentModelService(h.models, h.models.chatGPT, h.policy)
	body := []byte(`{"input":[{"role":"user","content":"test"}],"tools":[{"type":"web_search"}]}`)
	require.ErrorContains(t, s.Respond(h.ctx, "chatgpt-generation", body, func([]byte) error { return nil }), "local function")
	after, err := h.policy.Get(h.ctx)
	require.NoError(t, err)
	require.Equal(t, before.DailyUsed, after.DailyUsed)
}
