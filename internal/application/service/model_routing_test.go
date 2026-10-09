package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/chat"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func TestModelTierFallbackAndExplicitGeneration(t *testing.T) {
	db := pgtest.Open(t)
	ctx := domainTestContext()
	global := types.Tenant{ID: 1, Name: "Space", ModelTiers: types.ModelTierConfig{Light: "global-light", Extraction: "global-extract", Generation: "global-generate", Embedding: "global-embedding", Rerank: "global-rerank"}}
	require.NoError(t, db.Create(&global).Error)
	domain, err := NewKnowledgeDomainService(db).Create(ctx, types.KnowledgeDomain{Name: "Research"})
	require.NoError(t, err)
	require.NoError(t, db.Model(domain).Update("model_tiers", types.ModelTierConfig{Light: "domain-light", Extraction: "domain-extract"}).Error)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, DomainID: domain.ID, SummaryModelID: "legacy", EmbeddingModelID: "legacy-embedding", ModelTiers: types.ModelTierConfig{Light: "kb-light"}}
	for _, test := range []struct {
		tier             types.ModelTier
		expected, source string
	}{
		{types.ModelTierLight, "kb-light", "knowledge_base"}, {types.ModelTierExtraction, "domain-extract", "domain"}, {types.ModelTierGeneration, "global-generate", "global"}, {types.ModelTierEmbedding, "global-embedding", "global"}, {types.ModelTierRerank, "global-rerank", "global"},
	} {
		resolved, err := resolveModelTier(ctx, db, kb, test.tier)
		require.NoError(t, err)
		require.Equal(t, test.expected, resolved.ModelID)
		require.Equal(t, test.source, resolved.Source)
	}
	id, err := ResolveGenerationModel(ctx, db, kb, "session-choice")
	require.NoError(t, err)
	require.Equal(t, "session-choice", id)
	id, err = ResolveGenerationModel(ctx, db, kb, "")
	require.NoError(t, err)
	require.Equal(t, "global-generate", id)
	kb.ModelTiers.Light = ""
	id, err = ResolveModelTier(ctx, db, kb, types.ModelTierLight)
	require.NoError(t, err)
	require.Equal(t, "domain-light", id)
	require.NoError(t, db.Model(domain).Update("model_tiers", types.ModelTierConfig{}).Error)
	id, err = ResolveModelTier(ctx, db, kb, types.ModelTierLight)
	require.NoError(t, err)
	require.Equal(t, "global-light", id)
	require.NoError(t, db.Model(&types.Tenant{}).Where("id = 1").Update("model_tiers", types.ModelTierConfig{}).Error)
	id, err = ResolveModelTier(ctx, db, kb, types.ModelTierLight)
	require.NoError(t, err)
	require.Equal(t, "legacy", id)
	kb.DomainID = "missing-domain"
	_, err = ResolveModelTier(ctx, db, kb, types.ModelTierLight)
	require.Error(t, err, "configuration read failures must be visible")
}

type tierTestModels struct {
	interfaces.ModelService
	chat     chat.Chat
	selected string
}

func (m *tierTestModels) GetChatModel(_ context.Context, id string) (chat.Chat, error) {
	m.selected = id
	return m.chat, nil
}

type tierTestChat struct {
	calls int
	err   error
	tier  types.ModelTier
}

func (m *tierTestChat) Chat(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	m.calls++
	m.tier = types.ModelTierFromContext(ctx)
	return &types.ChatResponse{Content: `{"approved":true,"reason":"supported","natures":["描述"],"referents":[{"kind":"object","name":"Acme"}]}`}, m.err
}
func (m *tierTestChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("unused")
}
func (m *tierTestChat) GetModelName() string { return "test" }
func (m *tierTestChat) GetModelID() string   { return "tier-test" }

