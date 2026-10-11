package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ollamaapi "github.com/ollama/ollama/api"
	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/utils/ollama"
)

func TestChatGPTModelServiceUsesRealOllamaHTTPFallbackAndPreservesQuotaPause(t *testing.T) {
	for _, reason := range []string{"subscription_sharing_usage_limit_exceeded", "subscription_sharing_usage_unavailable", "daily_limit"} {
		t.Run(reason, func(t *testing.T) {
			const answer = `{"approved":true,"reason":"Original publication meets the existing review rules"}`
			h := newChatGPTGenerationHarness(t, func(request chatGPTGenerationRequest) (string, string) {
				if reason == "daily_limit" {
					return answer, ""
				}
				return "", reason
			})
			// Keep the daily accounting independent of a test spanning UTC midnight.
			h.policy.now = func() time.Time { return time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC) }
			const localModelName = "fixture-local-fallback:latest"
			var mu sync.Mutex
			var requests []ollamaapi.ChatRequest
			var paths []string
			var violations []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				mu.Lock()
				defer mu.Unlock()
				paths = append(paths, r.Method+" "+r.URL.Path)
				if err != nil {
					violations = append(violations, "unreadable request")
				}
				if r.Header.Get("Authorization") != "" {
					violations = append(violations, "unexpected authorization header")
				}
				for _, secret := range []string{"fixture-owned-oauth-token", "fixture-refresh-token", "fixture-saved-id-token"} {
					for _, header := range r.Header {
						for _, value := range header {
							if strings.Contains(value, secret) {
								violations = append(violations, "subscription credential forwarded in header")
							}
						}
					}
					if strings.Contains(string(body), secret) {
						violations = append(violations, "subscription credential forwarded in body")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/":
					if r.Method != http.MethodHead {
						violations = append(violations, "heartbeat must use HEAD")
					}
					w.WriteHeader(http.StatusOK)
				case "/api/tags":
					_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]string{"name": localModelName, "model": localModelName}}})
				case "/api/show":
					var request ollamaapi.ShowRequest
					if json.Unmarshal(body, &request) != nil || request.Name != localModelName {
						violations = append(violations, "show must use the selected local model")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"capabilities": []string{"completion"}, "model_info": map[string]any{"general.architecture": "llama"}})
				case "/api/chat":
					var request ollamaapi.ChatRequest
					if json.Unmarshal(body, &request) != nil {
						violations = append(violations, "invalid chat request")
						http.Error(w, "invalid request", 400)
						return
					}
					requests = append(requests, request)
					_ = json.NewEncoder(w).Encode(map[string]any{"model": localModelName, "message": map[string]string{"role": "assistant", "content": answer}, "done": true, "done_reason": "stop", "prompt_eval_count": 4, "eval_count": 9})
				case "/api/pull":
					violations = append(violations, "the available fixture model must never be pulled")
					http.Error(w, "fixture model is already present", 400)
				default:
					violations = append(violations, "unexpected Ollama route "+r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			t.Setenv("OLLAMA_BASE_URL", server.URL)
			t.Setenv("OLLAMA_OPTIONAL", "false")
			t.Setenv("OLLAMA_API_KEY", "")
			local, err := ollama.GetOllamaService()
			require.NoError(t, err)
			require.NoError(t, local.StartService(h.ctx))
			info, err := local.GetModelInfo(h.ctx, localModelName)
			require.NoError(t, err)
			require.Len(t, info.Capabilities, 1)
			require.Equal(t, "completion", string(info.Capabilities[0]))
			h.models.ollamaService = local
			fallback := &types.Model{ID: "chatgpt-http-fallback", SpaceID: types.SingleSpaceID, Name: localModelName, Type: types.ModelTypeKnowledgeQA, Source: types.ModelSourceLocal, Status: types.ModelStatusActive}
			require.NoError(t, h.db.Create(fallback).Error)
			var limit *int
			if reason == "daily_limit" {
				value := 1
				limit = &value
			}
			_, err = h.policy.Update(h.ctx, limit, fallback.ID)
			require.NoError(t, err)
			model, err := h.models.GetChatModel(h.ctx, "chatgpt-generation")
			require.NoError(t, err)
			messages := []chat.Message{{Role: "system", Content: intakeReviewPrompt()}, {Role: "user", Content: `{"title":"Original publication","content":"Complete measured product capabilities"}`}}
			thinking := false
			opts := &chat.ChatOptions{Temperature: 0, TopP: 0.7, MaxTokens: 512, Thinking: &thinking, Format: json.RawMessage(`"json"`)}
			originalOptions, err := json.Marshal(opts)
			require.NoError(t, err)
			if reason == "daily_limit" {
				result, err := model.Chat(h.ctx, messages, opts)
				require.NoError(t, err)
				require.JSONEq(t, answer, result.Content)
			}
			for range 2 {
				result, err := model.Chat(h.ctx, messages, opts)
				require.NoError(t, err)
				require.JSONEq(t, answer, result.Content)
				policy, err := h.policy.Get(h.ctx)
				require.NoError(t, err)
				require.True(t, policy.Paused)
				require.Equal(t, reason, policy.Reason)
				require.Equal(t, 1, policy.DailyUsed, "local fallback calls must not reserve additional ChatGPT calls")
				require.Equal(t, fallback.ID, policy.FallbackModelID)
			}
			source := h.requests()
			require.Len(t, source, 1, "the current quota attempt reaches ChatGPT, and the paused call goes directly to the local fallback")
			require.Equal(t, messages[0].Content, source[0].Instructions)
			require.Equal(t, messages[1].Content, source[0].userText())
			mu.Lock()
			captured := append([]ollamaapi.ChatRequest(nil), requests...)
			seenPaths := append([]string(nil), paths...)
			contractViolations := append([]string(nil), violations...)
			mu.Unlock()
			require.Empty(t, contractViolations)
			require.Contains(t, seenPaths, "HEAD /")
			require.Contains(t, seenPaths, "GET /api/tags")
			require.Contains(t, seenPaths, "POST /api/show")
			require.NotContains(t, seenPaths, "POST /api/pull")
			require.Len(t, captured, 2)
			for _, request := range captured {
				require.Equal(t, localModelName, request.Model)
				require.NotNil(t, request.Stream)
				require.False(t, *request.Stream)
				require.Len(t, request.Messages, len(messages))
				for index, message := range messages {
					require.Equal(t, message.Role, request.Messages[index].Role)
					require.Equal(t, message.Content, request.Messages[index].Content)
				}
				require.JSONEq(t, `"json"`, string(request.Format))
				require.Equal(t, float64(0), request.Options["temperature"])
				require.Equal(t, float64(0.7), request.Options["top_p"])
				require.Equal(t, float64(512), request.Options["num_predict"])
				require.NotNil(t, request.Think)
				require.Equal(t, false, request.Think.Value)
			}
			afterOptions, err := json.Marshal(opts)
			require.NoError(t, err)
			require.JSONEq(t, string(originalOptions), string(afterOptions))
		})
	}
}
