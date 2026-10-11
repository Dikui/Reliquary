package service

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type ResolvedModelTier struct {
	ModelID string `json:"model_id"`
	Source  string `json:"source"`
}

func ValidateModelTierConfig(ctx context.Context, db *gorm.DB, config types.ModelTierConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if db == nil {
		return nil
	}
	for _, tier := range types.ModelTiers {
		id := config.ModelID(tier)
		if id == "" {
			continue
		}
		var model types.Model
		if err := db.WithContext(ctx).Where("id = ? AND (space_id = ? OR is_builtin = true)", id, types.MustSpaceIDFromContext(ctx)).First(&model).Error; err != nil {
			return fmt.Errorf("%s model is unavailable: %w", tier, err)
		}
		expected := types.ModelTypeKnowledgeQA
		if tier == types.ModelTierEmbedding {
			expected = types.ModelTypeEmbedding
		}
		if tier == types.ModelTierRerank {
			expected = types.ModelTypeRerank
		}
		if model.Type != expected || model.Status != types.ModelStatusActive {
			return fmt.Errorf("%s tier requires an active %s model", tier, expected)
		}
	}
	return nil
}

// ResolveModelTier reads the model choices stored on the only knowledge base.
func ResolveModelTier(ctx context.Context, db *gorm.DB, kb *types.KnowledgeBase, tier types.ModelTier) (string, error) {
	resolved, err := resolveModelTier(ctx, db, kb, tier)
	return resolved.ModelID, err
}

func resolveModelTier(ctx context.Context, db *gorm.DB, kb *types.KnowledgeBase, tier types.ModelTier) (ResolvedModelTier, error) {
	valid := false
	for _, candidate := range types.ModelTiers {
		if candidate == tier {
			valid = true
		}
	}
	if !valid {
		return ResolvedModelTier{}, fmt.Errorf("unknown model tier %q", tier)
	}
	if kb == nil && db != nil {
		spaceID := types.MustSpaceIDFromContext(ctx)
		kb = &types.KnowledgeBase{}
		if err := db.WithContext(ctx).Where("space_id = ?", spaceID).First(kb).Error; err != nil {
			return ResolvedModelTier{}, fmt.Errorf("read knowledge base model tiers: %w", err)
		}
	}
	if kb != nil {
		if id := kb.ModelTiers.ModelID(tier); id != "" {
			return ResolvedModelTier{id, "knowledge_base"}, nil
		}
	}
	if kb != nil {
		id := ""
		switch tier {
		case types.ModelTierLight:
			if strings.TrimSpace(id) == "" {
				id = kb.SummaryModelID
			}
		case types.ModelTierEntityResolution, types.ModelTierGeneration:
			id = kb.SummaryModelID
		case types.ModelTierEmbedding:
			id = kb.EmbeddingModelID
		}
		if id = strings.TrimSpace(id); id != "" {
			return ResolvedModelTier{id, "knowledge_base"}, nil
		}
	}
	return ResolvedModelTier{Source: "unconfigured"}, nil
}

func GetTierChatModel(ctx context.Context, db *gorm.DB, models interfaces.ModelService, kb *types.KnowledgeBase, tier types.ModelTier) (chat.Chat, error) {
	id, err := ResolveModelTier(ctx, db, kb, tier)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, fmt.Errorf("%s model is not configured", tier)
	}
	model, err := models.GetChatModel(types.WithModelTier(ctx, tier), id)
	if err != nil {
		return nil, err
	}
	return &tierChat{inner: model, tier: tier, modelID: id}, nil
}

type tierChat struct {
	inner   chat.Chat
	tier    types.ModelTier
	modelID string
}

func (m *tierChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	ctx = types.WithModelTier(ctx, m.tier)
	types.RecordModelCall(ctx, m.GetModelID())
	return m.inner.Chat(ctx, messages, opts)
}
func (m *tierChat) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	ctx = types.WithModelTier(ctx, m.tier)
	types.RecordModelCall(ctx, m.GetModelID())
	return m.inner.ChatStream(ctx, messages, opts)
}

func (m *tierChat) GetModelName() string { return m.inner.GetModelName() }
func (m *tierChat) GetModelID() string   { return m.modelID }

func GetTierEmbeddingModel(ctx context.Context, db *gorm.DB, models interfaces.ModelService, kb *types.KnowledgeBase) (embedding.Embedder, error) {
	id, err := ResolveModelTier(ctx, db, kb, types.ModelTierEmbedding)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, fmt.Errorf("embedding model is not configured")
	}
	model, err := models.GetEmbeddingModel(types.WithModelTier(ctx, types.ModelTierEmbedding), id)
	if err != nil {
		return nil, err
	}
	return &tierEmbedder{inner: model, modelID: id}, nil
}

type tierEmbedder struct {
	inner   embedding.Embedder
	modelID string
}

func (m *tierEmbedder) GetProfileHash() string {
	if model, ok := m.inner.(embedding.ProfiledEmbedder); ok {
		return model.GetProfileHash()
	}
	return ""
}

func (m *tierEmbedder) SupportsImages() bool {
	images, ok := m.inner.(embedding.ImageEmbedder)
	return ok && images.SupportsImages()
}

func (m *tierEmbedder) EmbedImage(ctx context.Context, image string) ([]float32, error) {
	images, ok := m.inner.(embedding.ImageEmbedder)
	if !ok || !images.SupportsImages() {
		return nil, fmt.Errorf("embedding model does not support images")
	}
	ctx = types.WithModelTier(ctx, types.ModelTierEmbedding)
	types.RecordModelCall(ctx, m.GetModelID())
	return images.EmbedImage(ctx, image)
}

func (m *tierEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	ctx = types.WithModelTier(ctx, types.ModelTierEmbedding)
	types.RecordModelCall(ctx, m.GetModelID())
	return m.inner.Embed(ctx, text)
}
func (m *tierEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	ctx = types.WithModelTier(ctx, types.ModelTierEmbedding)
	types.RecordModelCall(ctx, m.GetModelID())
	return m.inner.BatchEmbed(ctx, texts)
}
func (m *tierEmbedder) BatchEmbedWithPool(ctx context.Context, model embedding.Embedder, texts []string) ([][]float32, error) {
	ctx = types.WithModelTier(ctx, types.ModelTierEmbedding)
	return m.inner.BatchEmbedWithPool(ctx, m, texts)
}
func (m *tierEmbedder) GetModelID() string   { return m.modelID }
func (m *tierEmbedder) GetModelName() string { return m.inner.GetModelName() }
func (m *tierEmbedder) GetDimensions() int   { return m.inner.GetDimensions() }

func modelRoutingDB(reader any) *gorm.DB {
	if provider, ok := reader.(interface{ ModelRoutingDB() *gorm.DB }); ok {
		return provider.ModelRoutingDB()
	}
	return nil
}
