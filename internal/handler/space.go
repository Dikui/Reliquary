package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"reliquary/internal/config"
	"reliquary/internal/errors"
	"reliquary/internal/handler/dto"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

// SpaceHandler implements HTTP request handlers for space management
// Provides functionality for creating, retrieving, updating, and deleting spaces
// through the REST API endpoints
type SpaceHandler struct {
	service          interfaces.SpaceService
	apiKeyService    interfaces.APIKeyService
	userService      interfaces.UserService
	libraryService   interfaces.LibraryService
	config           *config.Config
	systemSettingSvc interfaces.SystemSettingService
}

// NewSpaceHandler creates handlers for personal workspace settings and API keys.
func NewSpaceHandler(
	service interfaces.SpaceService,
	apiKeyService interfaces.APIKeyService,
	userService interfaces.UserService,
	libraryService interfaces.LibraryService,
	config *config.Config,
	systemSettingSvc interfaces.SystemSettingService,
) *SpaceHandler {
	return &SpaceHandler{
		service:          service,
		apiKeyService:    apiKeyService,
		userService:      userService,
		libraryService:   libraryService,
		config:           config,
		systemSettingSvc: systemSettingSvc,
	}
}

// updateSpaceRequest is the JSON body for PUT /space.
// It binds the name and description. Configuration fields use dedicated
// endpoints with their own validation.
//
// Pointers so we can distinguish "not sent" from "explicit empty
// string"; when nil we leave the existing column untouched.
type updateSpaceRequest struct {
	Name        *string `json:"name"        binding:"omitempty,min=1,max=128"`
	Description *string `json:"description" binding:"omitempty,max=512"`
}

type apiKeyCreateRequest struct {
	Name      string `json:"name"`
	ExpiresAt *int64 `json:"expires_at_unix"`
}

// apiKeyUpdateRequest 修改已创建 API Key 的配置，字段语义与创建接口一致。
type apiKeyUpdateRequest struct {
	Name      string `json:"name"`
	ExpiresAt *int64 `json:"expires_at_unix"`
}

