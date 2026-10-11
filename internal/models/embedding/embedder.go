package embedding

import (
	"context"
	"fmt"
	"strings"

	"reliquary/internal/logger"
	"reliquary/internal/models/provider"
	"reliquary/internal/models/utils/ollama"
	"reliquary/internal/types"
)

// Embedder defines the interface for text vectorization
type Embedder interface {
	// Embed converts text to vector
	Embed(ctx context.Context, text string) ([]float32, error)

	// BatchEmbed converts multiple texts to vectors in batch
	BatchEmbed(ctx context.Context, texts []string) ([][]float32, error)

	// GetModelName returns the model name
	GetModelName() string

	// GetDimensions returns the vector dimensions
	GetDimensions() int

	// GetModelID returns the model ID
	GetModelID() string

	EmbedderPooler
}

// ImageEmbedder embeds image bytes represented by a data URI in the same space
// as Embed and BatchEmbed. Callers must check SupportsImages before using it.
type ImageEmbedder interface {
	EmbedImage(context.Context, string) ([]float32, error)
	SupportsImages() bool
}

// ProfiledEmbedder identifies the model configuration used to create a client.
type ProfiledEmbedder interface{ GetProfileHash() string }

type EmbedderPooler interface {
	BatchEmbedWithPool(ctx context.Context, model Embedder, texts []string) ([][]float32, error)
}

// EmbedderType represents the embedder type
type EmbedderType string

// Config represents the embedder configuration
type Config struct {
	Source                    types.ModelSource `json:"source"`
	BaseURL                   string            `json:"base_url"`
	ModelName                 string            `json:"model_name"`
	APIKey                    string            `json:"api_key"`
	TruncatePromptTokens      int               `json:"truncate_prompt_tokens"`
	Dimensions                int               `json:"dimensions"`
	SupportsDimensionOverride bool              `json:"supports_dimension_override"`
	SupportsImages            bool              `json:"supports_images"`
	MultimodalFormat          string            `json:"multimodal_format"`
	ProfileHash               string            `json:"-"`
	ModelID                   string            `json:"model_id"`
	Provider                  string            `json:"provider"`
	// MaxConcurrency caps concurrent background calls to this model; 0 falls
	// back to the process-wide default (see limiter.GateN).
	MaxConcurrency    int               `json:"max_concurrency"`
	RequestsPerMinute int               `json:"requests_per_minute"`
	ExtraConfig       map[string]string `json:"extra_config"`
	// CustomHeaders 允许在调用远程 API 时附加自定义 HTTP 请求头（类似 OpenAI Python SDK 的 extra_headers）。
	CustomHeaders map[string]string `json:"custom_headers"`
}

// ConfigFromModel 根据 types.Model 构造 embedding.Config。
// 生产路径（从 DB 拉起）和测试连接路径（临时表单）共享这份映射。
func ConfigFromModel(m *types.Model) Config {
	if m == nil {
		return Config{}
	}
	return Config{
		Source:                    m.Source,
		BaseURL:                   m.Parameters.BaseURL,
		APIKey:                    m.Parameters.APIKey,
		ModelID:                   m.ID,
		ModelName:                 m.Name,
		Dimensions:                m.Parameters.EmbeddingParameters.Dimension,
		SupportsDimensionOverride: m.Parameters.EmbeddingParameters.SupportsDimensionOverride,
		SupportsImages:            m.Parameters.EmbeddingParameters.SupportsImages,
		MultimodalFormat:          m.Parameters.EmbeddingParameters.MultimodalFormat,
		ProfileHash:               types.EmbeddingProfileHash(m.ID, m.Parameters.EmbeddingParameters.Dimension, m),
		TruncatePromptTokens:      m.Parameters.EmbeddingParameters.TruncatePromptTokens,
		Provider:                  m.Parameters.Provider,
		MaxConcurrency:            m.Parameters.MaxConcurrency,
		RequestsPerMinute:         m.Parameters.RequestsPerMinute,
		ExtraConfig:               m.Parameters.ExtraConfig,
		CustomHeaders:             m.Parameters.CustomHeaders,
	}
}

// NewEmbedder creates an embedder based on the configuration
func NewEmbedder(config Config, pooler EmbedderPooler, ollamaService *ollama.OllamaService) (Embedder, error) {
	e, err := newEmbedder(config, pooler, ollamaService)
	if err != nil {
		return e, err
	}
	if setter, ok := e.(interface{ SetSupportsDimensionOverride(bool) }); ok {
		setter.SetSupportsDimensionOverride(config.SupportsDimensionOverride)
	}
	// Innermost: gate real provider calls, including per-sub-batch pool callbacks,
	// before debug logging. See concurrencyEmbedder for the placement rationale.
	e = wrapEmbeddingConcurrency(e, config.MaxConcurrency)
	if w, ok := e.(*concurrencyEmbedder); ok {
		w.requestsPerMinute = config.RequestsPerMinute
		w.profileHash = config.ProfileHash
	}
	if logger.LLMDebugEnabled() {
		e = &debugEmbedder{inner: e}
	}

	return e, nil
}

func newEmbedder(config Config, pooler EmbedderPooler, ollamaService *ollama.OllamaService) (Embedder, error) {
	if config.SupportsImages {
		if config.Source != types.ModelSourceRemote {
			return nil, fmt.Errorf("image embedding requires a remote HTTP model")
		}
		if format := strings.TrimSpace(config.MultimodalFormat); format != "" && format != "jina" {
			return nil, fmt.Errorf("unsupported multimodal embedding format: %s", format)
		}
	}
	switch strings.ToLower(string(config.Source)) {
	case string(types.ModelSourceLocal):
		return NewOllamaEmbedder(config.BaseURL,
			config.ModelName, config.TruncatePromptTokens, config.Dimensions, config.ModelID, pooler, ollamaService)
	case string(types.ModelSourceRemote):
		providerName := provider.Resolve(provider.ProviderName(config.Provider), config.BaseURL)
		baseURL := config.BaseURL
		if baseURL == "" {
			baseURL = provider.GetOrDefault(providerName).Info().GetDefaultURL(types.ModelTypeEmbedding)
		}
		embedder, err := NewOpenAIEmbedder(config.APIKey,
			baseURL,
			config.ModelName,
			config.TruncatePromptTokens,
			config.Dimensions,
			config.ModelID,
			pooler)
		if err != nil {
			return nil, err
		}
		embedder.SetCustomHeaders(config.CustomHeaders)
		embedder.supportsImages = config.SupportsImages
		return embedder, nil
	default:
		return nil, fmt.Errorf("unsupported embedder source: %s", config.Source)
	}
}
