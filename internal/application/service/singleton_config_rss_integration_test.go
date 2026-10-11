package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/datasource"
	"reliquary/internal/datasource/connector/rss"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/utils"
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
	chunks.knowledgeRepo = h.service.repo
	chunks.kbRepository = repository.NewKnowledgeBaseRepository(h.db)
	chunks.modelService = models
	chunks.retrieveEngine = h.service.retrieveEngine
	require.NoError(t, h.db.Create(&types.Model{ID: "saved-embedding", TenantID: h.kb.TenantID, Name: "Fixture embedding", Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{EmbeddingParameters: types.EmbeddingParameters{Dimension: 3}}}).Error)
	require.NoError(t, h.db.Model(h.kb).Update("embedding_model_id", "saved-embedding").Error)
	bases := &knowledgeBaseService{db: h.db, repo: repository.NewKnowledgeBaseRepository(h.db), kgRepo: h.service.repo}
	h.service.kbService = bases
	return bases
}

func TestSavedKnowledgeBaseConfigurationControlsUploadedMarkdownChunks(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	bases := singletonIntakeSettings(t, h)
	require.NoError(t, h.db.Create(&types.Model{ID: "saved-light", TenantID: h.kb.TenantID, Name: "Fixture", Type: types.ModelTypeKnowledgeQA, Source: types.ModelSourceRemote, Status: types.ModelStatusActive}).Error)
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
		article, err := h.service.CreateKnowledgeFromFile(h.ctx, h.kb.ID, newMultipartFileHeader(t, marker+".md", body), map[string]string{"publisher": "Acme", "original_url": "https://example.org/" + marker, "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
		require.NoError(t, err)
		h.drain(t)
		require.Equal(t, types.ParseStatusCompleted, h.article(t, article.ID).ParseStatus)
		chunks, err := h.service.chunkRepo.ListChunksByKnowledgeIDAndTypes(h.ctx, h.kb.TenantID, article.ID, []types.ChunkType{types.ChunkTypeText})
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

func TestGlobalRSSSourceAutomaticallyWritesToKnowledgeBaseAndKeepsSyncLogs(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	h := newRetainedIntakeHarness(t)
	bases := singletonIntakeSettings(t, h)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed.xml" {
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Fixture publisher</title><link>http://%s</link><description>Original publications</description><item><title>Globalrssfixture</title><link>http://%s/article</link><guid>global-article</guid><pubDate>%s</pubDate></item></channel></rss>`, r.Host, r.Host, time.Now().Add(time.Minute).UTC().Format(time.RFC1123Z))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Globalrssfixture</title></head><body><article><h1>Globalrssfixture</h1><p>`+strings.Repeat("Acme publishes original measurements documenting widget deployment choices and measured capabilities. ", 12)+`</p></article></body></html>`)
	}))
	defer server.Close()
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(rss.NewConnector()))
	dsRepo, logs := repository.NewDataSourceRepository(h.db), repository.NewSyncLogRepository(h.db)
	syncQueue := &retainedProcessingQueue{}
	scheduler := datasource.NewScheduler(dsRepo, logs, syncQueue)
	t.Cleanup(scheduler.Stop)
	syncer := &DataSourceService{dsRepo: dsRepo, syncLogRepo: logs, knowledgeService: h.service, kbService: bases, taskEnqueuer: syncQueue, connectorRegistry: registry, scheduler: scheduler, tenantRepo: h.service.tenantRepo}
	cfg, err := (&types.DataSourceConfig{Type: types.ConnectorTypeRSS, Settings: map[string]interface{}{"feed_urls": server.URL + "/feed.xml", "publisher": "Fixture publisher"}}).ToJSON()
	require.NoError(t, err)
	created, err := syncer.CreateDataSource(h.ctx, &types.DataSource{ID: "global-feed", TenantID: h.kb.TenantID, Name: "Fixture publisher", Type: types.ConnectorTypeRSS, Config: cfg, Status: types.DataSourceStatusActive, CreatedAt: time.Now().Add(-24 * time.Hour)})
	require.NoError(t, err)
	require.Equal(t, h.kb.ID, created.KnowledgeBaseID, "callers do not choose a destination knowledge base")
	log, err := syncer.ManualSync(h.ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, syncQueue.tasks, 1)
	require.NoError(t, syncer.ProcessSync(h.ctx, syncQueue.tasks[0]))
	h.drain(t)
	var articles []types.Knowledge
	require.NoError(t, h.db.Find(&articles).Error)
	require.Len(t, articles, 1)
	require.Equal(t, h.kb.ID, articles[0].KnowledgeBaseID)
	require.Equal(t, types.ChannelRSS, articles[0].Channel)
	require.Equal(t, types.ParseStatusCompleted, articles[0].ParseStatus)
	history, err := syncer.GetSyncLogs(h.ctx, created.ID, 10, 0)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, log.ID, history[0].ID)
	require.Equal(t, types.SyncLogStatusSuccess, history[0].Status)
	require.Equal(t, 1, history[0].ItemsCreated)
}
