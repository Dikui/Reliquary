package provider

import "reliquary/internal/types"

const (
	OpenAIBaseURL             = "https://api.openai.com/v1"
	AnthropicBaseURL          = "https://api.anthropic.com/v1"
	DeepSeekBaseURL           = "https://api.deepseek.com/v1"
	AliyunChatBaseURL         = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	AliyunRerankBaseURL       = "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank"
	ZhipuChatBaseURL          = "https://open.bigmodel.cn/api/paas/v4"
	ZhipuEmbeddingBaseURL     = ZhipuChatBaseURL
	MoonshotBaseURL           = "https://api.moonshot.ai/v1"
	SiliconFlowBaseURL        = "https://api.siliconflow.cn/v1"
	VolcengineChatBaseURL     = "https://ark.cn-beijing.volces.com/api/v3"
	OpenRouterBaseURL         = "https://openrouter.ai/api/v1"
	GeminiOpenAICompatBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"
	OllamaBaseURL             = "http://localhost:11434"
)

// The table is the single source for the settings API, defaults and detection.
// Only model types covered by the remaining clients are offered.
var presets = []ProviderInfo{
	{
		Name: ProviderGeneric, DisplayName: "通用 OpenAI 兼容", Description: "Custom OpenAI-compatible chat, embedding and /rerank endpoints",
		DefaultURLs:     map[types.ModelType]string{},
		ModelTypes:      []types.ModelType{types.ModelTypeKnowledgeQA, types.ModelTypeEmbedding, types.ModelTypeRerank, types.ModelTypeVLLM},
		Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "remote_api", types.ModelTypeEmbedding: "openai", types.ModelTypeRerank: "remote_api", types.ModelTypeVLLM: "remote_api"},
	},
	{
		Name: ProviderOpenAI, DisplayName: "OpenAI", Description: "GPT chat, reasoning and text embedding models", RequiresAuth: true,
		DefaultURLs:     map[types.ModelType]string{types.ModelTypeKnowledgeQA: OpenAIBaseURL, types.ModelTypeEmbedding: OpenAIBaseURL, types.ModelTypeVLLM: OpenAIBaseURL},
		ModelTypes:      []types.ModelType{types.ModelTypeKnowledgeQA, types.ModelTypeEmbedding, types.ModelTypeVLLM},
		Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "remote_api", types.ModelTypeEmbedding: "openai", types.ModelTypeVLLM: "remote_api"},
		URLRules:        []URLRule{{Hosts: []string{"api.openai.com"}}},
	},
	{
		Name: ProviderAnthropic, DisplayName: "Anthropic", Description: "Claude via the native Anthropic Messages API", RequiresAuth: true,
		DefaultURLs: map[types.ModelType]string{types.ModelTypeKnowledgeQA: AnthropicBaseURL},
		ModelTypes:  []types.ModelType{types.ModelTypeKnowledgeQA}, Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "anthropic"},
		URLRules: []URLRule{{Hosts: []string{"api.anthropic.com"}}},
	},
	{
		Name: ProviderDeepSeek, DisplayName: "DeepSeek", Description: "DeepSeek chat and reasoning models", RequiresAuth: true,
		DefaultURLs: map[types.ModelType]string{types.ModelTypeKnowledgeQA: DeepSeekBaseURL},
		ModelTypes:  []types.ModelType{types.ModelTypeKnowledgeQA}, Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "remote_api"},
		URLRules: []URLRule{{Hosts: []string{"api.deepseek.com"}}},
	},
	{
		Name: ProviderAliyun, DisplayName: "阿里云百炼", Description: "Qwen chat, text embeddings and native DashScope rerank", RequiresAuth: true,
		DefaultURLs:     map[types.ModelType]string{types.ModelTypeKnowledgeQA: AliyunChatBaseURL, types.ModelTypeEmbedding: AliyunChatBaseURL, types.ModelTypeRerank: AliyunRerankBaseURL, types.ModelTypeVLLM: AliyunChatBaseURL},
		ModelTypes:      []types.ModelType{types.ModelTypeKnowledgeQA, types.ModelTypeEmbedding, types.ModelTypeRerank, types.ModelTypeVLLM},
		Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "remote_api", types.ModelTypeEmbedding: "openai", types.ModelTypeRerank: "aliyun_rerank", types.ModelTypeVLLM: "remote_api"},
		URLRules:        []URLRule{{Hosts: []string{"dashscope.aliyuncs.com"}}},
	},
	{
		Name: ProviderZhipu, DisplayName: "智谱 BigModel", Description: "GLM chat and OpenAI-compatible text embeddings", RequiresAuth: true,
		DefaultURLs:     map[types.ModelType]string{types.ModelTypeKnowledgeQA: ZhipuChatBaseURL, types.ModelTypeEmbedding: ZhipuEmbeddingBaseURL, types.ModelTypeVLLM: ZhipuChatBaseURL},
		ModelTypes:      []types.ModelType{types.ModelTypeKnowledgeQA, types.ModelTypeEmbedding, types.ModelTypeVLLM},
		Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "remote_api", types.ModelTypeEmbedding: "openai", types.ModelTypeVLLM: "remote_api"},
		URLRules:        []URLRule{{Hosts: []string{"open.bigmodel.cn"}}},
	},
	{
		Name: ProviderMoonshot, DisplayName: "月之暗面 Moonshot", Description: "Kimi and Moonshot chat models", RequiresAuth: true,
		DefaultURLs:     map[types.ModelType]string{types.ModelTypeKnowledgeQA: MoonshotBaseURL, types.ModelTypeVLLM: MoonshotBaseURL},
		ModelTypes:      []types.ModelType{types.ModelTypeKnowledgeQA, types.ModelTypeVLLM},
		Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "remote_api", types.ModelTypeVLLM: "remote_api"},
		URLRules:        []URLRule{{Hosts: []string{"api.moonshot.ai", "api.moonshot.cn"}}},
	},
	{
		Name: ProviderSiliconFlow, DisplayName: "硅基流动 SiliconFlow", Description: "OpenAI-compatible chat, embeddings and /rerank models", RequiresAuth: true,
		DefaultURLs:     map[types.ModelType]string{types.ModelTypeKnowledgeQA: SiliconFlowBaseURL, types.ModelTypeEmbedding: SiliconFlowBaseURL, types.ModelTypeRerank: SiliconFlowBaseURL, types.ModelTypeVLLM: SiliconFlowBaseURL},
		ModelTypes:      []types.ModelType{types.ModelTypeKnowledgeQA, types.ModelTypeEmbedding, types.ModelTypeRerank, types.ModelTypeVLLM},
		Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "remote_api", types.ModelTypeEmbedding: "openai", types.ModelTypeRerank: "remote_api", types.ModelTypeVLLM: "remote_api"},
		URLRules:        []URLRule{{Hosts: []string{"siliconflow.cn", "siliconflow.ai"}}},
	},
	{
		Name: ProviderVolcengine, DisplayName: "火山方舟", Description: "Doubao chat via the OpenAI-compatible Ark API", RequiresAuth: true,
		DefaultURLs:     map[types.ModelType]string{types.ModelTypeKnowledgeQA: VolcengineChatBaseURL, types.ModelTypeVLLM: VolcengineChatBaseURL},
		ModelTypes:      []types.ModelType{types.ModelTypeKnowledgeQA, types.ModelTypeVLLM},
		Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "remote_api", types.ModelTypeVLLM: "remote_api"},
		URLRules:        []URLRule{{Hosts: []string{"volces.com"}}},
	},
	{
		Name: ProviderOpenRouter, DisplayName: "OpenRouter", Description: "Routed OpenAI-compatible chat and embedding models", RequiresAuth: true,
		DefaultURLs:     map[types.ModelType]string{types.ModelTypeKnowledgeQA: OpenRouterBaseURL, types.ModelTypeEmbedding: OpenRouterBaseURL, types.ModelTypeVLLM: OpenRouterBaseURL},
		ModelTypes:      []types.ModelType{types.ModelTypeKnowledgeQA, types.ModelTypeEmbedding, types.ModelTypeVLLM},
		Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "remote_api", types.ModelTypeEmbedding: "openai", types.ModelTypeVLLM: "remote_api"},
		URLRules:        []URLRule{{Hosts: []string{"openrouter.ai"}}},
	},
	{
		Name: ProviderGemini, DisplayName: "Google Gemini", Description: "Gemini chat and embeddings via the OpenAI-compatible API", RequiresAuth: true,
		DefaultURLs:     map[types.ModelType]string{types.ModelTypeKnowledgeQA: GeminiOpenAICompatBaseURL, types.ModelTypeEmbedding: GeminiOpenAICompatBaseURL},
		ModelTypes:      []types.ModelType{types.ModelTypeKnowledgeQA, types.ModelTypeEmbedding},
		Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "remote_api", types.ModelTypeEmbedding: "openai"},
		URLRules:        []URLRule{{Hosts: []string{"generativelanguage.googleapis.com"}}},
	},
	{
		Name: ProviderOllama, DisplayName: "Ollama", Description: "Local chat, embedding and vision models with pull and list management",
		DefaultURLs:     map[types.ModelType]string{types.ModelTypeKnowledgeQA: OllamaBaseURL, types.ModelTypeEmbedding: OllamaBaseURL, types.ModelTypeVLLM: OllamaBaseURL},
		ModelTypes:      []types.ModelType{types.ModelTypeKnowledgeQA, types.ModelTypeEmbedding, types.ModelTypeVLLM},
		Implementations: map[types.ModelType]string{types.ModelTypeKnowledgeQA: "ollama", types.ModelTypeEmbedding: "ollama", types.ModelTypeVLLM: "ollama"},
		URLRules:        []URLRule{{Hosts: []string{"localhost", "127.0.0.1", "::1", "host.docker.internal"}, Port: "11434"}, {Hosts: []string{"ollama"}}},
	},
}
