package chat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/models/provider"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

func TestSavedModelChatUsesGenericClientForRemovedProviders(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "invalid JSON", 400)
			return
		}
		assert.Equal(t, "saved-model", body["model"])
		assert.Equal(t, map[string]any{"enable_thinking": false}, body["chat_template_kwargs"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"saved","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	for _, name := range []string{"litellm", "requesty", "gpustack", "modelscope", "qianfan", "qiniu", "longcat", "lkeap", "nvidia", "novita", "azure_openai", "hunyuan", "minimax", "mimo", "jina"} {
		t.Run(name, func(t *testing.T) {
			model := &types.Model{Name: "saved-model", Source: types.ModelSourceRemote, Parameters: types.ModelParameters{Provider: name, BaseURL: server.URL + "/v1", APIKey: "test-key"}}
			c, err := NewChat(ConfigFromModel(model), nil)
			require.NoError(t, err)
			response, err := c.Chat(t.Context(), []Message{{Role: "user", Content: "hi"}}, &ChatOptions{Thinking: ptrBool(false)})
			require.NoError(t, err)
			assert.Equal(t, "ok", response.Content)
			assert.Equal(t, provider.ProviderGeneric, provider.Resolve(provider.ProviderName(name), model.Parameters.BaseURL))
		})
	}
}
