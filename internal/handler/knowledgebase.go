package handler

import (
	"encoding/json"
	stderrors "errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

// KnowledgeBaseHandler defines the HTTP handler for knowledge base operations
type KnowledgeBaseHandler struct {
	service interfaces.KnowledgeBaseService
}

func NewKnowledgeBaseHandler(service interfaces.KnowledgeBaseService) *KnowledgeBaseHandler {
	return &KnowledgeBaseHandler{service: service}
}

func buildKBResponse(
	kb *types.KnowledgeBase,
	extras map[string]interface{},
) interface{} {
	b, err := json.Marshal(kb)
	if err != nil {
		return kb
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return kb
	}

	for k, v := range extras {
		m[k] = v
	}
	return m
}

// HybridSearch keeps the old route explicit while callers adopt article retrieval.
func (h *KnowledgeBaseHandler) HybridSearch(c *gin.Context) {
	if _, _, _, err := h.validateAndGetKnowledgeBase(c); err != nil {
		c.Error(err)
		return
	}
	c.Error(apperrors.NewBadRequestError("chunk search and knowledge base scope are no longer supported; use POST /api/v1/knowledge-search with query and article filters"))
}

func (h *KnowledgeBaseHandler) validateAndGetKnowledgeBase(c *gin.Context) (*types.KnowledgeBase, string, uint64, error) {
	ctx := c.Request.Context()

	// Get space ID from context
	spaceID, exists := c.Get(types.SpaceIDContextKey.String())
	if !exists {
		logger.Error(ctx, "Failed to get space ID")
		return nil, "", 0, apperrors.NewUnauthorizedError("Unauthorized")
	}

	// Get knowledge base ID from URL parameter
	id := c.Param("id")
	var kb *types.KnowledgeBase
	var err error
	if id == "" {
		kb, err = h.service.GetKnowledgeBase(ctx)
	} else {
		kb, err = h.service.GetKnowledgeBaseByID(ctx, id)
	}
	if err != nil {
		// Missing IDs must retain their 404 response.
		if stderrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			return nil, id, 0, apperrors.NewNotFoundError("knowledge base not found")
		}
		logger.ErrorWithFields(ctx, err, nil)
		return nil, id, 0, apperrors.NewInternalServerError(err.Error())
	}

	// Check 1: Verify space ownership (owner has full access)
	if kb.SpaceID == spaceID.(uint64) {
		return kb, kb.ID, spaceID.(uint64), nil
	}

	// No permission: not owner and no shared access
	logger.Warnf(
		ctx,
		"Space has no permission to access this knowledge base, knowledge base ID: %s, "+
			"request space ID: %d, knowledge base space ID: %d",
		id, spaceID.(uint64), kb.SpaceID,
	)
	return nil, id, 0, apperrors.NewForbiddenError("No permission to operate")
}

func (h *KnowledgeBaseHandler) GetKnowledgeBase(c *gin.Context) {
	// Validate and get the knowledge base
	kb, _, _, err := h.validateAndGetKnowledgeBase(c)
	if err != nil {
		c.Error(err)
		return
	}
	// Fill counts (knowledge_count, chunk_count, is_processing) so hover/detail shows correct numbers
	if fillErr := h.service.FillKnowledgeBaseCounts(c.Request.Context(), kb); fillErr != nil {
		logger.Warnf(c.Request.Context(), "Failed to fill KB counts for %s: %v", kb.ID, fillErr)
	}
	var extras map[string]interface{}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": buildKBResponse(kb, extras)})
}

// UpdateKnowledgeBaseRequest defines the request body structure for updating a knowledge base
type UpdateKnowledgeBaseRequest struct {
	Name        string                     `json:"name"        binding:"required"`
	Description string                     `json:"description"`
	Config      *types.KnowledgeBaseConfig `json:"config"`
	ModelTiers  *types.ModelTierConfig     `json:"model_tiers"`
}

// UpdateKnowledgeBase godoc
func (h *KnowledgeBaseHandler) UpdateKnowledgeBase(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start updating knowledge base")

	// Validate and get the knowledge base
	_, id, _, err := h.validateAndGetKnowledgeBase(c)
	if err != nil {
		c.Error(err)
		return
	}

	// Parse request body
	var req UpdateKnowledgeBaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(apperrors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}
	if req.ModelTiers != nil {
		if req.Config == nil {
			req.Config = &types.KnowledgeBaseConfig{}
		}
		req.Config.ModelTiers = req.ModelTiers
	}
	if req.Config != nil {
		probe := &types.KnowledgeBase{}
		if req.Config.ChunkingConfig != nil {
			probe.ChunkingConfig = *req.Config.ChunkingConfig
		}
		if err := validateKnowledgeBasePromptInstructions(probe); err != nil {
			c.Error(err)
			return
		}
	}

	logger.Infof(ctx, "Updating knowledge base, ID: %s, name: %s",
		secutils.SanitizeForLog(id), secutils.SanitizeForLog(req.Name))

	// Update the knowledge base
	kb, err := h.service.UpdateKnowledgeBase(ctx, id, req.Name, req.Description, req.Config)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Knowledge base updated successfully, ID: %s",
		secutils.SanitizeForLog(id))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    buildKBResponse(kb, nil),
	})
}

func validateKnowledgeBasePromptInstructions(kb *types.KnowledgeBase) error {
	if err := types.ValidateKnowledgeBasePromptInstructions(kb); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	return nil
}
