package service

import (
	"context"

	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"

	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

type singletonIngestionModels struct{ *summaryStateModelService }

func (singletonIngestionModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return chunkSearchEmbedder{}, nil
}

func singletonIntakeSettings(t *testing.T, h *retainedIntakeHarness) *knowledgeBaseService {
	t.Helper()
	models := singletonIngestionModels{h.service.modelService.(*summaryStateModelService)}
	h.service.modelService = models
	indices := pgretriever.NewPostgresRetrieveEngineRepository(h.db)
	h.service.retrieveEngine = retriever.NewPostgresRetrievalService(indices)
	chunks := h.service.chunkService.(*chunkService)
	chunks.entryRepo = h.service.repo
	chunks.kbRepository = repository.NewKnowledgeBaseRepository(h.db)
	chunks.modelService = models
	chunks.retrieveEngine = h.service.retrieveEngine
	require.NoError(t, h.db.Create(&types.Model{ID: "saved-embedding", SpaceID: h.kb.SpaceID, Name: "Fixture embedding", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{EmbeddingParameters: types.EmbeddingParameters{Dimension: 3}}}).Error)
	require.NoError(t, h.db.Model(h.kb).Update("embedding_model_id", "saved-embedding").Error)
	bases := &knowledgeBaseService{db: h.db, repo: repository.NewKnowledgeBaseRepository(h.db), entryRepo: h.service.repo}
	h.service.kbService = bases
	return bases
}

func TestSavedKnowledgeBaseConfigurationControlsUploadedMarkdownChunks(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	bases := singletonIntakeSettings(t, h)
	require.NoError(t, h.db.Create(&types.Model{ID: "saved-light", SpaceID: h.kb.SpaceID, Name: "Fixture", Type: types.ModelTypeGeneration, Source: types.ModelSourceRemote, Status: types.ModelStatusActive}).Error)
	config := &types.KnowledgeBaseConfig{ModelTiers: &types.ModelTierConfig{Light: "saved-light", Generation: "saved-light", Embedding: "saved-embedding"}, ChunkingConfig: &types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 0, ChunkOverlapSet: true, Strategy: "recursive", Separators: []string{"\n\n", "\n", " "}}}
	_, err := bases.UpdateKnowledgeBase(h.ctx, h.kb.ID, h.kb.Name, "", config)
	require.NoError(t, err)
	persisted, err := bases.GetKnowledgeBase(h.ctx)
	require.NoError(t, err)
	require.Equal(t, "saved-light", persisted.ModelTiers.Light)
	require.Equal(t, "saved-light", persisted.SummaryModelID)
	require.Equal(t, "saved-embedding", persisted.EmbeddingModelID)
	selected, err := ResolveModelTier(h.ctx, h.db, nil, types.ModelTierLight)
	require.NoError(t, err)
	require.Equal(t, "saved-light", selected)
	ingest := func(marker string) []*types.Chunk {
		text := "Acme published original measurements about widget deployment and supported capabilities. "
		if marker == "Smallfixture" {
			text = "Researchers published original experimental measurements on telescope optics, detector sensitivity and observatory calibration. "
		}
		body := "# " + marker + "\n\n" + strings.Repeat(text, 28)
		article, err := h.service.createLegacyFileFixture(h.ctx, h.kb.ID, newMultipartFileHeader(t, marker+".md", body), map[string]string{"publisher": "Acme", "original_url": "https://example.org/" + marker, "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
		require.NoError(t, err)
		h.drain(t)
		require.Equal(t, types.ParseStatusCompleted, h.article(t, article.ID).ParseStatus)
		chunks, err := h.service.chunkRepo.ListChunksByEntryIDAndTypes(h.ctx, h.kb.SpaceID, article.ID, []types.ChunkType{types.ChunkTypeText})
		require.NoError(t, err)
		return chunks
	}
	large := ingest("Largefixture")
	config.ChunkingConfig.ChunkSize = 128
	_, err = bases.UpdateKnowledgeBase(h.ctx, h.kb.ID, h.kb.Name, "", config)
	require.NoError(t, err)
	small := ingest("Smallfixture")
	require.Greater(t, len(small), len(large), "saved chunk size must affect the actual ingestion pipeline")
	for _, chunk := range small {
		require.LessOrEqual(t, len([]rune(chunk.Content)), 128)
		require.Equal(t, h.kb.ID, chunk.KnowledgeBaseID)
	}
	hits, err := h.service.retrieveEngine.Retrieve(h.ctx, []types.RetrieveParams{{RetrieverType: types.VectorRetrieverType, KnowledgeBaseIDs: []string{h.kb.ID}, Embedding: []float32{1, 0, 0}, TopK: 10, Threshold: 0.9}})
	require.NoError(t, err)
	require.NotEmpty(t, hits, "configured model must produce searchable vectors")
	for _, group := range hits {
		require.NoError(t, group.Error)
		require.NotEmpty(t, group.Results)
		for _, hit := range group.Results {
			require.Equal(t, h.kb.ID, hit.KnowledgeBaseID)
		}
	}
	// Saving model choices alone must preserve the ingestion settings.
	_, err = bases.UpdateKnowledgeBase(h.ctx, h.kb.ID, h.kb.Name, "", &types.KnowledgeBaseConfig{ModelTiers: config.ModelTiers})
	require.NoError(t, err)
	persisted, err = bases.GetKnowledgeBase(h.ctx)
	require.NoError(t, err)
	require.Equal(t, 128, persisted.ChunkingConfig.ChunkSize)
	require.Zero(t, persisted.ChunkingConfig.ChunkOverlap)
	_, err = bases.UpdateKnowledgeBase(h.ctx, h.kb.ID, h.kb.Name, "", &types.KnowledgeBaseConfig{ModelTiers: &types.ModelTierConfig{}})
	require.NoError(t, err)
	cleared, err := bases.GetKnowledgeBase(h.ctx)
	require.NoError(t, err)
	require.Empty(t, cleared.SummaryModelID)
	require.Empty(t, cleared.EmbeddingModelID)
}
