package handler

import (
	"encoding/json"
	stderrors "errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/storageurl"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

// KnowledgeBaseHandler defines the HTTP handler for knowledge base operations
type KnowledgeBaseHandler struct {
	service interfaces.KnowledgeBaseService
	// fileService back the optional `resource_urls=public`
	// mode on hybrid-search. Both may be nil in tests, in which case only the
	// default handle mode is available.
	fileService interfaces.FileService
}

// NewKnowledgeBaseHandler creates a new knowledge base handler instance
func NewKnowledgeBaseHandler(
	service interfaces.KnowledgeBaseService,
	fileService interfaces.FileService,
) *KnowledgeBaseHandler {
	return &KnowledgeBaseHandler{
		service:     service,
		fileService: fileService,
	}
}

// resolveResourceRewriter builds the storage-reference rewriter for one response
// from the request's `resource_urls` parameter, falling back to the deployment
// default. The returned error is already an AppError the caller can hand to
// c.Error: a rejected scope is a 403, a typo in the parameter is a 400.
func (h *KnowledgeBaseHandler) resolveResourceRewriter(c *gin.Context) (*storageurl.Rewriter, error) {
	ctx := c.Request.Context()
	mode, err := storageurl.ResolveMode(ctx, c.Query(storageurl.QueryParam))
	if err != nil {
		if stderrors.Is(err, storageurl.ErrPublicModeForbidden) {
			return nil, apperrors.NewForbiddenError(err.Error())
		}
		return nil, apperrors.NewBadRequestError(err.Error())
	}
	return storageurl.NewRequestRewriter(ctx, mode, h.fileService), nil
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

// HybridSearch godoc
func (h *KnowledgeBaseHandler) HybridSearch(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start hybrid search")

	// Validate and check permission for knowledge base access
	_, id, effectiveTenantID, err := h.validateAndGetKnowledgeBase(c)
	if err != nil {
		c.Error(err)
		return
	}

	// Parse request body
	var req types.SearchParams
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(apperrors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}
	precomputedVectorOnly := len(req.QueryEmbedding) > 0 && req.DisableKeywordsMatch && !req.DisableVectorMatch
	if strings.TrimSpace(req.QueryText) == "" && !precomputedVectorOnly {
		_ = c.Error(apperrors.NewBadRequestError("query_text is required"))
		return
	}

	logger.Infof(ctx, "Executing hybrid search, knowledge base ID: %s, query: %s, effectiveTenantID: %d",
		secutils.SanitizeForLog(id), secutils.SanitizeForLog(req.QueryText), effectiveTenantID)

	// Resolve before retrieving so a typo or a rejected scope costs nothing.
	rewriter, err := h.resolveResourceRewriter(c)
	if err != nil {
		logger.Warnf(ctx, "Rejected resource URL mode: %v", err)
		_ = c.Error(err)
		return
	}

	results, err := h.service.HybridSearch(ctx, id, req)
	if err != nil {
		// Preserve typed application errors from the service.
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Hybrid search completed, knowledge base ID: %s, result count: %d",
		secutils.SanitizeForLog(id), len(results))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    rewriter.CopyReferences(ctx, results),
	})
}

func (h *KnowledgeBaseHandler) validateAndGetKnowledgeBase(c *gin.Context) (*types.KnowledgeBase, string, uint64, error) {
	ctx := c.Request.Context()

	// Get tenant ID from context
	tenantID, exists := c.Get(types.TenantIDContextKey.String())
	if !exists {
		logger.Error(ctx, "Failed to get tenant ID")
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

	// Check 1: Verify tenant ownership (owner has full access)
	if kb.TenantID == tenantID.(uint64) {
		return kb, kb.ID, tenantID.(uint64), nil
	}

	// No permission: not owner and no shared access
	logger.Warnf(
		ctx,
		"Tenant has no permission to access this knowledge base, knowledge base ID: %s, "+
			"request tenant ID: %d, knowledge base tenant ID: %d",
		id, tenantID.(uint64), kb.TenantID,
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
		if req.Config.ImageProcessingConfig != nil {
			probe.ImageProcessingConfig = *req.Config.ImageProcessingConfig
		}
		if req.Config.VLMConfig != nil {
			probe.VLMConfig = *req.Config.VLMConfig
		}
		if req.Config.QuestionGenerationConfig != nil {
			probe.QuestionGenerationConfig = req.Config.QuestionGenerationConfig
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
