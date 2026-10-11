package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

const chatGPTTestCompleted = `data: {"type":"response.completed","response":{"id":"resp_test","status":"completed","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12,"input_tokens_details":{"cached_tokens":4}}}}

`

func newChatGPTTestServer(t *testing.T, handler http.HandlerFunc) (*ChatGPTChat, *httptest.Server) {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,api.openai.com")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := NewChatGPTChat(&ChatConfig{
		Source: types.ModelSourceChatGPT, ModelName: "account-model", ModelID: "model-id",
		ChatGPTToken: func(context.Context) (string, error) { return "test-oauth-token", nil }, ChatGPTHTTPClient: server.Client(),
	})
	require.NoError(t, err)
	c.endpoint = server.URL + "/v1/responses"
	return c, server
}

func TestChatGPTRequestRestrictionsInstructionsAndImages(t *testing.T) {
	type captured struct {
		Path, Auth, Accept string
		Body               map[string]interface{}
	}
	requests := make(chan captured, 1)
	c, _ := newChatGPTTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		requests <- captured{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Accept"), body}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"{\\\"ok\\\":true}\"}\n\n"+chatGPTTestCompleted)
	})
	previousResolver := LocalImageResolver
	LocalImageResolver = func(storageURL string) ([]byte, bool) {
		return []byte("resolved stored image"), storageURL == "resource://stored-image"
	}
	t.Cleanup(func() { LocalImageResolver = previousResolver })

	response, err := c.Chat(context.Background(), []Message{
		{Role: "system", Content: "Original instructions including JSON requirements."},
		{Role: "system", MultiContent: []MessageContentPart{{Type: "text", Text: "Second original system prompt."}}},
		{Role: "user", Content: "Previous user turn."},
		{Role: "assistant", Content: "Previous model answer."},
		{Role: "user", Content: "Classify this article.", MultiContent: []MessageContentPart{
			{Type: "text", Text: "Caption"},
			{Type: "image_url", ImageURL: &ImageURL{URL: "https://example.test/image.png", Detail: "low"}},
		}, Images: []string{"data:image/png;base64,aW1hZ2U=", "resource://stored-image"}},
	}, &ChatOptions{Temperature: 0.9, TopP: 0.8, Seed: 45, MaxTokens: 100, MaxCompletionTokens: 50, FrequencyPenalty: 0.2, PresencePenalty: 0.3, Format: json.RawMessage(`{"type":"json_object"}`), PromptCacheKey: "must-omit", CacheRetention: CacheRetentionLong})
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, response.Content)
	assert.Equal(t, "stop", response.FinishReason)
	assert.Equal(t, 10, response.Usage.PromptTokens)
	assert.Equal(t, 2, response.Usage.CompletionTokens)
	assert.Equal(t, 12, response.Usage.TotalTokens)
	assert.Equal(t, 4, response.Usage.CacheReadTokens)
	request := <-requests
	assert.Equal(t, "/v1/responses", request.Path)
	assert.Equal(t, "Bearer test-oauth-token", request.Auth)
	assert.Equal(t, "text/event-stream", request.Accept)
	assert.Len(t, request.Body, 5)
	assert.Equal(t, "account-model", request.Body["model"])
	assert.Equal(t, true, request.Body["stream"])
	assert.Equal(t, false, request.Body["store"])
	assert.Equal(t, "Original instructions including JSON requirements.\n\nSecond original system prompt.", request.Body["instructions"])
	for _, key := range []string{"temperature", "max_output_tokens", "metadata", "previous_response_id", "top_p", "format", "response_format", "max_tokens", "prompt_cache_key", "prompt_cache_retention", "user", "background", "conversation"} {
		assert.NotContains(t, request.Body, key)
	}
	inputs := request.Body["input"].([]interface{})
	require.Len(t, inputs, 3)
	assert.Equal(t, "user", inputs[0].(map[string]interface{})["role"])
	assert.Equal(t, "assistant", inputs[1].(map[string]interface{})["role"])
	parts := inputs[2].(map[string]interface{})["content"].([]interface{})
	require.Len(t, parts, 5)
	assert.Equal(t, map[string]interface{}{"type": "input_text", "text": "Classify this article."}, parts[0])
	assert.Equal(t, map[string]interface{}{"type": "input_text", "text": "Caption"}, parts[1])
	assert.Equal(t, map[string]interface{}{"type": "input_image", "image_url": "https://example.test/image.png", "detail": "low"}, parts[2])
	assert.Equal(t, map[string]interface{}{"type": "input_image", "image_url": "data:image/png;base64,aW1hZ2U="}, parts[3])
	assert.True(t, strings.HasPrefix(parts[4].(map[string]interface{})["image_url"].(string), "data:text/plain; charset=utf-8;base64,"))
}

