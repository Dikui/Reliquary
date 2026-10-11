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

// TenantHandler implements HTTP request handlers for tenant management
// Provides functionality for creating, retrieving, updating, and deleting tenants
// through the REST API endpoints
type TenantHandler struct {
	service          interfaces.TenantService
	apiKeyService    interfaces.TenantAPIKeyService
	userService      interfaces.UserService
	kbService        interfaces.KnowledgeBaseService
	config           *config.Config
	systemSettingSvc interfaces.SystemSettingService
}

// NewTenantHandler creates handlers for personal workspace settings and API keys.
func NewTenantHandler(
	service interfaces.TenantService,
	apiKeyService interfaces.TenantAPIKeyService,
	userService interfaces.UserService,
	kbService interfaces.KnowledgeBaseService,
	config *config.Config,
	systemSettingSvc interfaces.SystemSettingService,
) *TenantHandler {
	return &TenantHandler{
		service:          service,
		apiKeyService:    apiKeyService,
		userService:      userService,
		kbService:        kbService,
		config:           config,
		systemSettingSvc: systemSettingSvc,
	}
}

// updateTenantRequest is the JSON body for PUT /space.
// It binds the name and description. Configuration fields use dedicated
// endpoints with their own validation.
//
// Pointers so we can distinguish "not sent" from "explicit empty
// string"; when nil we leave the existing column untouched.
type updateTenantRequest struct {
	Name        *string `json:"name"        binding:"omitempty,min=1,max=128"`
	Description *string `json:"description" binding:"omitempty,max=512"`
}

type tenantAPIKeyCreateRequest struct {
	Name      string `json:"name"`
	ExpiresAt *int64 `json:"expires_at_unix"`
}

// tenantAPIKeyUpdateRequest 修改已创建 API Key 的配置，字段语义与创建接口一致。
type tenantAPIKeyUpdateRequest struct {
	Name      string `json:"name"`
	ExpiresAt *int64 `json:"expires_at_unix"`
}