type apiKeyResponse struct {
	Status     string     `json:"status"`
	ID         uint64     `json:"id"`
	Name       string     `json:"name"`
	APIKey     string     `json:"api_key"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type apiKeyCreateResponse struct {
	apiKeyResponse
	Token string `json:"token"`
}

// GetSpace godoc
func (h *SpaceHandler) GetSpace(c *gin.Context) {
	ctx := c.Request.Context()

	id := types.MustSpaceIDFromContext(ctx)

	space, err := h.service.GetSpaceByID(ctx, id)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			logger.Error(ctx, "Failed to retrieve workspace: application error", appErr)
			c.Error(appErr)
		} else {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("Failed to retrieve workspace").WithDetails(err.Error()))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    dto.NewSpaceResponse(ctx, space),
	})
}

// UpdateSpace godoc
func (h *SpaceHandler) UpdateSpace(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start updating space")

	id := types.MustSpaceIDFromContext(ctx)

	// Bind the name and description; configuration fields use dedicated
	// endpoints. Pointers distinguish omitted fields from explicit empty strings.
	var req updateSpaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}

	// Load the persisted space so any column the request omits keeps
	// its current value through the GORM `Updates(struct)` zero-skip
	// behaviour (we always pass back the full struct).
	existing, err := h.service.GetSpaceByID(ctx, id)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
		} else {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("Failed to load workspace").WithDetails(err.Error()))
		}
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			c.Error(errors.NewValidationError("name cannot be blank"))
			return
		}
		existing.Name = trimmed
	}
	if req.Description != nil {
		existing.Description = strings.TrimSpace(*req.Description)
	}

	logger.Infof(ctx, "Updating space, ID: %d, Name: %s", id, secutils.SanitizeForLog(existing.Name))

	updatedSpace, err := h.service.UpdateSpace(ctx, existing)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			logger.Error(ctx, "Failed to update workspace: application error", appErr)
			c.Error(appErr)
		} else {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("Failed to update workspace").WithDetails(err.Error()))
		}
		return
	}

	logger.Infof(
		ctx,
		"Space updated successfully, ID: %d, Name: %s",
		updatedSpace.ID,
		secutils.SanitizeForLog(updatedSpace.Name),
	)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    dto.NewSpaceResponse(ctx, updatedSpace),
	})
}

// ListAPIKeys godoc
func (h *SpaceHandler) ListAPIKeys(c *gin.Context) {
	ctx := c.Request.Context()
	id := types.MustSpaceIDFromContext(ctx)
	keys, err := h.apiKeyService.ListAPIKeys(ctx, id)
	if err != nil {
		c.Error(errors.NewInternalServerError("Failed to list API keys").WithDetails(err.Error()))
		return
	}
	resp := make([]apiKeyResponse, 0, len(keys))
	for _, key := range keys {
		resp = append(resp, apiKeyForResponse(key))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": resp})
}

// CreateAPIKey godoc
func (h *SpaceHandler) CreateAPIKey(c *gin.Context) {
	ctx := c.Request.Context()
	id := types.MustSpaceIDFromContext(ctx)
	var req apiKeyCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}
	if err := validateAPIKeyRequest(ctx, h.libraryService, id, req); err != nil {
		c.Error(err)
		return
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != 0 {
		t := time.Unix(*req.ExpiresAt, 0).UTC()
		if !t.After(time.Now().UTC()) {
			c.Error(errors.NewValidationError("expires_at_unix must be in the future"))
			return
		}
		expiresAt = &t
	}
	result, err := h.apiKeyService.CreateAPIKey(ctx, interfaces.APIKeyCreateRequest{
		SpaceID:   id,
		Name:      req.Name,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		c.Error(errors.NewInternalServerError("Failed to create API key").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data": apiKeyCreateResponse{
			apiKeyResponse: apiKeyForResponse(result.APIKey),
			Token:          result.Token,
		},
	})
}

// UpdateAPIKey 修改个人空间 API Key 的名称和到期时间。
// 字段校验与创建接口保持一致。
func (h *SpaceHandler) UpdateAPIKey(c *gin.Context) {
	ctx := c.Request.Context()
	spaceID := types.MustSpaceIDFromContext(ctx)
	keyID, err := strconv.ParseUint(c.Param("key_id"), 10, 64)
	if err != nil || keyID == 0 {
		c.Error(errors.NewBadRequestError("Invalid API key ID"))
		return
	}
	var req apiKeyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}
	if appErr := validateAPIKeyRequest(ctx, h.libraryService, spaceID, apiKeyCreateRequest(req)); appErr != nil {
		c.Error(appErr)
		return
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != 0 {
		t := time.Unix(*req.ExpiresAt, 0).UTC()
		expiresAt = &t
	}

	updated, err := h.apiKeyService.UpdateAPIKey(ctx, interfaces.APIKeyUpdateRequest{
		SpaceID: spaceID, APIKeyID: keyID, Name: req.Name, ExpiresAt: expiresAt,
	})
	if err != nil {
		c.Error(errors.NewNotFoundError("API key not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": apiKeyForResponse(updated)})
}

// DeleteAPIKey godoc
func (h *SpaceHandler) DeleteAPIKey(c *gin.Context) {
	ctx := c.Request.Context()
	spaceID := types.MustSpaceIDFromContext(ctx)
	keyID, err := strconv.ParseUint(c.Param("key_id"), 10, 64)
	if err != nil || keyID == 0 {
		c.Error(errors.NewBadRequestError("Invalid API key ID"))
		return
	}
	if err := h.apiKeyService.RevokeAPIKey(ctx, spaceID, keyID); err != nil {
		c.Error(errors.NewNotFoundError("API key not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func apiKeyForResponse(key *types.APIKey) apiKeyResponse {
	if key == nil {
		return apiKeyResponse{}
	}
	status := "active"
	if key.APIKey == "" && key.KeyHash == fmt.Sprintf("disabled-%d", key.ID) {
		status = "migration_invalidated"
	} else if key.RevokedAt != nil {
		status = "revoked"
	} else if key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now()) {
		status = "expired"
	}
	return apiKeyResponse{
		Status:     status,
		ID:         key.ID,
		Name:       key.Name,
		APIKey:     key.APIKey,
		LastUsedAt: key.LastUsedAt,
		ExpiresAt:  key.ExpiresAt,
		CreatedAt:  key.CreatedAt,
	}
}

func validateAPIKeyRequest(ctx context.Context, libraryService interfaces.LibraryService, spaceID uint64, req apiKeyCreateRequest) *errors.AppError {
	if strings.TrimSpace(req.Name) == "" {
		return errors.NewValidationError("name is required")
	}
	return nil
}

// GetSpaceKV godoc
func (h *SpaceHandler) GetSpaceKV(c *gin.Context) {
	ctx := c.Request.Context()
	key := secutils.SanitizeForLog(c.Param("key"))

	switch key {
	case "parser-engine-config":
		if !dto.CanViewIntegrationSecrets(ctx) {
			c.Error(errors.NewForbiddenError("integration configuration requires an authenticated user"))
			return
		}
	}

	switch key {
	case "parser-engine-config":
		h.GetSpaceParserEngineConfig(c)
		return
	case "retrieval-config":
		h.GetSpaceRetrievalConfig(c)
		return
	default:
		logger.Info(ctx, "KV key not supported", "key", key)
		c.Error(errors.NewBadRequestError("unsupported key"))
		return
	}
}

// UpdateSpaceKV godoc
func (h *SpaceHandler) UpdateSpaceKV(c *gin.Context) {
	ctx := c.Request.Context()
	key := secutils.SanitizeForLog(c.Param("key"))

	switch key {
	case "parser-engine-config":
		if !dto.CanViewIntegrationSecrets(ctx) {
			c.Error(errors.NewForbiddenError("integration configuration requires an authenticated user"))
			return
		}
	}

	switch key {
	case "parser-engine-config":
		h.updateSpaceParserEngineConfigInternal(c)
		return
	case "retrieval-config":
		h.updateSpaceRetrievalConfigInternal(c)
		return
	default:
		logger.Info(ctx, "KV key not supported", "key", key)
		c.Error(errors.NewBadRequestError("unsupported key"))
		return
	}
}

// GetSpaceParserEngineConfig returns the space's parser engine config (MinerU endpoint, API key, etc.).
func (h *SpaceHandler) GetSpaceParserEngineConfig(c *gin.Context) {
	ctx := c.Request.Context()
	space, _ := types.SpaceInfoFromContext(ctx)
	if space == nil {
		logger.Error(ctx, "Workspace is empty")
		c.Error(errors.NewBadRequestError("Workspace is empty"))
		return
	}
	data := types.ParserEngineConfigForResponse(space.ParserEngineConfig, true)
	if data == nil {
		data = &types.ParserEngineConfig{}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}

// updateSpaceParserEngineConfigInternal updates the space's parser engine config.
func (h *SpaceHandler) updateSpaceParserEngineConfigInternal(c *gin.Context) {
	ctx := c.Request.Context()
	var cfg types.ParserEngineConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}
	space, _ := types.SpaceInfoFromContext(ctx)
	if space == nil {
		logger.Error(ctx, "Workspace is empty")
		c.Error(errors.NewBadRequestError("Workspace is empty"))
		return
	}
	merged := types.MergeParserEngineConfigForUpdate(&cfg, space.ParserEngineConfig)
	if err := validateParserEngineOutboundURLs(merged); err != nil {
		c.Error(errors.NewValidationError(err.Error()))
		return
	}
	space.ParserEngineConfig = merged
	updatedSpace, err := h.service.UpdateSpace(ctx, space)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
		} else {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("Failed to update workspace parser engine config").WithDetails(err.Error()))
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    types.ParserEngineConfigForResponse(updatedSpace.ParserEngineConfig, true),
		"message": "解析引擎配置已更新",
	})
}

// GetPromptTemplates godoc
// GetSpaceRetrievalConfig returns the space's global retrieval configuration.
func (h *SpaceHandler) GetSpaceRetrievalConfig(c *gin.Context) {
	ctx := c.Request.Context()
	space, _ := types.SpaceInfoFromContext(ctx)
	if space == nil {
		logger.Error(ctx, "Workspace is empty")
		c.Error(errors.NewBadRequestError("Workspace is empty"))
		return
	}
	data := space.RetrievalConfig
	if data == nil {
		data = &types.RetrievalConfig{}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}

// updateSpaceRetrievalConfigInternal updates the space's global retrieval configuration.
func (h *SpaceHandler) updateSpaceRetrievalConfigInternal(c *gin.Context) {
	ctx := c.Request.Context()

	var cfg types.RetrievalConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}

	// Validate thresholds
	if cfg.RRFImageWeight < 0 || cfg.RRFImageWeight > 10 {
		c.Error(errors.NewBadRequestError("rrf_image_weight must be between 0 and 10"))
		return
	}
	if cfg.VectorThreshold < 0 || cfg.VectorThreshold > 1 {
		c.Error(errors.NewBadRequestError("vector_threshold must be between 0 and 1"))
		return
	}
	if cfg.KeywordThreshold < 0 || cfg.KeywordThreshold > 1 {
		c.Error(errors.NewBadRequestError("keyword_threshold must be between 0 and 1"))
		return
	}
	if cfg.SimilarReportsThreshold < 0 || cfg.SimilarReportsThreshold > 1 {
		c.Error(errors.NewBadRequestError("similar_reports_threshold must be between 0 and 1"))
		return
	}
	if cfg.SimilarReportsWindowDays < 0 || cfg.SimilarReportsWindowDays > 365 {
		c.Error(errors.NewBadRequestError("similar_reports_window_days must be between 0 and 365"))
		return
	}
	if cfg.RerankThreshold < -10 || cfg.RerankThreshold > 10 {
		c.Error(errors.NewBadRequestError("rerank_threshold must be between -10 and 10"))
		return
	}
	if cfg.EmbeddingTopK < 0 || cfg.EmbeddingTopK > 200 {
		c.Error(errors.NewBadRequestError("embedding_top_k must be between 0 and 200"))
		return
	}
	if cfg.RerankTopK < 0 || cfg.RerankTopK > 200 {
		c.Error(errors.NewBadRequestError("rerank_top_k must be between 0 and 200"))
		return
	}

	space, _ := types.SpaceInfoFromContext(ctx)
	if space == nil {
		logger.Error(ctx, "Workspace is empty")
		c.Error(errors.NewBadRequestError("Workspace is empty"))
		return
	}

	space.RetrievalConfig = &cfg
	updatedSpace, err := h.service.UpdateSpace(ctx, space)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
		} else {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("Failed to update retrieval config").WithDetails(err.Error()))
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    updatedSpace.RetrievalConfig,
		"message": "Retrieval configuration updated successfully",
	})
}

func validateParserEngineOutboundURLs(cfg *types.ParserEngineConfig) error {
	if cfg == nil {
		return nil
	}
	if endpoint := strings.TrimSpace(cfg.MinerUEndpoint); endpoint != "" {
		if err := secutils.ValidateURLForSSRF(endpoint); err != nil {
			return fmt.Errorf("mineru_endpoint failed SSRF validation: %v", err)
		}
	}
	if vlmURL := strings.TrimSpace(cfg.MinerUVLMServerURL); vlmURL != "" {
		if err := secutils.ValidateURLForSSRF(vlmURL); err != nil {
			return fmt.Errorf("mineru_vlm_server_url failed SSRF validation: %v", err)
		}
	}
	return nil
}