func TestTierChatCountsAttemptAndAnnotatesCalls(t *testing.T) {
	ctx, calls := types.WithModelCallCollector(context.Background())
	model := &tierTestChat{err: errors.New("provider failed")}
	models := &tierTestModels{chat: model}
	resolved, err := GetTierChatModel(ctx, nil, models, &types.KnowledgeBase{ModelTiers: types.ModelTierConfig{Light: "chosen-light"}}, types.ModelTierLight)
	require.NoError(t, err)
	_, err = resolved.Chat(types.WithLLMCallMetadata(ctx, "entity_resolution", ""), nil, nil)
	require.Error(t, err)
	require.Equal(t, "chosen-light", models.selected)
	require.Equal(t, types.ModelTierLight, model.tier)
	require.Equal(t, []types.ModelCallCount{{Stage: "entity_resolution", Tier: types.ModelTierLight, ModelID: "chosen-light", Calls: 1}}, calls.Snapshot())
}

func TestIntakeReviewClassifiesLabelsInOneCall(t *testing.T) {
	model := &tierTestChat{}
	models := &tierTestModels{chat: model}
	message := &types.Knowledge{ID: "message", TenantID: 1, KnowledgeBaseID: "kb", Channel: "manual"}
	require.NoError(t, message.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "pending", SourceInfo: types.SourceInfo{Publisher: "Acme", OriginalURL: "https://example.org/source", PublishedAt: "2026-10-01"}}))
	repo := &intakeRepo{knowledge: message}
	svc := &knowledgeService{repo: repo, modelService: models}
	rejected, err := svc.reviewIntake(context.Background(), &types.KnowledgeBase{ID: "kb", TenantID: 1, ModelTiers: types.ModelTierConfig{Light: "chosen-light"}}, message, "Acme released a report.")
	require.NoError(t, err)
	require.False(t, rejected)
	require.Equal(t, 1, model.calls)
	intake, err := message.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
	require.Equal(t, []types.KnowledgeIntakeTag{{Kind: "nature", Name: "描述"}, {Kind: "object", Name: "Acme"}}, intake.Classification)
	require.Equal(t, 1, intake.ModelCalls[0].Calls)
	require.Equal(t, types.ModelTierLight, intake.ModelCalls[0].Tier)
}

func TestModelTierReferencesProtectModelDeletion(t *testing.T) {
	db := pgtest.Open(t)
	ctx := domainTestContext()
	global := &types.Tenant{ID: 1, Name: "Space", ModelTiers: types.ModelTierConfig{Light: "bound-model"}}
	require.NoError(t, db.Create(global).Error)
	domain, err := NewKnowledgeDomainService(db).Create(ctx, types.KnowledgeDomain{Name: "Research"})
	require.NoError(t, err)
	require.NoError(t, db.Model(domain).Update("model_tiers", types.ModelTierConfig{Extraction: "bound-model"}).Error)
	kb := &types.KnowledgeBase{ID: "tier-bound-kb", TenantID: 1, DomainID: domain.ID, Name: "Research KB", Type: types.KnowledgeBaseTypeDocument, ModelTiers: types.ModelTierConfig{Generation: "bound-model"}}
	kb.EnsureDefaults()
	require.NoError(t, db.Create(kb).Error)
	svc := &modelService{db: db, kbRepo: repository.NewKnowledgeBaseRepository(db), agentRepo: &stubAgentRepoForModelDelete{}, tenantService: &stubTenantServiceForModelDelete{tenant: global}}
	usage, err := svc.getModelUsageDetails(ctx, 1, "bound-model")
	require.NoError(t, err)
	require.True(t, usage.InUse())
	require.Equal(t, int64(1), usage.KnowledgeBaseTotal)
	require.Equal(t, []types.ModelUsageBinding{"generation_model_tier"}, usage.KnowledgeBases[0].Bindings)
	require.Len(t, usage.ModelTiers, 2)
	require.Equal(t, "domain", usage.ModelTiers[0].Scope)
	require.Equal(t, "global", usage.ModelTiers[1].Scope)
}
