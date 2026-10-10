package rerank

import (
	"context"
	"encoding/json"
	"fmt"

	"reliquary/internal/logger"
	"reliquary/internal/models/provider"
	"reliquary/internal/types"
)

// Reranker defines the interface for document reranking
type Reranker interface {
	// Rerank reranks documents based on relevance to the query
	Rerank(ctx context.Context, query string, documents []string) ([]RankResult, error)

	// GetModelName returns the model name
	GetModelName() string

	// GetModelID returns the model ID
	GetModelID() string
}

type RankResult struct {
	Index          int          `json:"index"`
	Document       DocumentInfo `json:"document"`
	RelevanceScore float64      `json:"relevance_score"`
}

// Handles the RelevanceScore field by checking if RelevanceScore exists first, otherwise falls back to Score field
func (r *RankResult) UnmarshalJSON(data []byte) error {
	var temp struct {
		Index          int          `json:"index"`
		Document       DocumentInfo `json:"document"`
		RelevanceScore *float64     `json:"relevance_score"`
		Score          *float64     `json:"score"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal rank result: %w", err)
	}

	r.Index = temp.Index
	r.Document = temp.Document

	if temp.RelevanceScore != nil {
		r.RelevanceScore = *temp.RelevanceScore
	} else if temp.Score != nil {
		r.RelevanceScore = *temp.Score
	}

	return nil
}

type DocumentInfo struct {
	Text string `json:"text"`
}

// UnmarshalJSON handles both string and object formats for DocumentInfo
func (d *DocumentInfo) UnmarshalJSON(data []byte) error {
	// First try to unmarshal as a string
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		d.Text = text
		return nil
	}

	// If that fails, try to unmarshal as an object with text field
	var temp struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal DocumentInfo: %w", err)
	}

	d.Text = temp.Text
	return nil
}

type RerankerConfig struct {
	MaxConcurrency    int
	RequestsPerMinute int
	APIKey            string
	BaseURL           string
	ModelName         string
	Source            types.ModelSource
	ModelID           string
	Provider          string // Provider preset identifier
	ExtraConfig       map[string]string
	// CustomHeaders 允许在调用远程 API 时附加自定义 HTTP 请求头（类似 OpenAI Python SDK 的 extra_headers）。
	CustomHeaders map[string]string
	AppSecret     string // 加密值，工厂函数调用方传入，使用前已解密
}

// ConfigFromModel 根据 types.Model 构造 RerankerConfig。
// 生产路径（从 DB 拉起）和测试连接路径（临时表单）共享这份映射。
// appSecret 保留现有模型配置的映射，不参与当前重排请求。
func ConfigFromModel(m *types.Model, appSecret string) *RerankerConfig {
	if m == nil {
		return nil
	}
	return &RerankerConfig{
		MaxConcurrency:    m.Parameters.MaxConcurrency,
		RequestsPerMinute: m.Parameters.RequestsPerMinute,
		ModelID:           m.ID,
		APIKey:            m.Parameters.APIKey,
		BaseURL:           m.Parameters.BaseURL,
		ModelName:         m.Name,
		Source:            m.Source,
		Provider:          m.Parameters.Provider,
		ExtraConfig:       m.Parameters.ExtraConfig,
		CustomHeaders:     m.Parameters.CustomHeaders,
		AppSecret:         appSecret,
	}
}

// NewReranker creates a reranker based on the configuration
func NewReranker(config *RerankerConfig) (Reranker, error) {
	r, err := newReranker(config)
	if err != nil {
		return r, err
	}
	if logger.LLMDebugEnabled() {
		r = &debugReranker{inner: r}
	}
	return &governedReranker{inner: r, limit: config.MaxConcurrency, requestsPerMinute: config.RequestsPerMinute}, nil
}

// customHeaderSetter 表示支持注入自定义 HTTP header 的 reranker 实现。
type customHeaderSetter interface {
	SetCustomHeaders(map[string]string)
}

func newReranker(config *RerankerConfig) (Reranker, error) {
	providerName := provider.Resolve(provider.ProviderName(config.Provider), config.BaseURL)
	resolved := *config
	if resolved.BaseURL == "" {
		resolved.BaseURL = provider.GetOrDefault(providerName).Info().GetDefaultURL(types.ModelTypeRerank)
	}

	var (
		reranker Reranker
		err      error
	)
	switch providerName {
	case provider.ProviderAliyun:
		reranker, err = NewAliyunReranker(&resolved)
	default:
		reranker, err = NewOpenAIReranker(&resolved)
	}
	if err != nil {
		return nil, err
	}
	if setter, ok := reranker.(customHeaderSetter); ok {
		setter.SetCustomHeaders(config.CustomHeaders)
	}
	return reranker, nil
}
