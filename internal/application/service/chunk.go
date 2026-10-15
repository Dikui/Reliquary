// Package service provides business logic implementations for Reliquary application
// This package contains service layer implementations that coordinate between
// repositories and handlers, applying business rules and transaction management
package service

import (
	"context"
	"errors"

	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// chunkService implements the ChunkService interface
// It provides operations for managing document chunks in the library
// Chunks are segments of documents that have been processed and prepared for indexing
type chunkService struct {
	chunkRepository   interfaces.ChunkRepository // Repository for chunk data persistence
	entryRepo         interfaces.EntryRepository
	libraryRepository interfaces.LibraryRepository
	modelService      interfaces.ModelService
	retrieveEngine    interfaces.RetrievalService
}

// NewChunkService creates a new chunk service
// It initializes a service with the provided chunk repository
// Parameters:
//   - chunkRepository: Repository for chunk operations
//
// Returns:
//   - interfaces.ChunkService: Initialized chunk service implementation
func NewChunkService(
	chunkRepository interfaces.ChunkRepository,
	entryRepo interfaces.EntryRepository,
	libraryRepository interfaces.LibraryRepository,
	modelService interfaces.ModelService,
	retrieveEngine interfaces.RetrievalService,
) interfaces.ChunkService {
	return &chunkService{
		chunkRepository:   chunkRepository,
		entryRepo:         entryRepo,
		libraryRepository: libraryRepository,
		modelService:      modelService,
		retrieveEngine:    retrieveEngine,
	}
}

// GetRepository gets the chunk repository
// Parameters:
//   - ctx: Context with authentication and request information
//
// Returns:
//   - interfaces.ChunkRepository: Chunk repository
func (s *chunkService) GetRepository() interfaces.ChunkRepository {
	return s.chunkRepository
}

// CreateChunks creates multiple chunks
// This method persists a batch of document chunks to the repository
// Parameters:
//   - ctx: Context with authentication and request information
//   - chunks: Slice of document chunks to create
//
// Returns:
//   - error: Any error encountered during chunk creation
func (s *chunkService) CreateChunks(ctx context.Context, chunks []*types.Chunk) error {
	err := s.chunkRepository.CreateChunks(ctx, chunks)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"chunk_count": len(chunks),
		})
		return err
	}

	logger.Infof(ctx, "Add %d chunks successfully", len(chunks))
	return nil
}

// GetChunkByID retrieves a chunk by its ID
// This method fetches a specific chunk using its ID and validates space access
// Parameters:
//   - ctx: Context with authentication and request information
//   - entryID: ID of the entry document containing the chunk
//   - id: ID of the chunk to retrieve
//
// Returns:
//   - *types.Chunk: Retrieved chunk if found
//   - error: Any error encountered during retrieval
func (s *chunkService) GetChunkByID(ctx context.Context, id string) (*types.Chunk, error) {
	spaceID := types.MustSpaceIDFromContext(ctx)
	logger.Infof(ctx, "Getting chunk by ID, ID: %s, space ID: %d", id, spaceID)
	chunk, err := s.chunkRepository.GetChunkByID(ctx, spaceID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"space_id": spaceID,
		})
		return nil, err
	}

	logger.Info(ctx, "Chunk retrieved successfully")
	return chunk, nil
}

// GetChunkByIDOnly retrieves a chunk by ID without space filter (for permission resolution).
func (s *chunkService) GetChunkByIDOnly(ctx context.Context, id string) (*types.Chunk, error) {
	chunk, err := s.chunkRepository.GetChunkByIDOnly(ctx, id)
	if err != nil {
		// errors.Is (not string equality) so the sentinel survives wrapping.
		// ErrChunkNotFound aliases the repo sentinel, so this matches directly.
		if errors.Is(err, ErrChunkNotFound) {
			return nil, ErrChunkNotFound
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"chunk_id": id})
		return nil, err
	}
	return chunk, nil
}

// ListChunksByEntryID lists all chunks for a entry ID
// This method retrieves all chunks belonging to a specific entry document
// Parameters:
//   - ctx: Context with authentication and request information
//   - entryID: ID of the entry document
//
// Returns:
//   - []*types.Chunk: List of chunks belonging to the entry document
//   - error: Any error encountered during retrieval
func (s *chunkService) ListChunksByEntryID(ctx context.Context, entryID string) ([]*types.Chunk, error) {
	logger.Info(ctx, "Start listing chunks by entry ID")
	logger.Infof(ctx, "Entry ID: %s", entryID)

	spaceID := types.MustSpaceIDFromContext(ctx)
	logger.Infof(ctx, "Space ID: %d", spaceID)

	chunks, err := s.chunkRepository.ListChunksByEntryID(ctx, spaceID, entryID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"entry_id": entryID,
			"space_id": spaceID,
		})
		return nil, err
	}

	logger.Infof(ctx, "Retrieved %d chunks successfully", len(chunks))
	return chunks, nil
}