func TestChatGPTTerminalEventsAndInterruptedStreams(t *testing.T) {
	cases := []struct {
		name, data, kind, code, param string
		completed                     bool
	}{
		{"completed", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"final JSON"}]}]}}`, "", "", "", true},
		{"incomplete", `{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`, "incomplete", "max_output_tokens", "", false},
		{"failed quota", `{"type":"response.failed","response":{"status":"failed","error":{"code":"subscription_sharing_usage_limit_exceeded","message":"Usage is limited","param":"model"}}}`, "failed", "subscription_sharing_usage_limit_exceeded", "model", false},
		{"failed unavailable", `{"type":"response.failed","response":{"error":{"code":"subscription_sharing_usage_unavailable","message":"Availability check failed"}}}`, "failed", "subscription_sharing_usage_unavailable", "", false},
		{"explicit error", `{"type":"error","code":"unsupported_input","message":"Unsupported input","param":"input"}`, "error", "unsupported_input", "input", false},
		{"nested explicit error", `{"type":"error","error":{"code":"error_code","message":"Nested error","param":"model"}}`, "error", "error_code", "model", false},
		{"missing completed", "", "interrupted", "", "", false},
		{"done is not completed", "[DONE]", "interrupted", "", "", false},
		{"malformed JSON", "{broken", "invalid_event", "", "", false},
		{"invalid completed", `{"type":"response.completed","response":{"status":"incomplete"}}`, "invalid_event", "", "", false},
		{"missing completed response", `{"type":"response.completed"}`, "invalid_event", "", "", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newChatGPTTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("x-request-id", "req_terminal")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
				if test.data != "" {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", test.data)
				}
			})
			messages := []Message{{Role: "user", Content: "Hello"}}
			response, err := c.Chat(context.Background(), messages, nil)
			if test.completed {
				require.NoError(t, err)
				assert.Equal(t, "final JSON", response.Content)
			} else {
				require.Nil(t, response, "partial output must not become a successful Chat result")
				var apiErr *types.ChatGPTError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, test.kind, apiErr.Kind)
				assert.Equal(t, test.code, apiErr.Code)
				assert.Equal(t, test.param, apiErr.Param)
				assert.Equal(t, 200, apiErr.Status)
				assert.Equal(t, "req_terminal", apiErr.RequestID)
				assert.Equal(t, strings.HasPrefix(test.code, "subscription_sharing_usage_"), types.IsChatGPTQuotaError(err))
			}
			stream, err := c.ChatStream(context.Background(), messages, nil)
			require.NoError(t, err)
			var chunks []types.StreamResponse
			for chunk := range stream {
				chunks = append(chunks, chunk)
			}
			require.Len(t, chunks, 2)
			assert.Equal(t, "partial", chunks[0].Content)
			assert.False(t, chunks[0].Done)
			last := chunks[1]
			assert.True(t, last.Done)
			if test.completed {
				assert.Equal(t, types.ResponseTypeAnswer, last.ResponseType)
				assert.Equal(t, "stop", last.FinishReason)
			} else {
				assert.Equal(t, types.ResponseTypeError, last.ResponseType)
				assert.Equal(t, types.FinishReasonIncomplete, last.FinishReason)
				streamErr, ok := last.Data["error"].(*types.ChatGPTError)
				require.True(t, ok, "stream failures must retain their original typed error")
				assert.Equal(t, test.kind, streamErr.Kind)
				assert.Equal(t, test.code, streamErr.Code)
				assert.Equal(t, "req_terminal", streamErr.RequestID)
			}
		})
	}
}

func TestChatGPTHTTPFailuresPreserveActualStatusBodyAndRequestID(t *testing.T) {
	for _, test := range []struct {
		name, body, code, message string
		status                    int
		quota                     bool
	}{
		{"limit", `{"error":{"code":"subscription_sharing_usage_limit_exceeded","param":"model","message":"Usage limited"}}`, "subscription_sharing_usage_limit_exceeded", "Usage limited", 429, true},
		{"unavailable", `{"error":{"code":"subscription_sharing_usage_unavailable","message":"Usage unavailable"}}`, "subscription_sharing_usage_unavailable", "Usage unavailable", 503, true},
		{"direct admission", `{"detail":"Signed identity was not accepted"}`, "", "Signed identity was not accepted", 401, false},
		{"non JSON infrastructure", `temporary outage`, "", "Service Unavailable", 503, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newChatGPTTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-request-id", "req_http")
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			})
			_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
			var apiErr *types.ChatGPTError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, "http", apiErr.Kind)
			assert.Equal(t, test.status, apiErr.Status)
			assert.Equal(t, "req_http", apiErr.RequestID)
			assert.Equal(t, test.body, apiErr.Body)
			assert.Equal(t, test.message, apiErr.Message)
			assert.Equal(t, test.code, apiErr.Code)
			assert.Equal(t, test.quota, types.IsChatGPTQuotaError(err))
			stream, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
			assert.Nil(t, stream)
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, test.status, apiErr.Status)
		})
	}
}

func TestChatGPTGetsFreshTokenForEveryInference(t *testing.T) {
	requests := make(chan string, 2)
	c, _ := newChatGPTTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Header.Get("Authorization")
		_, _ = io.WriteString(w, chatGPTTestCompleted)
	})
	call := 0
	c.token = func(context.Context) (string, error) {
		call++
		return fmt.Sprintf("renewed-%d", call), nil
	}
	for range 2 {
		_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
		require.NoError(t, err)
	}
	assert.Equal(t, "Bearer renewed-1", <-requests)
	assert.Equal(t, "Bearer renewed-2", <-requests)
	sentinel := errors.New("sign-in required after failed refresh")
	c.token = func(context.Context) (string, error) { return "", sentinel }
	_, err := c.Chat(context.Background(), nil, nil)
	assert.ErrorIs(t, err, sentinel)
	assert.Empty(t, requests, "failed credential refresh must prevent inference")
}

type chatGPTTestRouteTransport struct {
	serverURL *url.URL
	base      http.RoundTripper
}

func (tr chatGPTTestRouteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.String() != chatGPTResponsesEndpoint {
		return nil, fmt.Errorf("unexpected official URL: %s", req.URL)
	}
	copy := req.Clone(req.Context())
	copy.URL.Scheme, copy.URL.Host = tr.serverURL.Scheme, tr.serverURL.Host
	copy.Host = tr.serverURL.Host
	return tr.base.RoundTrip(copy)
}

func TestChatGPTFactoryUsesFixedResponsesRoute(t *testing.T) {
	_, server := newChatGPTTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, chatGPTTestCompleted)
	})
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	config := &ChatConfig{
		Source: types.ModelSourceChatGPT, ModelName: "account-model", ModelID: "local-id",
		BaseURL: "https://chatgpt.com/backend-api", APIKey: "must-not-be-used",
		ChatGPTToken:      func(context.Context) (string, error) { return "owned-oauth-session", nil },
		ChatGPTHTTPClient: &http.Client{Transport: chatGPTTestRouteTransport{parsed, server.Client().Transport}},
	}
	c, err := NewChat(config, nil)
	require.NoError(t, err)
	assert.Equal(t, "local-id", c.GetModelID())
	assert.Equal(t, "account-model", c.GetModelName())
	_, err = c.Chat(context.Background(), []Message{{Role: "user", Content: "Hello"}}, nil)
	require.NoError(t, err)
	_, err = NewChatGPTChat(&ChatConfig{ModelName: "account-model"})
	require.ErrorContains(t, err, "sign-in")
	config.ModelName = ""
	_, err = NewChatGPTChat(config)
	require.ErrorContains(t, err, "model selection")
}

func TestChatGPTSSEMultilineEventAndFinalOutputWithoutDeltas(t *testing.T) {
	c, _ := newChatGPTTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// The event name supplies the type, multiple data lines form one event,
		// and EOF closes the event without a trailing blank line.
		_, _ = io.WriteString(w, `: keepalive
event: response.completed
data: {"response":{"status":"completed",
data: "output":[{"type":"message","content":[{"type":"output_text","text":"complete output"}]}]}}`)
	})
	stream, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	require.NoError(t, err)
	var chunks []types.StreamResponse
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	require.Len(t, chunks, 1)
	assert.True(t, chunks[0].Done)
	assert.Equal(t, "complete output", chunks[0].Content)
	assert.Equal(t, types.ResponseTypeAnswer, chunks[0].ResponseType)
}

func TestChatGPTNetworkInterruptionNeverCompletes(t *testing.T) {
	c, _ := newChatGPTTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "99999")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
	})
	result, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	assert.Nil(t, result)
	var apiErr *types.ChatGPTError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "interrupted", apiErr.Kind)
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestChatGPTUnsupportedToolRequestsAreRejectedBeforeInference(t *testing.T) {
	requests := make(chan bool, 1)
	c, _ := newChatGPTTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests <- true
	})
	for _, test := range []struct {
		messages []Message
		opts     *ChatOptions
	}{
		{[]Message{{Role: "user", Content: "Hi"}}, &ChatOptions{Tools: []Tool{{Type: "function"}}}},
		{[]Message{{Role: "tool", Content: "result"}}, nil},
		{[]Message{{Role: "assistant", ToolCalls: []ToolCall{{ID: "call"}}}}, nil},
	} {
		_, err := c.Chat(context.Background(), test.messages, test.opts)
		var apiErr *types.ChatGPTError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "subscription_sharing_unsupported_capability", apiErr.Code)
	}
	assert.Empty(t, requests)
}

func TestChatGPTInferenceRedirectDoesNotForwardOAuthCredential(t *testing.T) {
	redirected := make(chan string, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected <- r.Header.Get("Authorization")
		_, _ = io.WriteString(w, chatGPTTestCompleted)
	}))
	t.Cleanup(target.Close)
	c, _ := newChatGPTTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/v1/responses", http.StatusTemporaryRedirect)
	})
	_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "Hello"}}, nil)
	var apiErr *types.ChatGPTError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 307, apiErr.Status)
	assert.Empty(t, redirected, "an admission redirect must not receive the OAuth bearer token")
}
