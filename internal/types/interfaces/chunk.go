package interfaces

import (
	"context"

	"reliquary/internal/types"
)

// ChunkImageInfo holds (knowledge_id, image_info) pairs for image cleanup before chunk deletion.
type ChunkImageInfo struct {
	KnowledgeID string `gorm:"column:knowledge_id"`
	ImageInfo   string `gorm:"column:image_info"`
}

// ChunkRepository defines the interface for chunk repository operations
type ChunkRepository interface {
	// CreateChunks creates chunks
	CreateChunks(ctx context.Context, chunks []*types.Chunk) error
	// GetChunkByID gets a chunk by id
	GetChunkByID(ctx context.Context, spaceID uint64, id string) (*types.Chunk, error)
	// GetChunkByIDOnly gets a chunk by id without space filter (for permission resolution)
	GetChunkByIDOnly(ctx context.Context, id string) (*types.Chunk, error)
	// GetChunkBySeqID gets a chunk by seq_id
	GetChunkBySeqID(ctx context.Context, spaceID uint64, seqID int64) (*types.Chunk, error)
	// ListChunksByID lists chunks by ids
	ListChunksByID(ctx context.Context, spaceID uint64, ids []string) ([]*types.Chunk, error)
	ListChunksByIDOnly(ctx context.Context, ids []string) ([]*types.Chunk, error)
	// ListChunksBySeqID lists chunks by seq_ids
	ListChunksBySeqID(ctx context.Context, spaceID uint64, seqIDs []int64) ([]*types.Chunk, error)
	// ListChunksByKnowledgeID lists the knowledge's text chunks. Despite the name
	// it filters to chunk_type = 'text'; reach for ListChunksByKnowledgeIDAndTypes
	// when summary, parent_text or image chunks are needed too.
	ListChunksByKnowledgeID(ctx context.Context, spaceID uint64, knowledgeID string) ([]*types.Chunk, error)
	// ListChunksByKnowledgeIDAndTypes lists the knowledge's chunks restricted to
	// the given chunk types, ordered by chunk_index.
	ListChunksByKnowledgeIDAndTypes(
		ctx context.Context, spaceID uint64, knowledgeID string, chunkTypes []types.ChunkType,
	) ([]*types.Chunk, error)
	ListPagedChunksByKnowledgeID(ctx context.Context,
		spaceID uint64,
		knowledgeID string,
		page *types.Pagination,
		chunkType []types.ChunkType,
		keyword string,
		sortOrder string,
		isEnabled *bool) ([]*types.Chunk, int64, error)
	ListChunkByParentID(ctx context.Context, spaceID uint64, parentID string) ([]*types.Chunk, error)
	// ListChunksByParentIDs lists chunks whose parent_chunk_id is in the given list
	ListChunksByParentIDs(ctx context.Context, spaceID uint64, parentIDs []string) ([]*types.Chunk, error)
	// UpdateChunk updates a chunk
	UpdateChunk(ctx context.Context, chunk *types.Chunk) error
	// UpdateChunks updates chunks in batch
	UpdateChunks(ctx context.Context, chunks []*types.Chunk) error
	// SaveChunks persists full chunk objects in a single transaction using GORM Save (UPDATE).
	SaveChunks(ctx context.Context, chunks []*types.Chunk) error
	// DeleteChunk deletes a chunk
	DeleteChunk(ctx context.Context, spaceID uint64, id string) error
	// DeleteChunks deletes chunks by IDs in batch
	DeleteChunks(ctx context.Context, spaceID uint64, ids []string) error
	// DeleteChunksByKnowledgeID deletes chunks by knowledge id
	DeleteChunksByKnowledgeID(ctx context.Context, spaceID uint64, knowledgeID string) error
	// DeleteByKnowledgeList deletes all chunks for a knowledge list
	DeleteByKnowledgeList(ctx context.Context, spaceID uint64, knowledgeIDs []string) error
	// ListImageInfoByKnowledgeIDs returns non-empty (knowledge_id, image_info) pairs for image cleanup.
	ListImageInfoByKnowledgeIDs(ctx context.Context, spaceID uint64, knowledgeIDs []string) ([]ChunkImageInfo, error)
	// CountChunksByKnowledgeBaseID counts the number of chunks in a knowledge base.
	CountChunksByKnowledgeBaseID(ctx context.Context, spaceID uint64, kbID string) (int64, error)
	// DeleteUnindexedChunks deletes unindexed chunks by knowledge id and chunk index range
	DeleteUnindexedChunks(ctx context.Context, spaceID uint64, knowledgeID string) ([]*types.Chunk, error)
}

// ChunkService defines the interface for chunk service operations
type ChunkService interface {
	// CreateChunks creates chunks
	CreateChunks(ctx context.Context, chunks []*types.Chunk) error
	// GetChunkByID gets a chunk by id (uses space from context)
	GetChunkByID(ctx context.Context, id string) (*types.Chunk, error)
	// GetChunkByIDOnly gets a chunk by id without space filter (for permission resolution)
	GetChunkByIDOnly(ctx context.Context, id string) (*types.Chunk, error)
	// ListChunksByKnowledgeID lists chunks by knowledge id
	ListChunksByKnowledgeID(ctx context.Context, knowledgeID string) ([]*types.Chunk, error)
	// ListPagedChunksByKnowledgeID lists paged chunks by knowledge id
	ListPagedChunksByKnowledgeID(
		ctx context.Context,
		knowledgeID string,
		page *types.Pagination,
		chunkType []types.ChunkType,
	) (*types.PageResult, error)
	// UpdateChunk updates a chunk
	UpdateChunk(ctx context.Context, chunk *types.Chunk) error
	// UpdateChunks updates chunks in batch
	UpdateChunks(ctx context.Context, chunks []*types.Chunk) error
	// DeleteChunk deletes a chunk
	DeleteChunk(ctx context.Context, id string) error
	// DeleteChunks deletes chunks by IDs in batch
	DeleteChunks(ctx context.Context, ids []string) error
	// DeleteChunksByKnowledgeID deletes chunks by knowledge id
	DeleteChunksByKnowledgeID(ctx context.Context, knowledgeID string) error
	// DeleteByKnowledgeList deletes all chunks for a knowledge list
	DeleteByKnowledgeList(ctx context.Context, ids []string) error
	// ListChunkByParentID lists chunks by parent id
	ListChunkByParentID(ctx context.Context, spaceID uint64, parentID string) ([]*types.Chunk, error)
	// GetRepository gets the chunk repository
	GetRepository() ChunkRepository
}