type tenantAPIKeyResponse struct {
	ID         uint64     `json:"id"`
	Name       string     `json:"name"`
	APIKey     string     `json:"api_key"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type tenantAPIKeyCreateResponse struct {
	tenantAPIKeyResponse
	Token string `json:"token"`
}

// GetTenant godoc
func (h *TenantHandler) GetTenant(c *gin.Context) {
	ctx := c.Request.Context()

	id := types.MustTenantIDFromContext(ctx)

	tenant, err := h.service.GetTenantByID(ctx, id)
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
		"data":    dto.NewTenantResponse(ctx, tenant),
	})
}

// UpdateTenant godoc
func (h *TenantHandler) UpdateTenant(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start updating tenant")

	id := types.MustTenantIDFromContext(ctx)

	// Bind the name and description; configuration fields use dedicated
	// endpoints. Pointers distinguish omitted fields from explicit empty strings.
	var req updateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}

	// Load the persisted tenant so any column the request omits keeps
	// its current value through the GORM `Updates(struct)` zero-skip
	// behaviour (we always pass back the full struct).
	existing, err := h.service.GetTenantByID(ctx, id)
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

	logger.Infof(ctx, "Updating tenant, ID: %d, Name: %s", id, secutils.SanitizeForLog(existing.Name))

	updatedTenant, err := h.service.UpdateTenant(ctx, existing)
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
		"Tenant updated successfully, ID: %d, Name: %s",
		updatedTenant.ID,
		secutils.SanitizeForLog(updatedTenant.Name),
	)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    dto.NewTenantResponse(ctx, updatedTenant),
	})
}

// ListAPIKeys godoc
func (h *TenantHandler) ListAPIKeys(c *gin.Context) {
	ctx := c.Request.Context()
	id := types.MustTenantIDFromContext(ctx)
	keys, err := h.apiKeyService.ListAPIKeys(ctx, id)
	if err != nil {
		c.Error(errors.NewInternalServerError("Failed to list API keys").WithDetails(err.Error()))
		return
	}
	resp := make([]tenantAPIKeyResponse, 0, len(keys))
	for _, key := range keys {
		resp = append(resp, tenantAPIKeyForResponse(key))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": resp})
}

// CreateAPIKey godoc
func (h *TenantHandler) CreateAPIKey(c *gin.Context) {
	ctx := c.Request.Context()
	id := types.MustTenantIDFromContext(ctx)
	var req tenantAPIKeyCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}
	if err := validateTenantAPIKeyRequest(ctx, h.kbService, id, req); err != nil {
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
	result, err := h.apiKeyService.CreateAPIKey(ctx, interfaces.TenantAPIKeyCreateRequest{
		TenantID:  id,
		Name:      req.Name,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		c.Error(errors.NewInternalServerError("Failed to create API key").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data": tenantAPIKeyCreateResponse{
			tenantAPIKeyResponse: tenantAPIKeyForResponse(result.APIKey),
			Token:                result.Token,
		},
	})
}

// UpdateAPIKey 修改个人空间 API Key 的名称和到期时间。
// 字段校验与创建接口保持一致。
func (h *TenantHandler) UpdateAPIKey(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := types.MustTenantIDFromContext(ctx)
	keyID, err := strconv.ParseUint(c.Param("key_id"), 10, 64)
	if err != nil || keyID == 0 {
		c.Error(errors.NewBadRequestError("Invalid API key ID"))
		return
	}
	var req tenantAPIKeyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}
	if appErr := validateTenantAPIKeyRequest(ctx, h.kbService, tenantID, tenantAPIKeyCreateRequest(req)); appErr != nil {
		c.Error(appErr)
		return
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != 0 {
		t := time.Unix(*req.ExpiresAt, 0).UTC()
		expiresAt = &t
	}

	updated, err := h.apiKeyService.UpdateAPIKey(ctx, interfaces.TenantAPIKeyUpdateRequest{
		TenantID: tenantID, APIKeyID: keyID, Name: req.Name, ExpiresAt: expiresAt,
	})
	if err != nil {
		c.Error(errors.NewNotFoundError("API key not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": tenantAPIKeyForResponse(updated)})
}

// DeleteAPIKey godoc
func (h *TenantHandler) DeleteAPIKey(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := types.MustTenantIDFromContext(ctx)
	keyID, err := strconv.ParseUint(c.Param("key_id"), 10, 64)
	if err != nil || keyID == 0 {
		c.Error(errors.NewBadRequestError("Invalid API key ID"))
		return
	}
	if err := h.apiKeyService.RevokeAPIKey(ctx, tenantID, keyID); err != nil {
		c.Error(errors.NewNotFoundError("API key not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func tenantAPIKeyForResponse(key *types.TenantAPIKey) tenantAPIKeyResponse {
	if key == nil {
		return tenantAPIKeyResponse{}
	}
	return tenantAPIKeyResponse{
		ID:         key.ID,
		Name:       key.Name,
		APIKey:     key.APIKey,
		LastUsedAt: key.LastUsedAt,
		ExpiresAt:  key.ExpiresAt,
		CreatedAt:  key.CreatedAt,
	}
}

func validateTenantAPIKeyRequest(ctx context.Context, kbService interfaces.KnowledgeBaseService, tenantID uint64, req tenantAPIKeyCreateRequest) *errors.AppError {
	if strings.TrimSpace(req.Name) == "" {
		return errors.NewValidationError("name is required")
	}
	return nil
}

// GetTenantKV godoc
func (h *TenantHandler) GetTenantKV(c *gin.Context) {
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
		h.GetTenantParserEngineConfig(c)
		return
	case "retrieval-config":
		h.GetTenantRetrievalConfig(c)
		return
	default:
		logger.Info(ctx, "KV key not supported", "key", key)
		c.Error(errors.NewBadRequestError("unsupported key"))
		return
	}
}

// UpdateTenantKV godoc
func (h *TenantHandler) UpdateTenantKV(c *gin.Context) {
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
		h.updateTenantParserEngineConfigInternal(c)
		return
	case "retrieval-config":
		h.updateTenantRetrievalConfigInternal(c)
		return
	default:
		logger.Info(ctx, "KV key not supported", "key", key)
		c.Error(errors.NewBadRequestError("unsupported key"))
		return
	}
}

// GetTenantParserEngineConfig returns the tenant's parser engine config (MinerU endpoint, API key, etc.).
func (h *TenantHandler) GetTenantParserEngineConfig(c *gin.Context) {
	ctx := c.Request.Context()
	tenant, _ := types.TenantInfoFromContext(ctx)
	if tenant == nil {
		logger.Error(ctx, "Workspace is empty")
		c.Error(errors.NewBadRequestError("Workspace is empty"))
		return
	}
	data := types.ParserEngineConfigForResponse(tenant.ParserEngineConfig, true)
	if data == nil {
		data = &types.ParserEngineConfig{}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}

// updateTenantParserEngineConfigInternal updates the tenant's parser engine config.
func (h *TenantHandler) updateTenantParserEngineConfigInternal(c *gin.Context) {
	ctx := c.Request.Context()
	var cfg types.ParserEngineConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}
	tenant, _ := types.TenantInfoFromContext(ctx)
	if tenant == nil {
		logger.Error(ctx, "Workspace is empty")
		c.Error(errors.NewBadRequestError("Workspace is empty"))
		return
	}
	merged := types.MergeParserEngineConfigForUpdate(&cfg, tenant.ParserEngineConfig)
	if err := validateParserEngineOutboundURLs(merged); err != nil {
		c.Error(errors.NewValidationError(err.Error()))
		return
	}
	tenant.ParserEngineConfig = merged
	updatedTenant, err := h.service.UpdateTenant(ctx, tenant)
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
		"data":    types.ParserEngineConfigForResponse(updatedTenant.ParserEngineConfig, true),
		"message": "解析引擎配置已更新",
	})
}

// GetPromptTemplates godoc
// GetTenantRetrievalConfig returns the tenant's global retrieval configuration.
func (h *TenantHandler) GetTenantRetrievalConfig(c *gin.Context) {
	ctx := c.Request.Context()
	tenant, _ := types.TenantInfoFromContext(ctx)
	if tenant == nil {
		logger.Error(ctx, "Workspace is empty")
		c.Error(errors.NewBadRequestError("Workspace is empty"))
		return
	}
	data := tenant.RetrievalConfig
	if data == nil {
		data = &types.RetrievalConfig{}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}

// updateTenantRetrievalConfigInternal updates the tenant's global retrieval configuration.
func (h *TenantHandler) updateTenantRetrievalConfigInternal(c *gin.Context) {
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

	tenant, _ := types.TenantInfoFromContext(ctx)
	if tenant == nil {
		logger.Error(ctx, "Workspace is empty")
		c.Error(errors.NewBadRequestError("Workspace is empty"))
		return
	}

	tenant.RetrievalConfig = &cfg
	updatedTenant, err := h.service.UpdateTenant(ctx, tenant)
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
		"data":    updatedTenant.RetrievalConfig,
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
