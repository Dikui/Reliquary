package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
)

func TestProviderRegistry(t *testing.T) {
	// Test that all default providers are registered
	t.Run("default providers registered", func(t *testing.T) {
		providers := List()
		assert.NotEmpty(t, providers, "should have registered providers")

		// Check specific providers exist
		for _, name := range []ProviderName{ProviderOpenAI, ProviderAliyun, ProviderZhipu, ProviderGeneric} {
			p, ok := Get(name)
			assert.True(t, ok, "provider %s should be registered", name)
			assert.NotNil(t, p, "provider %s should not be nil", name)
		}
	})

	t.Run("GetOrDefault fallback", func(t *testing.T) {
		// Non-existent provider should fall back to generic
		p := GetOrDefault("nonexistent")
		require.NotNil(t, p)
		assert.Equal(t, ProviderGeneric, p.Info().Name)
	})
}

func TestDetectProvider(t *testing.T) {
	tests := []struct {
		url      string
		expected ProviderName
	}{
		{"https://custom-endpoint.example.com/v1", ProviderGeneric},
		{"https://api.openai.com/v1", ProviderOpenAI},
		{"https://api.anthropic.com/v1", ProviderAnthropic},
		{"https://api.deepseek.com/v1", ProviderDeepSeek},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", ProviderAliyun},
		{"https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank", ProviderAliyun},
		{"https://open.bigmodel.cn/api/paas/v4", ProviderZhipu},
		{"https://api.moonshot.ai/v1", ProviderMoonshot},
		{"https://api.moonshot.cn/v1", ProviderMoonshot},
		{"https://api.siliconflow.cn/v1", ProviderSiliconFlow},
		{"https://api.siliconflow.ai/v1", ProviderSiliconFlow},
		{"https://ark.cn-beijing.volces.com/api/v3", ProviderVolcengine},
		{"https://openrouter.ai/api/v1", ProviderOpenRouter},
		{"https://generativelanguage.googleapis.com/v1beta/openai/", ProviderGemini},
		{"http://localhost:11434", ProviderOllama},
		{"http://127.0.0.1:11434", ProviderOllama},
		{"http://ollama:11434", ProviderOllama},
		{"http://localhost:4000/v1", ProviderGeneric},
		{"https://api.openai.com.example.test/v1", ProviderGeneric},
		{"https://example.test/api.openai.com", ProviderGeneric},
		{"not a URL", ProviderGeneric},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) { assert.Equal(t, tt.expected, DetectProvider(tt.url)) })
	}
}

func TestAnthropicProviderValidation(t *testing.T) {
	p := GetOrDefault(ProviderAnthropic)

	t.Run("valid config", func(t *testing.T) {
		config := &Config{
			APIKey:    "sk-ant-test",
			ModelName: "claude-sonnet-4-5",
		}
		err := p.ValidateConfig(config)
		assert.NoError(t, err)
	})

	t.Run("missing API key", func(t *testing.T) {
		config := &Config{
			ModelName: "claude-sonnet-4-5",
		}
		err := p.ValidateConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API key")
	})

	t.Run("info", func(t *testing.T) {
		info := p.Info()
		assert.Equal(t, ProviderAnthropic, info.Name)
		assert.Equal(t, AnthropicBaseURL, info.GetDefaultURL(types.ModelTypeKnowledgeQA))
		assert.Contains(t, info.ModelTypes, types.ModelTypeKnowledgeQA)
		assert.True(t, info.RequiresAuth)
	})
}

func TestOpenAIProviderValidation(t *testing.T) {
	p := GetOrDefault(ProviderOpenAI)

	t.Run("valid config", func(t *testing.T) {
		config := &Config{
			APIKey:    "sk-test",
			ModelName: "gpt-4",
		}
		err := p.ValidateConfig(config)
		assert.NoError(t, err)
	})

	t.Run("missing API key", func(t *testing.T) {
		config := &Config{
			ModelName: "gpt-4",
		}
		err := p.ValidateConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API key")
	})

	t.Run("missing model name", func(t *testing.T) {
		config := &Config{
			APIKey: "sk-test",
		}
		err := p.ValidateConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "model name")
	})
}

func TestAliyunProviderValidation(t *testing.T) {
	p := GetOrDefault(ProviderAliyun)

	t.Run("valid config", func(t *testing.T) {
		config := &Config{
			APIKey:    "sk-test",
			ModelName: "qwen-max",
		}
		err := p.ValidateConfig(config)
		assert.NoError(t, err)
	})

	t.Run("info", func(t *testing.T) {
		info := p.Info()
		assert.Equal(t, ProviderAliyun, info.Name)
		assert.Contains(t, info.ModelTypes, types.ModelTypeKnowledgeQA)
		assert.Contains(t, info.ModelTypes, types.ModelTypeEmbedding)
		assert.Contains(t, info.ModelTypes, types.ModelTypeRerank)
	})
}

