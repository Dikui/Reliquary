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
type ModelRoutingView struct {
	Overrides types.ModelTierConfig                 `json:"overrides"`
	Resolved  map[types.ModelTier]ResolvedModelTier `json:"resolved"`
}

func ModelRoutingForScope(ctx context.Context, db *gorm.DB, kb *types.KnowledgeBase, overrides types.ModelTierConfig) (ModelRoutingView, error) {
	view := ModelRoutingView{Overrides: overrides, Resolved: map[types.ModelTier]ResolvedModelTier{}}
	for _, tier := range types.ModelTiers {
		resolved, err := resolveModelTier(ctx, db, kb, tier)
		if err != nil {
			return view, err
		}
		view.Resolved[tier] = resolved
	}
	return view, nil
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
		if err := db.WithContext(ctx).Where("id = ? AND (tenant_id = ? OR is_builtin = true)", id, types.MustTenantIDFromContext(ctx)).First(&model).Error; err != nil {
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

// ResolveModelTier resolves a KB override, then global default.
// Legacy model fields apply only after both scopes are unconfigured.
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
	tenantID, _ := types.TenantIDFromContext(ctx)
	if tenantID == 0 {
		tenantID = 1
	}
	if kb != nil {
		if kb.TenantID != 0 {
			tenantID = kb.TenantID
		}
		if id := kb.ModelTiers.ModelID(tier); id != "" {
			return ResolvedModelTier{id, "knowledge_base"}, nil
		}

	}
	if db != nil {
		var tenant types.Tenant
		if err := db.WithContext(ctx).Select("model_tiers").Where("id = ?", tenantID).First(&tenant).Error; err != nil {
			return ResolvedModelTier{}, fmt.Errorf("read global default model tiers: %w", err)
		}
		if id := tenant.ModelTiers.ModelID(tier); id != "" {
			return ResolvedModelTier{id, "global"}, nil
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
			return ResolvedModelTier{id, "legacy"}, nil
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
