package retriever

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"
	"reliquary/internal/logger"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

// safetyMaxChars is an absolute upper bound for any single embedding input.
// Beyond this we truncate (with a warning) instead of blindly forwarding to
// the embedding API, which would either error out or silently truncate in a
// model-specific way. Set well above any current chunkSize budget so it only
// kicks in for genuinely pathological inputs.
const safetyMaxChars = 20000

// embedRetryAttempts and embedRetryBaseDelay control the exponential backoff
// applied to BatchEmbedWithPool calls.
const (
	embedRetryAttempts  = 5
	embedRetryBaseDelay = 200 * time.Millisecond
)

var embeddingImagePayloadPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<img\b[^>]*\bsrc=["']\s*data:image/[a-z0-9.+-]+;base64,[^"']+["'][^>]*>`),
	regexp.MustCompile(`(?is)!\[[^\]]*\]\(\s*data:image/[a-z0-9.+-]+;base64,[^)]+\)`),
	regexp.MustCompile(`(?i)data:image/[a-z0-9.+-]+;base64,[a-z0-9+/=]{200,}`),
	regexp.MustCompile(`(?i)data:[a-z0-9.+/-]+;base64,[a-z0-9+/=]{200,}`),
}

// KeywordsVectorHybridRetrieveEngineService implements a hybrid retrieval engine
// that supports both keyword-based and vector-based retrieval
type KeywordsVectorHybridRetrieveEngineService struct {
	indexRepository interfaces.RetrieveEngineRepository
	engineType      types.RetrieverEngineType
}

// NewKVHybridRetrieveEngine creates a new instance of the hybrid retrieval engine
// KV stands for KeywordsVector
func NewKVHybridRetrieveEngine(indexRepository interfaces.RetrieveEngineRepository,
	engineType types.RetrieverEngineType,
) interfaces.RetrieveEngineService {
	return &KeywordsVectorHybridRetrieveEngineService{indexRepository: indexRepository, engineType: engineType}
}

// EngineType returns the type of the retrieval engine
func (v *KeywordsVectorHybridRetrieveEngineService) EngineType() types.RetrieverEngineType {
	return v.engineType
}

// Retrieve performs retrieval based on the provided parameters
func (v *KeywordsVectorHybridRetrieveEngineService) Retrieve(ctx context.Context,
	params types.RetrieveParams,
) ([]*types.RetrieveResult, error) {
	return v.indexRepository.Retrieve(ctx, params)
}

// Index creates embeddings for the content and saves it to the repository
// if vector retrieval is enabled in the retriever types
func (v *KeywordsVectorHybridRetrieveEngineService) Index(ctx context.Context,
	embedder embedding.Embedder, indexInfo *types.IndexInfo, retrieverTypes []types.RetrieverType,
) error {
	params := make(map[string]any)
	embeddingMap := make(map[string][]float32)
	if slices.Contains(retrieverTypes, types.VectorRetrieverType) {
		embedding, err := embedder.Embed(ctx, sanitizeForEmbedding(ctx, indexInfo.Content))
		if err != nil {
			return err
		}
		embeddingMap[indexInfo.SourceID] = embedding
		params["model_id"] = embedder.GetModelID()
		params["model_profile_hash"] = embedderProfileHash(embedder)
	}
	params["embedding"] = embeddingMap
	return v.indexRepository.Save(ctx, indexInfo, params)
}

// BatchIndex creates embeddings for multiple content items and saves them to the repository
// in batches for efficiency. Uses concurrent batch saving to improve performance.
func (v *KeywordsVectorHybridRetrieveEngineService) BatchIndex(ctx context.Context,
	embedder embedding.Embedder, indexInfoList []*types.IndexInfo, retrieverTypes []types.RetrieverType,
) error {
	if len(indexInfoList) == 0 {
		return nil
	}

	if slices.Contains(retrieverTypes, types.VectorRetrieverType) {
		var contentList []string
		for _, indexInfo := range indexInfoList {
			contentList = append(contentList, sanitizeForEmbedding(ctx, indexInfo.Content))
		}
		embeddings, err := batchEmbedWithBackoff(ctx, embedder, contentList)
		if err != nil {
			return err
		}

		batchSize := 40
		chunks := utils.ChunkSlice(indexInfoList, batchSize)

		// Use concurrent batch saving for better performance
		// Limit concurrency to avoid overwhelming the backend
		const maxConcurrency = 5
		if len(chunks) <= maxConcurrency {
			// For small number of batches, use simple concurrency
			return v.concurrentBatchSave(ctx, chunks, embeddings, batchSize, embedder.GetModelID(), embedderProfileHash(embedder))
		}

		// For large number of batches, use bounded concurrency
		return v.boundedConcurrentBatchSave(ctx, chunks, embeddings, batchSize, maxConcurrency, embedder.GetModelID(), embedderProfileHash(embedder))
	}

	// For non-vector retrieval, use concurrent batch saving as well
	chunks := utils.ChunkSlice(indexInfoList, 10)
	const maxConcurrency = 5
	if len(chunks) <= maxConcurrency {
		return v.concurrentBatchSaveNoEmbedding(ctx, chunks)
	}
	return v.boundedConcurrentBatchSaveNoEmbedding(ctx, chunks, maxConcurrency)
}

// batchEmbedWithBackoff calls BatchEmbedWithPool with exponential backoff on
// transient failures (200 / 400 / 800 / 1600 / 3200 ms). It returns the last
// embedding result on success or the last error if every attempt failed.
func batchEmbedWithBackoff(ctx context.Context, embedder embedding.Embedder, contentList []string) ([][]float32, error) {
	delay := embedRetryBaseDelay
	var (
		embeddings [][]float32
		err        error
	)
	for attempt := 0; attempt < embedRetryAttempts; attempt++ {
		embeddings, err = embedder.BatchEmbedWithPool(ctx, embedder, contentList)
		if err == nil {
			return embeddings, nil
		}
		logger.Errorf(ctx, "BatchEmbedWithPool attempt %d/%d failed: %v", attempt+1, embedRetryAttempts, err)
		if attempt+1 < embedRetryAttempts {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			delay *= 2
		}
	}
	return embeddings, err
}

// sanitizeForEmbedding caps content length at safetyMaxChars characters so
// pathologically large inputs cannot blow up the embedding API call. The
// truncation point is char-based, not token-based, so it sits well above any
// realistic token limit. We log a warning whenever truncation kicks in.
func sanitizeForEmbedding(ctx context.Context, content string) string {
	sanitized := content
	// Scrubbing only matters when an inline base64 payload is present; skip the
	// regex passes otherwise so the common (no-image) path stays cheap.
	if strings.Contains(content, "base64,") {
		for _, pattern := range embeddingImagePayloadPatterns {
			sanitized = pattern.ReplaceAllString(sanitized, "[image]")
		}
	}

	if utf8.RuneCountInString(sanitized) <= safetyMaxChars {
		return sanitized
	}
	runes := []rune(sanitized)
	logger.Warnf(ctx, "embedding input truncated: %d runes -> %d", len(runes), safetyMaxChars)
	return string(runes[:safetyMaxChars])
}

// concurrentBatchSave saves all batches concurrently without concurrency limit
func (v *KeywordsVectorHybridRetrieveEngineService) concurrentBatchSave(
	ctx context.Context,
	chunks [][]*types.IndexInfo,
	embeddings [][]float32,
	batchSize int,
	modelID string,
	profileHash string,
) error {
	g, ctx := errgroup.WithContext(ctx)
	for i, indexChunk := range chunks {
		g.Go(func() error {
			params := make(map[string]any)
			embeddingMap := make(map[string][]float32)
			for j, indexInfo := range indexChunk {
				embeddingMap[indexInfo.SourceID] = embeddings[i*batchSize+j]
			}
			params["embedding"] = embeddingMap
			params["model_id"] = modelID
			params["model_profile_hash"] = profileHash
			return v.indexRepository.BatchSave(ctx, indexChunk, params)
		})
	}
	return g.Wait()
}

// boundedConcurrentBatchSave saves batches with bounded concurrency using semaphore pattern
func (v *KeywordsVectorHybridRetrieveEngineService) boundedConcurrentBatchSave(
	ctx context.Context,
	chunks [][]*types.IndexInfo,
	embeddings [][]float32,
	batchSize int,
	maxConcurrency int,
	modelID string,
	profileHash string,
) error {
	g, ctx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, maxConcurrency)

	for i, indexChunk := range chunks {
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return ctx.Err()
			}

			params := make(map[string]any)
			embeddingMap := make(map[string][]float32)
			for j, indexInfo := range indexChunk {
				embeddingMap[indexInfo.SourceID] = embeddings[i*batchSize+j]
			}
			params["embedding"] = embeddingMap
			params["model_id"] = modelID
			params["model_profile_hash"] = profileHash
			return v.indexRepository.BatchSave(ctx, indexChunk, params)
		})
	}
	return g.Wait()
}

func embedderProfileHash(embedder embedding.Embedder) string {
	if profile, ok := embedder.(embedding.ProfiledEmbedder); ok {
		return profile.GetProfileHash()
	}
	return ""
}

// concurrentBatchSaveNoEmbedding saves all batches concurrently without embeddings
func (v *KeywordsVectorHybridRetrieveEngineService) concurrentBatchSaveNoEmbedding(
	ctx context.Context,
	chunks [][]*types.IndexInfo,
) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, indexChunk := range chunks {
		g.Go(func() error {
			params := make(map[string]any)
			return v.indexRepository.BatchSave(ctx, indexChunk, params)
		})
	}
	return g.Wait()
}

// boundedConcurrentBatchSaveNoEmbedding saves batches with bounded concurrency without embeddings
func (v *KeywordsVectorHybridRetrieveEngineService) boundedConcurrentBatchSaveNoEmbedding(
	ctx context.Context,
	chunks [][]*types.IndexInfo,
	maxConcurrency int,
) error {
	g, ctx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, maxConcurrency)

	for _, indexChunk := range chunks {
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return ctx.Err()
			}

			params := make(map[string]any)
			return v.indexRepository.BatchSave(ctx, indexChunk, params)
		})
	}
	return g.Wait()
}

// DeleteByChunkIDList deletes vectors by their chunk IDs
func (v *KeywordsVectorHybridRetrieveEngineService) DeleteByChunkIDList(ctx context.Context,
	indexIDList []string, dimension int, entryType string,
) error {
	return v.indexRepository.DeleteByChunkIDList(ctx, indexIDList, dimension, entryType)
}

// DeleteBySourceIDList deletes vectors by their source IDs
func (v *KeywordsVectorHybridRetrieveEngineService) DeleteBySourceIDList(ctx context.Context,
	sourceIDList []string, dimension int, entryType string,
) error {
	return v.indexRepository.DeleteBySourceIDList(ctx, sourceIDList, dimension, entryType)
}

// DeleteByEntryIDList deletes vectors by their entry IDs
func (v *KeywordsVectorHybridRetrieveEngineService) DeleteByEntryIDList(ctx context.Context,
	entryIDList []string, dimension int, entryType string,
) error {
	return v.indexRepository.DeleteByEntryIDList(ctx, entryIDList, dimension, entryType)
}

// Support returns the retriever types supported by this engine
func (v *KeywordsVectorHybridRetrieveEngineService) Support() []types.RetrieverType {
	return v.indexRepository.Support()
}

// EstimateStorageSize estimates the storage space needed for the provided index information
func (v *KeywordsVectorHybridRetrieveEngineService) EstimateStorageSize(
	ctx context.Context,
	embedder embedding.Embedder,
	indexInfoList []*types.IndexInfo,
	retrieverTypes []types.RetrieverType,
) int64 {
	params := make(map[string]any)
	if slices.Contains(retrieverTypes, types.VectorRetrieverType) {
		embeddingMap := make(map[string][]float32)
		// just for estimate storage size
		for _, indexInfo := range indexInfoList {
			embeddingMap[indexInfo.ChunkID] = make([]float32, embedder.GetDimensions())
		}
		params["embedding"] = embeddingMap
	}
	return v.indexRepository.EstimateStorageSize(ctx, indexInfoList, params)
}