// ListPagedChunksByEntryID lists chunks for a entry ID with pagination
// This method retrieves chunks with pagination support for better performance with large datasets
// Parameters:
//   - ctx: Context with authentication and request information
//   - entryID: ID of the entry document
//   - page: Pagination parameters including page number and page size
//
// Returns:
//   - *types.PageResult: Paginated result containing chunks and pagination metadata
//   - error: Any error encountered during retrieval
func (s *chunkService) ListPagedChunksByEntryID(ctx context.Context,
	entryID string, page *types.Pagination, chunkType []types.ChunkType,
) (*types.PageResult, error) {
	spaceID := types.MustSpaceIDFromContext(ctx)
	chunks, total, err := s.chunkRepository.ListPagedChunksByEntryID(ctx,
		spaceID,
		entryID,
		page,
		chunkType,
		"",
		"",
		nil)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"entry_id": entryID,
			"space_id": spaceID,
		})
		return nil, err
	}

	logger.Infof(ctx, "Retrieved %d chunks out of %d total chunks", len(chunks), total)
	return types.NewPageResult(total, page, chunks), nil
}

// updateChunk updates a chunk
// This method updates an existing chunk in the repository
// Parameters:
//   - ctx: Context with authentication and request information
//   - chunk: Chunk with updated fields
//
// Returns:
//   - error: Any error encountered during update
//
// This method handles the actual update logic for a chunk, including updating the vector database representation
func (s *chunkService) UpdateChunk(ctx context.Context, chunk *types.Chunk) error {
	logger.Infof(ctx, "Updating chunk, ID: %s, entry ID: %s", chunk.ID, chunk.EntryID)

	// Update the chunk in the repository
	err := s.chunkRepository.UpdateChunk(ctx, chunk)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"chunk_id": chunk.ID,
			"entry_id": chunk.EntryID,
		})
		return err
	}

	logger.Info(ctx, "Chunk updated successfully")
	return nil
}

// UpdateChunks updates chunks in batch
func (s *chunkService) UpdateChunks(ctx context.Context, chunks []*types.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	logger.Infof(ctx, "Updating %d chunks in batch", len(chunks))

	// Update the chunks in the repository
	err := s.chunkRepository.UpdateChunks(ctx, chunks)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"chunk_count": len(chunks),
		})
		return err
	}

	logger.Infof(ctx, "Successfully updated %d chunks", len(chunks))
	return nil
}

// DeleteChunk deletes a chunk by ID
// This method removes a specific chunk from the repository
// Parameters:
//   - ctx: Context with authentication and request information
//   - id: ID of the chunk to delete
//
// Returns:
//   - error: Any error encountered during deletion
func (s *chunkService) DeleteChunk(ctx context.Context, id string) error {
	spaceID := types.MustSpaceIDFromContext(ctx)
	err := s.chunkRepository.DeleteChunk(ctx, spaceID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"space_id": spaceID,
		})
		return err
	}
	logger.Info(ctx, "Chunk deleted successfully")
	return nil
}

// DeleteChunks deletes chunks by IDs in batch
// This method removes multiple chunks from the repository in a single operation
// Parameters:
//   - ctx: Context with authentication and request information
//   - ids: Slice of chunk IDs to delete
//
// Returns:
//   - error: Any error encountered during batch deletion
func (s *chunkService) DeleteChunks(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	logger.Info(ctx, "Start deleting chunks in batch")
	logger.Infof(ctx, "Deleting %d chunks", len(ids))

	spaceID := types.MustSpaceIDFromContext(ctx)
	logger.Infof(ctx, "Space ID: %d", spaceID)

	err := s.chunkRepository.DeleteChunks(ctx, spaceID, ids)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"chunk_ids": ids,
			"space_id":  spaceID,
		})
		return err
	}

	logger.Infof(ctx, "Successfully deleted %d chunks", len(ids))
	return nil
}

// DeleteChunksByEntryID deletes all chunks for a entry ID
// This method removes all chunks belonging to a specific entry document
// Parameters:
//   - ctx: Context with authentication and request information
//   - entryID: ID of the entry document
//
// Returns:
//   - error: Any error encountered during bulk deletion
func (s *chunkService) DeleteChunksByEntryID(ctx context.Context, entryID string) error {
	logger.Info(ctx, "Start deleting all chunks by entry ID")
	logger.Infof(ctx, "Entry ID: %s", entryID)

	spaceID := types.MustSpaceIDFromContext(ctx)
	logger.Infof(ctx, "Space ID: %d", spaceID)

	err := s.chunkRepository.DeleteChunksByEntryID(ctx, spaceID, entryID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"entry_id": entryID,
			"space_id": spaceID,
		})
		return err
	}

	logger.Info(ctx, "All chunks under entry deleted successfully")
	return nil
}

func (s *chunkService) DeleteByEntryList(ctx context.Context, ids []string) error {
	logger.Info(ctx, "Start deleting all chunks by entry IDs")
	logger.Infof(ctx, "Entry IDs: %v", ids)

	spaceID := types.MustSpaceIDFromContext(ctx)
	logger.Infof(ctx, "Space ID: %d", spaceID)

	err := s.chunkRepository.DeleteByEntryList(ctx, spaceID, ids)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"entry_id": ids,
			"space_id": spaceID,
		})
		return err
	}

	logger.Info(ctx, "All chunks under entry deleted successfully")
	return nil
}

func (s *chunkService) ListChunkByParentID(
	ctx context.Context,
	spaceID uint64,
	parentID string,
) ([]*types.Chunk, error) {
	logger.Info(ctx, "Start listing chunk by parent ID")
	logger.Infof(ctx, "Parent ID: %s", parentID)

	chunks, err := s.chunkRepository.ListChunkByParentID(ctx, spaceID, parentID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"parent_id": parentID,
			"space_id":  spaceID,
		})
		return nil, err
	}

	logger.Info(ctx, "Chunk listed successfully")
	return chunks, nil
}
