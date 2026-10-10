// Package provider defines model service presets and their shared API implementations.
package provider

import (
	"fmt"
	"net/url"
	"reliquary/internal/types"
	"strings"
)

type ProviderName string

const (
	ProviderGeneric     ProviderName = "generic"
	ProviderOpenAI      ProviderName = "openai"
	ProviderAnthropic   ProviderName = "anthropic"
	ProviderDeepSeek    ProviderName = "deepseek"
	ProviderAliyun      ProviderName = "aliyun"
	ProviderZhipu       ProviderName = "zhipu"
	ProviderMoonshot    ProviderName = "moonshot"
	ProviderSiliconFlow ProviderName = "siliconflow"
	ProviderVolcengine  ProviderName = "volcengine"
	ProviderOpenRouter  ProviderName = "openrouter"
	ProviderGemini      ProviderName = "gemini"
	ProviderOllama      ProviderName = "ollama"
)

// URLRule matches a hostname or its subdomains, with an optional port.
type URLRule struct {
	Hosts []string
	Port  string
}

// ProviderInfo is a preset, rather than a separate vendor client.
type ProviderInfo struct {
	Name            ProviderName
	DisplayName     string
	Description     string
	DefaultURLs     map[types.ModelType]string
	ModelTypes      []types.ModelType
	RequiresAuth    bool
	Implementations map[types.ModelType]string
	URLRules        []URLRule
}

func (p ProviderInfo) Info() ProviderInfo { return p }

func (p ProviderInfo) GetDefaultURL(modelType types.ModelType) string {
	if address, ok := p.DefaultURLs[modelType]; ok {
		return address
	}
	return p.DefaultURLs[types.ModelTypeKnowledgeQA]
}

func (p ProviderInfo) ValidateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}
	if p.RequiresAuth && config.APIKey == "" {
		return fmt.Errorf("API key is required for %s provider", p.DisplayName)
	}
	if p.Name == ProviderGeneric && config.BaseURL == "" {
		return fmt.Errorf("base URL is required for generic provider")
	}
	if config.ModelName == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}

type Config struct {
	Provider  ProviderName   `json:"provider"`
	BaseURL   string         `json:"base_url"`
	APIKey    string         `json:"api_key"`
	ModelName string         `json:"model_name"`
	ModelID   string         `json:"model_id"`
	Extra     map[string]any `json:"extra,omitempty"`
}

func AllProviders() []ProviderName {
	names := make([]ProviderName, 0, len(presets))
	for _, p := range presets {
		names = append(names, p.Name)
	}
	return names
}

func Get(name ProviderName) (ProviderInfo, bool) {
	for _, p := range presets {
		if p.Name == name {
			return p, true
		}
	}
	return ProviderInfo{}, false
}

func GetOrDefault(name ProviderName) ProviderInfo {
	if p, ok := Get(name); ok {
		return p
	}
	return presets[0]
}

func List() []ProviderInfo { return append([]ProviderInfo(nil), presets...) }

func ListByModelType(modelType types.ModelType) []ProviderInfo {
	result := make([]ProviderInfo, 0)
	for _, p := range presets {
		for _, mt := range p.ModelTypes {
			if mt == modelType {
				result = append(result, p)
				break
			}
		}
	}
	return result
}

func DetectProvider(baseURL string) ProviderName {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return ProviderGeneric
	}
	host := strings.ToLower(u.Hostname())
	for _, p := range presets {
		for _, rule := range p.URLRules {
			if rule.Port != "" && u.Port() != rule.Port {
				continue
			}
			for _, suffix := range rule.Hosts {
				if host == suffix || strings.HasSuffix(host, "."+suffix) {
					return p.Name
				}
			}
		}
	}
	return ProviderGeneric
}

// Resolve keeps explicit unknown or removed providers on the generic protocol.
// Only models without a provider use address detection.
func Resolve(name ProviderName, baseURL string) ProviderName {
	if name == "" {
		return DetectProvider(baseURL)
	}
	return GetOrDefault(name).Name
}

func NewConfigFromModel(model *types.Model) (*Config, error) {
	if model == nil {
		return nil, fmt.Errorf("model is nil")
	}
	return &Config{Provider: Resolve(ProviderName(model.Parameters.Provider), model.Parameters.BaseURL), BaseURL: model.Parameters.BaseURL, APIKey: model.Parameters.APIKey, ModelName: model.Name, ModelID: model.ID}, nil
}

// IsOpenAIReasoningOrGPT5Model 判断模型是否为 OpenAI 的
// 推理类（o-series）或 GPT-5 系列模型。
//
// 这些模型在 OpenAI Chat Completions API 中：
//   - 不再支持 `max_tokens`，必须使用 `max_completion_tokens`；
//   - 仅支持默认的 `temperature=1`、`top_p=1`，且不支持 `frequency_penalty` /
//     `presence_penalty` 等采样参数（传非默认值会被拒绝）。
//
// 参考：
//   - https://platform.openai.com/docs/api-reference/chat
//
// 仅基于模型名做启发式匹配。
func IsOpenAIReasoningOrGPT5Model(modelName string) bool {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, "gpt-5") {
		return true
	}
	// o1 / o1-mini / o1-preview / o3 / o3-mini / o4-mini ...
	// 必须精确匹配，避免误命中 "openai-..." 之类。
	for _, prefix := range []string{"o1", "o3", "o4"} {
		if name == prefix || strings.HasPrefix(name, prefix+"-") {
			return true
		}
	}
	return false
}

// IsQwenThinkingModel 检查模型名是否为支持思维链的 Qwen 模型
// 支持思维链的模型需要特殊处理 enable_thinking 参数
func IsQwenThinkingModel(modelName string) bool {
	lowerName := strings.ToLower(modelName)
	return strings.HasPrefix(lowerName, "qwen3") ||
		strings.HasPrefix(lowerName, "qwen-plus") ||
		strings.HasPrefix(lowerName, "qwen-max") ||
		strings.HasPrefix(lowerName, "qwen-turbo")
}

// IsQwen3Model checks whether the model belongs to the Qwen3 family only.
func IsQwen3Model(modelName string) bool {
	return strings.HasPrefix(strings.ToLower(modelName), "qwen3")
}

// IsDeepSeekModel 检查模型名是否为 DeepSeek 模型
// DeepSeek 模型不支持 tool_choice 参数
func IsDeepSeekModel(modelName string) bool {
	return strings.Contains(strings.ToLower(modelName), "deepseek")
}

// IsMoonshotFixedTempModel reports whether the given Moonshot/Kimi model
// only accepts temperature=1. The following models reject any temperature
// value other than 1:
//   - moonshot-v1 series (moonshot-v1-8k, moonshot-v1-32k, moonshot-v1-128k)
//   - kimi-k2.5 and kimi-k2.6 (no temperature param in API docs)
//
// kimi-k2 / kimi-k2-turbo / kimi-k2-thinking models accept the full [0,1]
// range and are NOT affected.
func IsMoonshotFixedTempModel(modelName string) bool {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if strings.HasPrefix(name, "moonshot-v1") {
		return true
	}
	// kimi-k2.5, kimi-k2.6 — no temperature parameter supported
	if name == "kimi-k2.5" || name == "kimi-k2.6" {
		return true
	}
	return false
}
