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
	kgService interfaces.KnowledgeService
}

// NewChunkHandler creates a new chunk handler.
func NewChunkHandler(service interfaces.ChunkService, kgService interfaces.KnowledgeService) *ChunkHandler {
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

	// Get chunk by ID without tenant filter (chunk may belong to shared
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

// ListKnowledgeChunks godoc
func (h *ChunkHandler) ListKnowledgeChunks(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start retrieving knowledge chunks list")

	knowledgeID := secutils.SanitizeForLog(c.Param("knowledge_id"))
	if knowledgeID == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
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

	// Default to text chunks; callers may override via ?chunk_type=image_caption etc.
	chunkType := []types.ChunkType{types.ChunkTypeText}
	if queryTypes := c.QueryArray("chunk_type"); len(queryTypes) > 0 {
		chunkType = make([]types.ChunkType, 0, len(queryTypes))
		for _, qt := range queryTypes {
			chunkType = append(chunkType, types.ChunkType(qt))
		}
	}

	result, err := h.service.ListPagedChunksByKnowledgeID(ctx, knowledgeID, &pagination, chunkType)
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

func (h *ChunkHandler) UpsertGeneratedQuestion(c *gin.Context) {
	chunkID := secutils.SanitizeForLog(c.Param("id"))
	if chunkID == "" {
		c.Error(errors.NewBadRequestError("Chunk ID is required"))
		return
	}
	var req UpsertGeneratedQuestionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	item, err := h.service.UpsertGeneratedQuestion(c.Request.Context(), chunkID, req.QuestionID, req.Question)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *ChunkHandler) RegenerateGeneratedQuestions(c *gin.Context) {
	chunkID := secutils.SanitizeForLog(c.Param("id"))
	if chunkID == "" {
		c.Error(errors.NewBadRequestError("Chunk ID is required"))
		return
	}
	items, err := h.kgService.RegenerateChunkQuestions(c.Request.Context(), chunkID)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// DeleteGeneratedQuestion godoc
func (h *ChunkHandler) DeleteGeneratedQuestion(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start deleting generated question from chunk")

	chunkID := secutils.SanitizeForLog(c.Param("id"))
	if chunkID == "" {
		logger.Error(ctx, "Chunk ID is empty")
		c.Error(errors.NewBadRequestError("Chunk ID cannot be empty"))
		return
	}

	var req struct {
		QuestionID string `json:"question_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Errorf(ctx, "Failed to parse request parameters: %s", secutils.SanitizeForLog(err.Error()))
		c.Error(errors.NewBadRequestError("Question ID is required"))
		return
	}

	if err := h.service.DeleteGeneratedQuestion(ctx, chunkID, req.QuestionID); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	logger.Infof(ctx, "Generated question deleted successfully, chunk ID: %s, question ID: %s",
		secutils.SanitizeForLog(chunkID), secutils.SanitizeForLog(req.QuestionID))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Generated question deleted",
	})
}

type UpsertGeneratedQuestionRequest struct {
	QuestionID string `json:"question_id"`
	Question   string `json:"question" binding:"required"`
}
