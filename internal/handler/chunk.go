package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	"reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

type ChunkHandler struct {
	service   interfaces.ChunkService
	kgService interfaces.EntryService
}

// NewChunkHandler creates a new chunk handler.
func NewChunkHandler(service interfaces.ChunkService, kgService interfaces.EntryService) *ChunkHandler {
	return &ChunkHandler{service: service, kgService: kgService}
}

// GetChunkByIDOnly godoc
func (h *ChunkHandler) GetChunkByIDOnly(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start retrieving chunk by ID only")

	chunkID := secutils.SanitizeForLog(c.Param("id"))
	if chunkID == "" {
		logger.Error(ctx, "Chunk ID is empty")
		c.Error(errors.NewBadRequestError("Chunk ID cannot be empty"))
		return
	}

	// Get chunk by ID without space filter (chunk may belong to shared
	// KB; the route-level KB-access guard already verified read
	// permission against the parent KB before we got here).
	chunk, err := h.service.GetChunkByIDOnly(ctx, chunkID)
	if err != nil {
		if err == service.ErrChunkNotFound {
			logger.Warnf(ctx, "Chunk not found, chunk ID: %s", chunkID)
			c.Error(errors.NewNotFoundError("Chunk not found"))
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    chunk,
	})
}

// ListEntryChunks godoc
func (h *ChunkHandler) ListEntryChunks(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start retrieving entry chunks list")

	entryID := secutils.SanitizeForLog(c.Param("entry_id"))
	if entryID == "" {
		logger.Error(ctx, "Entry ID is empty")
		c.Error(errors.NewBadRequestError("Entry ID cannot be empty"))
		return
	}

	// Parse pagination parameters
	var pagination types.Pagination
	if err := c.ShouldBindQuery(&pagination); err != nil {
		logger.Errorf(ctx, "Failed to parse pagination parameters: %s", secutils.SanitizeForLog(err.Error()))
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if pagination.Page < 1 {
		pagination.Page = 1
	}
	if pagination.PageSize < 1 {
		pagination.PageSize = 10
	}
	if pagination.PageSize > 100 {
		pagination.PageSize = 100
	}

	// Default to text chunks; callers may override via ?chunk_type=parent_text.
	chunkType := []types.ChunkType{types.ChunkTypeText}
	if queryTypes := c.QueryArray("chunk_type"); len(queryTypes) > 0 {
		chunkType = make([]types.ChunkType, 0, len(queryTypes))
		for _, qt := range queryTypes {
			chunkType = append(chunkType, types.ChunkType(qt))
		}
	}

	result, err := h.service.ListPagedChunksByEntryID(ctx, entryID, &pagination, chunkType)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"data":      result.Data,
		"total":     result.Total,
		"page":      result.Page,
		"page_size": result.PageSize,
	})
}
