package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	filesvc "reliquary/internal/application/service/file"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/models/embedding"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

type personalStorageRetrieveEngine struct{ parentChildRetrieveEngine }

func (*personalStorageRetrieveEngine) EstimateStorageSize(context.Context, embedding.Embedder, []*types.IndexInfo, []types.RetrieverType) int64 {
	return 256
}

func TestPersonalIntakeBeyondFormerStorageLimitKeepsUsageAccounting(t *testing.T) {
	db := pgtest.Open(t)
	const initialUsed = int64(11 * 1024 * 1024 * 1024)
	tenant := &types.Tenant{ID: types.SingleTenantID, Name: "Personal space", StorageUsed: initialUsed}
	require.NoError(t, db.Create(tenant).Error)
	require.False(t, db.Migrator().HasColumn(&types.Tenant{}, "storage_quota"))
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenant.ID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	domain := BuiltinAIKnowledgeDomain()
	require.NoError(t, db.Create(domain).Error)
	kb := &types.KnowledgeBase{
		ID: "personal-storage-kb", TenantID: tenant.ID, Name: "Personal library", DomainID: domain.ID, Type: types.KnowledgeBaseTypeDocument,
		EmbeddingModelID: "embedding-1", IndexingStrategy: types.IndexingStrategy{VectorEnabled: true},
	}
	require.NoError(t, db.Create(kb).Error)
	repo := repository.NewKnowledgeRepository(db)
	tenantRepo := repository.NewTenantRepository(db)
	models := parentChildModelService{embedder: parentChildEmbedder{}}
	engine := &personalStorageRetrieveEngine{}
	retrieval := &retriever.PostgresRetrievalService{RetrieveEngineService: engine}
	queue := &createKnowledgeTaskEnqueuerStub{}
	chunks := NewChunkService(repository.NewChunkRepository(db), repo, repository.NewKnowledgeBaseRepository(db), models, retrieval, queue, nil)
	svc := &knowledgeService{
		db: db, repo: repo, tenantRepo: tenantRepo, kbService: &createKnowledgeFileKBServiceStub{kb: kb},
		fileSvc: filesvc.NewLocalFileService(t.TempDir(), ""), task: queue, chunkService: chunks, modelService: models, retrieveEngine: retrieval,
	}
	// Use a deterministic allowed origin so this intake regression does not depend on DNS.
	t.Setenv("SSRF_WHITELIST", "storage-test.example")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	creators := []struct {
		name   string
		create func() (*types.Knowledge, error)
	}{
		{"upload", func() (*types.Knowledge, error) {
			return svc.CreateKnowledgeFromFile(ctx, kb.ID, newMultipartFileHeader(t, "upload.txt", "uploaded content"), nil, nil, "", nil, types.ChannelWeb, nil)
		}},
		{"website", func() (*types.Knowledge, error) {
			return svc.CreateKnowledgeFromURL(ctx, kb.ID, "https://storage-test.example/article", "", "", nil, "Website content", nil, types.ChannelWeb, nil)
		}},
		{"file URL", func() (*types.Knowledge, error) {
			return svc.CreateKnowledgeFromURL(ctx, kb.ID, "https://storage-test.example/document.pdf", "", "", nil, "Downloaded content", nil, types.ChannelWeb, nil)
		}},
		{"manual", func() (*types.Knowledge, error) {
			return svc.CreateKnowledgeFromManual(ctx, kb.ID, &types.ManualKnowledgePayload{Title: "Manual content", Content: "Personal manually created body", Status: types.ManualKnowledgeStatusPublish}, types.ChannelWeb)
		}},
	}
	var created []*types.Knowledge
	for _, creator := range creators {
		t.Run(creator.name, func(t *testing.T) {
			item, err := creator.create()
			require.NoError(t, err)
			require.NotNil(t, item)
			item.ParseStatus = types.ParseStatusProcessing
			require.NoError(t, repo.UpdateKnowledge(ctx, item))
			svc.processChunks(ctx, kb, item, []types.ParsedChunk{{Content: creator.name + " body", ParentIndex: -1}})
			stored, err := repo.GetKnowledgeByID(ctx, tenant.ID, item.ID)
			require.NoError(t, err)
			require.NotEqual(t, types.ParseStatusFailed, stored.ParseStatus, stored.ErrorMessage)
			require.EqualValues(t, 256, stored.StorageSize)
			created = append(created, stored)
			current, err := tenantRepo.GetTenantByID(ctx, tenant.ID)
			require.NoError(t, err)
			require.Equal(t, initialUsed+int64(len(created))*256, current.StorageUsed)
			require.Equal(t, current.StorageUsed, tenant.StorageUsed)
		})
	}
	require.Len(t, created, len(creators))
	require.Len(t, engine.indexed, 1)
	for i, item := range created {
		require.NoError(t, svc.DeleteKnowledge(ctx, item.ID))
		current, err := tenantRepo.GetTenantByID(ctx, tenant.ID)
		require.NoError(t, err)
		require.Equal(t, initialUsed+int64(len(created)-i-1)*256, current.StorageUsed)
		require.Equal(t, current.StorageUsed, tenant.StorageUsed)
	}
}
