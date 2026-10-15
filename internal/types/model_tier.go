package types

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ModelTier identifies an independent processing and model selection budget.
type ModelTier string

const (
	ModelTierLight            ModelTier = "light"
	ModelTierEntityResolution ModelTier = "entity_resolution"
	ModelTierGeneration       ModelTier = "generation"
	ModelTierEmbedding        ModelTier = "embedding"
	ModelTierRerank           ModelTier = "rerank"
)

var ModelTiers = []ModelTier{ModelTierLight, ModelTierEntityResolution, ModelTierGeneration, ModelTierEmbedding, ModelTierRerank}

// ModelTierConfig stores the model choices for the library.
type ModelTierConfig struct {
	Light            string `json:"light"`
	EntityResolution string `json:"entity_resolution"`
	Generation       string `json:"generation"`
	Embedding        string `json:"embedding"`
	Rerank           string `json:"rerank"`
}

func (c ModelTierConfig) ModelID(tier ModelTier) string {
	switch tier {
	case ModelTierLight:
		return strings.TrimSpace(c.Light)
	case ModelTierEntityResolution:
		return strings.TrimSpace(c.EntityResolution)
	case ModelTierGeneration:
		return strings.TrimSpace(c.Generation)
	case ModelTierEmbedding:
		return strings.TrimSpace(c.Embedding)
	case ModelTierRerank:
		return strings.TrimSpace(c.Rerank)
	}
	return ""
}

func (c ModelTierConfig) Validate() error {
	for _, tier := range ModelTiers {
		if len(c.ModelID(tier)) > ModelIDMaxLen {
			return fmt.Errorf("%s model ID is too long", tier)
		}
	}
	return nil
}

func (c ModelTierConfig) UsageBindings(modelID string) []ModelUsageBinding {
	result := []ModelUsageBinding{}
	for _, tier := range ModelTiers {
		if c.ModelID(tier) == modelID {
			result = append(result, ModelUsageBinding(string(tier)+"_model_tier"))
		}
	}
	return result
}

func (c ModelTierConfig) Value() (driver.Value, error) { return json.Marshal(c) }
func (c *ModelTierConfig) Scan(value any) error {
	if value == nil {
		*c = ModelTierConfig{}
		return nil
	}
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("invalid model tier config %T", value)
	}
	return json.Unmarshal(data, c)
}

type modelTierContextKey struct{}
type modelCallsContextKey struct{}

func WithModelTier(ctx context.Context, tier ModelTier) context.Context {
	return context.WithValue(ctx, modelTierContextKey{}, tier)
}
func ModelTierFromContext(ctx context.Context) ModelTier {
	tier, _ := ctx.Value(modelTierContextKey{}).(ModelTier)
	return tier
}

type ModelCallCount struct {
	Stage   string    `json:"stage"`
	Tier    ModelTier `json:"tier"`
	ModelID string    `json:"model_id"`
	Calls   int       `json:"calls"`
}

// ModelCallCollector counts attempted provider calls, including failures.
type ModelCallCollector struct {
	mu     sync.Mutex
	counts map[string]ModelCallCount
}

func WithModelCallCollector(ctx context.Context) (context.Context, *ModelCallCollector) {
	c := &ModelCallCollector{counts: map[string]ModelCallCount{}}
	return context.WithValue(ctx, modelCallsContextKey{}, c), c
}
func RecordModelCall(ctx context.Context, modelID string) {
	c, _ := ctx.Value(modelCallsContextKey{}).(*ModelCallCollector)
	if c == nil {
		return
	}
	stage, _ := LLMCallMetadataFromContext(ctx)
	tier := ModelTierFromContext(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	key := string(tier) + "\x00" + stage + "\x00" + modelID
	v := c.counts[key]
	v.Stage = stage
	v.Tier = tier
	v.ModelID = modelID
	v.Calls++
	c.counts[key] = v
}

func ModelCallCountsFromContext(ctx context.Context) []ModelCallCount {
	c, _ := ctx.Value(modelCallsContextKey{}).(*ModelCallCollector)
	if c == nil {
		return nil
	}
	return c.Snapshot()
}
func (c *ModelCallCollector) Snapshot() []ModelCallCount {
	if c == nil {
		return []ModelCallCount{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]ModelCallCount, 0, len(c.counts))
	for _, v := range c.counts {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Tier != result[j].Tier {
			return result[i].Tier < result[j].Tier
		}
		return result[i].Stage < result[j].Stage
	})
	return result
}