func TestAliyunModelDetection(t *testing.T) {
	t.Run("Qwen3 model detection", func(t *testing.T) {
		assert.True(t, IsQwen3Model("qwen3-32b"))
		assert.True(t, IsQwen3Model("qwen3-72b"))
		assert.False(t, IsQwen3Model("qwen-max"))
		assert.False(t, IsQwen3Model("qwen2.5-72b"))
	})

	t.Run("DeepSeek model detection", func(t *testing.T) {
		assert.True(t, IsDeepSeekModel("deepseek-chat"))
		assert.True(t, IsDeepSeekModel("deepseek-v3.1"))
		assert.True(t, IsDeepSeekModel("DeepSeek-Chat"))
		assert.False(t, IsDeepSeekModel("qwen-max"))
	})
}

func TestZhipuProviderValidation(t *testing.T) {
	p := GetOrDefault(ProviderZhipu)

	t.Run("valid config", func(t *testing.T) {
		config := &Config{
			APIKey:    "test-key",
			ModelName: "glm-4",
		}
		err := p.ValidateConfig(config)
		assert.NoError(t, err)
	})

	t.Run("info", func(t *testing.T) {
		info := p.Info()
		assert.Equal(t, ProviderZhipu, info.Name)
		assert.Equal(t, ZhipuChatBaseURL, info.GetDefaultURL(types.ModelTypeKnowledgeQA))
		assert.Equal(t, ZhipuEmbeddingBaseURL, info.GetDefaultURL(types.ModelTypeEmbedding))
	})
}

func TestListByModelType(t *testing.T) {
	t.Run("chat models", func(t *testing.T) {
		providers := ListByModelType(types.ModelTypeKnowledgeQA)
		assert.NotEmpty(t, providers)
		// Multiple providers support chat
		assert.GreaterOrEqual(t, len(providers), 9)
	})

	t.Run("rerank models", func(t *testing.T) {
		providers := ListByModelType(types.ModelTypeRerank)
		assert.NotEmpty(t, providers)
		// Check that Aliyun supports rerank
		foundAliyun := false
		for _, p := range providers {
			if p.Name == ProviderAliyun {
				foundAliyun = true
			}
		}
		assert.True(t, foundAliyun, "Aliyun should support rerank")
	})

	t.Run("embedding models include openrouter", func(t *testing.T) {
		providers := ListByModelType(types.ModelTypeEmbedding)
		assert.NotEmpty(t, providers)

		found := false
		for _, p := range providers {
			if p.Name == ProviderOpenRouter {
				found = true
				assert.Equal(t, OpenRouterBaseURL, p.GetDefaultURL(types.ModelTypeEmbedding))
				break
			}
		}

		assert.True(t, found, "OpenRouter should support embedding")
	})

	t.Run("embedding models include gemini", func(t *testing.T) {
		providers := ListByModelType(types.ModelTypeEmbedding)
		assert.NotEmpty(t, providers)

		found := false
		for _, p := range providers {
			if p.Name == ProviderGemini {
				found = true
				assert.Equal(t, GeminiOpenAICompatBaseURL, p.GetDefaultURL(types.ModelTypeEmbedding))
				break
			}
		}

		assert.True(t, found, "Gemini should support embedding via the OpenAI-compatible API")
	})
}

func TestPresetCatalog(t *testing.T) {
	want := []ProviderName{ProviderGeneric, ProviderOpenAI, ProviderAnthropic, ProviderDeepSeek, ProviderAliyun, ProviderZhipu, ProviderMoonshot, ProviderSiliconFlow, ProviderVolcengine, ProviderOpenRouter, ProviderGemini, ProviderOllama}
	assert.Equal(t, want, AllProviders())
	require.Len(t, List(), len(want))
	for _, p := range List() {
		t.Run(string(p.Name), func(t *testing.T) {
			assert.NotEmpty(t, p.DisplayName)
			assert.NotEmpty(t, p.Description)
			assert.NotEmpty(t, p.ModelTypes)
			for _, mt := range p.ModelTypes {
				assert.NotEmpty(t, p.Implementations[mt])
				if p.Name != ProviderGeneric {
					assert.NotEmpty(t, p.DefaultURLs[mt])
				}
			}
			if p.Name != ProviderGeneric {
				assert.NotEmpty(t, p.URLRules)
			}
			assert.Equal(t, p.Name != ProviderGeneric && p.Name != ProviderOllama, p.RequiresAuth)
		})
	}
	assert.Equal(t, AliyunChatBaseURL, GetOrDefault(ProviderAliyun).DefaultURLs[types.ModelTypeEmbedding])
	assert.Equal(t, GeminiOpenAICompatBaseURL, GetOrDefault(ProviderGemini).DefaultURLs[types.ModelTypeEmbedding])
	for _, p := range ListByModelType(types.ModelTypeRerank) {
		assert.Contains(t, []ProviderName{ProviderGeneric, ProviderAliyun, ProviderSiliconFlow}, p.Name)
	}
}

func TestRemovedProviderConfigFallsBack(t *testing.T) {
	model := &types.Model{Name: "saved-model", Parameters: types.ModelParameters{Provider: "azure_openai", BaseURL: "https://api.anthropic.com/v1"}}
	config, err := NewConfigFromModel(model)
	require.NoError(t, err)
	assert.Equal(t, ProviderGeneric, config.Provider)
	assert.Equal(t, model.Parameters.BaseURL, config.BaseURL)
	assert.Equal(t, ProviderAnthropic, Resolve("", model.Parameters.BaseURL))
}
