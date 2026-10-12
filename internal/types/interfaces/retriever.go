package interfaces

import (
	"context"

	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

// RetrieveEngine defines the retrieve engine interface
type RetrieveEngine interface {
	// EngineType gets the retrieve engine type
	EngineType() types.RetrieverEngineType

	// Retrieve executes the retrieve
	Retrieve(ctx context.Context, params types.RetrieveParams) ([]*types.RetrieveResult, error)

	// Support gets the supported retrieve types
	Support() []types.RetrieverType
}

// RetrieveEngineRepository defines the retrieve engine repository interface
type RetrieveEngineRepository interface {
	// Save saves the index info
	Save(ctx context.Context, indexInfo *types.IndexInfo, params map[string]any) error

	// BatchSave saves the index info list
	BatchSave(ctx context.Context, indexInfoList []*types.IndexInfo, params map[string]any) error

	// EstimateStorageSize estimates the storage size
	EstimateStorageSize(ctx context.Context, indexInfoList []*types.IndexInfo, params map[string]any) int64

	// DeleteByChunkIDList deletes the index info by chunk id list
	DeleteByChunkIDList(ctx context.Context, indexIDList []string, dimension int, entryType string) error
	// DeleteBySourceIDList deletes the index info by source id list
	DeleteBySourceIDList(ctx context.Context, sourceIDList []string, dimension int, entryType string) error
	// DeleteByEntryIDList deletes the index info by entry id list
	DeleteByEntryIDList(ctx context.Context, entryIDList []string, dimension int, entryType string) error

	// BatchUpdateChunkEnabledStatus updates the enabled status of chunks in batch
	// chunkStatusMap: map of chunk ID to enabled status (true = enabled, false = disabled)
	BatchUpdateChunkEnabledStatus(ctx context.Context, chunkStatusMap map[string]bool) error

	// RetrieveEngine retrieves the engine
	RetrieveEngine
}

// RetrieveEngineService defines the retrieve engine service interface
type RetrieveEngineService interface {
	// Index indexes the index info
	Index(ctx context.Context,
		embedder embedding.Embedder,
		indexInfo *types.IndexInfo,
		retrieverTypes []types.RetrieverType,
	) error

	// BatchIndex indexes the index info list
	BatchIndex(ctx context.Context,
		embedder embedding.Embedder,
		indexInfoList []*types.IndexInfo,
		retrieverTypes []types.RetrieverType,
	) error

	// EstimateStorageSize estimates the storage size
	EstimateStorageSize(ctx context.Context,
		embedder embedding.Embedder,
		indexInfoList []*types.IndexInfo,
		retrieverTypes []types.RetrieverType,
	) int64
	// DeleteByChunkIDList deletes the index info by chunk id list
	DeleteByChunkIDList(ctx context.Context, indexIDList []string, dimension int, entryType string) error

	// DeleteBySourceIDList deletes the index info by source id list
	DeleteBySourceIDList(ctx context.Context, sourceIDList []string, dimension int, entryType string) error

	// DeleteByEntryIDList deletes the index info by entry id list
	DeleteByEntryIDList(ctx context.Context, entryIDList []string, dimension int, entryType string) error

	// BatchUpdateChunkEnabledStatus updates the enabled status of chunks in batch
	// chunkStatusMap: map of chunk ID to enabled status (true = enabled, false = disabled)
	BatchUpdateChunkEnabledStatus(ctx context.Context, chunkStatusMap map[string]bool) error

	// RetrieveEngine retrieves the engine
	RetrieveEngine
}

// RetrievalService accesses the application's PostgreSQL retrieval indexes.
type RetrievalService interface {
	Retrieve(context.Context, []types.RetrieveParams) ([]*types.RetrieveResult, error)
	SupportRetriever(types.RetrieverType) bool
	Index(context.Context, embedding.Embedder, *types.IndexInfo) error
	BatchIndex(context.Context, embedding.Embedder, []*types.IndexInfo) error
	EstimateStorageSize(context.Context, embedding.Embedder, []*types.IndexInfo) int64
	DeleteByChunkIDList(context.Context, []string, int, string) error
	DeleteBySourceIDList(context.Context, []string, int, string) error
	DeleteByEntryIDList(context.Context, []string, int, string) error
	BatchUpdateChunkEnabledStatus(context.Context, map[string]bool) error
}
