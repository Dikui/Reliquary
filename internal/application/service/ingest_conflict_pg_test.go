package service

import (
	"encoding/base64"
	"math/bits"
	"reliquary/internal/testutil/redistest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service/file"
	"reliquary/internal/types"
)

const ingestConflictBody = "An unrelated novel describes a fictional seafaring kingdom and its coastal villages. The characters navigate merchant fleets, negotiate alliances and debate narrative choices. Its chapters trace the kingdom's cultural history, maritime customs and competing dynastic traditions."

func newIngestConflictHarness(t *testing.T) (*articleProcessingHarness, string, string) {
	t.Helper()
	h, _ := newIngestHarness(t)
	root := t.TempDir()
	h.service.resourceCatalog = NewResourceCatalog(repository.NewResourceRepository(h.db))
	h.service.fileSvc = file.NewResourceCatalogFileService(file.NewLocalFileService(root, ""), h.service.resourceCatalog)
	h.service.task = redistest.NewClient(t, h.db)
	const url = "https://example.org/conflict-retry"
	first, err := h.service.Ingest(h.ctx, ingestMissingImageRequest(url, "https://example.org/original-measurements.png"))
	require.NoError(t, err)
	require.Equal(t, "accepted", first.Status)
	require.True(t, h.article(t, first.EntryID).Incomplete)
	return h, root, first.EntryID
}

func ingestConflictingMaterialRequest(t *testing.T) *types.IngestRequest {
	t.Helper()
	request := ingestFixtureRequest("https://example.org/conflict-retry", ingestConflictBody)
	request.Images = []types.IngestImage{{Data: articleImageDataURI(t, 93), SourceURL: "https://example.org/kingdom.png", Caption: "Coastal kingdom"}}
	request.File = &types.IngestFile{Name: "original.txt", Data: base64.StdEncoding.EncodeToString([]byte(ingestConflictBody))}
	return request
}

func assertIngestConflictCount(t *testing.T, h *articleProcessingHarness, originalID string, count int64) {
	t.Helper()
	var articles int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("related_article_id=? AND parse_status=?", originalID, types.ParseStatusRejected).Count(&articles).Error)
	require.Equal(t, count, articles)
	logs, err := NewOperationLogService(h.db).List(h.ctx, "ingest_conflict", 1, 100)
	require.NoError(t, err)
	require.Equal(t, count, logs.Total)
}

func TestIngestConflictSameBodyRetryKeepsPendingIDAndMaterials(t *testing.T) {
	h, root, originalID := newIngestConflictHarness(t)
	request := ingestConflictingMaterialRequest(t)
	image, err := decodeIngestInline(request.Images[0].Data)
	require.NoError(t, err)
	asset, err := h.service.UploadIngestAsset(h.ctx, "kingdom.png", image)
	require.NoError(t, err)
	request.Images[0].AssetID, request.Images[0].Data = asset.AssetID, ""
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "conflict", first.Status)
	require.Equal(t, originalID, h.article(t, first.EntryID).RelatedArticleID)
	beforeFiles := transactionStoredFiles(t, root)
	var beforeResources int64
	require.NoError(t, h.db.Model(&types.StoredResource{}).Count(&beforeResources).Error)
	beforeJobs := transactionJobs(t, h.db)
	// The existing exact body hash treats whitespace differences as retries.
	request.Content = strings.ReplaceAll(request.Content, " ", "  ")
	request.Title = "Retry title does not replace the first pending article"
	second, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "conflict", second.Status)
	require.Equal(t, first.EntryID, second.EntryID)
	require.Equal(t, beforeFiles, transactionStoredFiles(t, root))
	var afterResources int64
	require.NoError(t, h.db.Model(&types.StoredResource{}).Count(&afterResources).Error)
	require.Equal(t, beforeResources, afterResources)
	assertSamePendingOperations(t, beforeJobs, transactionJobs(t, h.db))
	var referenced types.IngestAsset
	require.NoError(t, h.db.Where("id=?", asset.AssetID).First(&referenced).Error)
	require.Equal(t, first.EntryID, referenced.EntryID)
	assertIngestConflictCount(t, h, originalID, 1)
}

func TestIngestConflictConcurrentSameBodyCreatesOnePending(t *testing.T) {
	h, root, originalID := newIngestConflictHarness(t)
	request := ingestConflictingMaterialRequest(t)
	beforeJobs := transactionJobs(t, h.db)
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(2)
	done.Add(2)
	results := make([]*types.IngestResult, 2)
	errs := make([]error, 2)
	for i := range results {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-start
			results[i], errs[i] = h.service.Ingest(h.ctx, request)
		}(i)
	}
	ready.Wait()
	close(start)
	done.Wait()
	for i := range results {
		require.NoError(t, errs[i])
		require.Equal(t, "conflict", results[i].Status)
	}
	require.Equal(t, results[0].EntryID, results[1].EntryID)
	assertIngestConflictCount(t, h, originalID, 1)
	assertSamePendingOperations(t, beforeJobs, transactionJobs(t, h.db))
	var resources int64
	require.NoError(t, h.db.Model(&types.StoredResource{}).Count(&resources).Error)
	require.EqualValues(t, 4, resources, "the original body and one conflict's file, image and body remain")
	require.Len(t, transactionStoredFiles(t, root), 4)
}

func TestIngestConflictDifferentBodyHashesCreateSeparatePending(t *testing.T) {
	h, _, originalID := newIngestConflictHarness(t)
	firstRequest := ingestFixtureRequest("https://example.org/conflict-retry", ingestConflictBody)
	secondRequest := ingestFixtureRequest(firstRequest.URL, ingestConflictBody+".")
	firstHash, firstSimHash, _ := messageBodyFingerprint(assembleIngestBody(firstRequest, nil))
	secondHash, secondSimHash, _ := messageBodyFingerprint(assembleIngestBody(secondRequest, nil))
	require.NotEqual(t, firstHash, secondHash)
	require.LessOrEqual(t, bits.OnesCount64(uint64(firstSimHash)^uint64(secondSimHash)), messageSimHashThreshold(), "conflict retries compare exact hashes even when SimHash considers the texts similar")
	first, err := h.service.Ingest(h.ctx, firstRequest)
	require.NoError(t, err)
	second, err := h.service.Ingest(h.ctx, secondRequest)
	require.NoError(t, err)
	require.Equal(t, "conflict", first.Status)
	require.Equal(t, "conflict", second.Status)
	require.NotEqual(t, first.EntryID, second.EntryID)
	require.Equal(t, firstHash, h.article(t, first.EntryID).ContentHash)
	require.Equal(t, secondHash, h.article(t, second.EntryID).ContentHash)
	assertIngestConflictCount(t, h, originalID, 2)
}
