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

func TestModelTiersUseOnlyKnowledgeBaseSettings(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, Name: "Knowledge Base", SummaryModelID: "summary", EmbeddingModelID: "embedding", ModelTiers: types.ModelTierConfig{Light: "chosen-light", EntityResolution: "chosen-entity", Generation: "chosen-generation", Embedding: "chosen-embedding", Rerank: "chosen-rerank"}}
	require.NoError(t, db.Create(kb).Error)
	for _, tier := range types.ModelTiers {
		resolved, err := resolveModelTier(ctx, db, nil, tier)
		require.NoError(t, err)
		require.Equal(t, kb.ModelTiers.ModelID(tier), resolved.ModelID)
		require.Equal(t, "knowledge_base", resolved.Source)
	}
	kb.ModelTiers = types.ModelTierConfig{}
	require.NoError(t, db.Save(kb).Error)
	id, err := ResolveModelTier(ctx, db, nil, types.ModelTierLight)
	require.NoError(t, err)
	require.Equal(t, "summary", id)
	require.NoError(t, db.Delete(kb).Error)
	_, err = ResolveModelTier(ctx, db, nil, types.ModelTierLight)
	require.Error(t, err, "missing knowledge base must remain visible")
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
	return &types.ChatResponse{Content: `{"approved":true,"reason":"supported"}`}, m.err
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

func TestIntakeReviewUsesLightModelInOneCall(t *testing.T) {
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
	require.Equal(t, 1, intake.ModelCalls[0].Calls)
	require.Equal(t, types.ModelTierLight, intake.ModelCalls[0].Tier)
}

func TestModelTierReferencesProtectModelDeletion(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	global := &types.Tenant{ID: 1, Name: "Space"}
	require.NoError(t, db.Create(global).Error)
	kb := &types.KnowledgeBase{ID: "tier-bound-kb", TenantID: 1, Name: "Research KB", Type: types.KnowledgeBaseTypeDocument, ModelTiers: types.ModelTierConfig{Generation: "bound-model"}}
	kb.EnsureDefaults()
	require.NoError(t, db.Create(kb).Error)
	svc := &modelService{db: db, kbRepo: repository.NewKnowledgeBaseRepository(db), tenantService: &stubTenantServiceForModelDelete{tenant: global}}
	usage, err := svc.getModelUsageDetails(ctx, 1, "bound-model")
	require.NoError(t, err)
	require.True(t, usage.InUse())
	require.Equal(t, int64(1), usage.KnowledgeBaseTotal)
	require.Equal(t, []types.ModelUsageBinding{"generation_model_tier"}, usage.KnowledgeBases[0].Bindings)
}
