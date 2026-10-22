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

func TestModelTiersUseSpaceEmbeddingAndLibraryProcessingSettings(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	library := &types.Library{ID: "library", SpaceID: 1, SystemRole: "official", Template: types.TemplateExternalTimely, Directory: "official", SourceEvidenceRule: "link_required", Name: "Library", SummaryModelID: "summary", EmbeddingModelID: "embedding", ModelTiers: types.ModelTierConfig{Light: "chosen-light", EntityResolution: "chosen-entity", Generation: "chosen-generation", Embedding: "chosen-embedding", Rerank: "chosen-rerank"}}
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Space", EmbeddingModelID: "chosen-embedding"}).Error)
	require.NoError(t, db.Create(library).Error)
	for _, tier := range types.ModelTiers {
		resolved, err := resolveModelTier(ctx, db, nil, tier)
		require.NoError(t, err)
		require.Equal(t, library.ModelTiers.ModelID(tier), resolved.ModelID)
		if tier == types.ModelTierEmbedding {
			require.Equal(t, "space", resolved.Source)
		} else {
			require.Equal(t, "library", resolved.Source)
		}
	}
	library.ModelTiers = types.ModelTierConfig{}
	require.NoError(t, db.Save(library).Error)
	id, err := ResolveModelTier(ctx, db, nil, types.ModelTierLight)
	require.NoError(t, err)
	require.Equal(t, "summary", id)
	media := types.SystemLibraryDefaults()[1]
	media.ID, media.SpaceID = "independent-media", 1
	require.NoError(t, db.Create(media).Error)
	resolved, err := resolveModelTier(ctx, db, media, types.ModelTierLight)
	require.NoError(t, err)
	require.Empty(t, resolved.ModelID, "an unconfigured library must not borrow official's model")
	media.ModelTiers.Generation = "media-generation"
	resolved, err = resolveModelTier(ctx, db, media, types.ModelTierLight)
	require.NoError(t, err)
	require.Equal(t, ResolvedModelTier{ModelID: "media-generation", Source: "generation"}, resolved)
	require.NoError(t, db.Model(library).Update("summary_model_id", "changed-official").Error)
	id, err = ResolveModelTier(ctx, db, media, types.ModelTierLight)
	require.NoError(t, err)
	require.Equal(t, "media-generation", id)
	require.NoError(t, db.Unscoped().Delete(library).Error)
	_, err = ResolveModelTier(ctx, db, nil, types.ModelTierLight)
	require.Error(t, err, "missing library must remain visible")
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
	resolved, err := GetTierChatModel(ctx, nil, models, &types.Library{ModelTiers: types.ModelTierConfig{Light: "chosen-light"}}, types.ModelTierLight)
	require.NoError(t, err)
	_, err = resolved.Chat(types.WithLLMCallMetadata(ctx, "entity_resolution", ""), nil, nil)
	require.Error(t, err)
	require.Equal(t, "chosen-light", models.selected)
	require.Equal(t, types.ModelTierLight, model.tier)
	require.Equal(t, []types.ModelCallCount{{Stage: "entity_resolution", Tier: types.ModelTierLight, ModelID: "chosen-light", Calls: 1}}, calls.Snapshot())
}

func TestIntakeApprovalDoesNotUseLightModel(t *testing.T) {
	model := &tierTestChat{}
	models := &tierTestModels{chat: model}
	message := &types.Entry{ID: "message", SpaceID: 1, LibraryID: "library", Channel: "manual"}
	require.NoError(t, message.SetIntakeMetadata(&types.EntryIntakeMetadata{Decision: "pending", SourceInfo: types.SourceInfo{Publisher: "Acme", OriginalURL: "https://example.org/source", PublishedAt: "2026-10-01"}}))
	repo := &intakeRepo{entry: message}
	svc := &entryService{repo: repo, modelService: models}
	rejected, err := svc.reviewIntake(context.Background(), &types.Library{ID: "library", SpaceID: 1, ModelTiers: types.ModelTierConfig{Light: "chosen-light"}}, message, "Acme released a report.")
	require.NoError(t, err)
	require.False(t, rejected)
	require.Zero(t, model.calls)
	intake, err := message.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
	require.Empty(t, intake.ModelCalls)
}

func TestModelTierReferencesProtectModelDeletion(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	global := &types.Space{ID: 1, Name: "Space"}
	require.NoError(t, db.Create(global).Error)
	library := &types.Library{ID: "tier-bound-library", SpaceID: 1, Name: "Research Library", Type: types.LibraryTypeDocument, ModelTiers: types.ModelTierConfig{Generation: "bound-model"}}
	library.EnsureDefaults()
	require.NoError(t, db.Create(library).Error)
	svc := &modelService{db: db, libraryRepo: repository.NewLibraryRepository(db), spaceService: &stubSpaceServiceForModelDelete{space: global}}
	usage, err := svc.getModelUsageDetails(ctx, 1, "bound-model")
	require.NoError(t, err)
	require.True(t, usage.InUse())
	require.Equal(t, int64(1), usage.LibraryTotal)
	require.Equal(t, []types.ModelUsageBinding{"generation_model_tier"}, usage.Libraries[0].Bindings)
}

func (m *tierTestModels) GetModelByID(_ context.Context, id string) (*types.Model, error) {
	return &types.Model{ID: id, Type: types.ModelTypeGeneration}, nil
}
