package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	"reliquary/internal/errors"
	"reliquary/internal/handler/dto"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

// ModelCredentialsHandler handles secret credentials for models via the
// dedicated /models/:id/credentials subresource. Credential values are written
// separately from model configuration and responses expose only presence.
//
// Recognized fields: "api_key" (every provider), "app_secret" (providers that require a signing secret).
type ModelCredentialsHandler struct {
	svc interfaces.ModelService
}

func NewModelCredentialsHandler(svc interfaces.ModelService) *ModelCredentialsHandler {
	return &ModelCredentialsHandler{svc: svc}
}

type modelCredentialsPutRequest struct {
	APIKey    *string `json:"api_key,omitempty"`
	AppSecret *string `json:"app_secret,omitempty"`
}

func (h *ModelCredentialsHandler) Put(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")
	spaceID := c.GetUint64(types.SpaceIDContextKey.String())
	if spaceID == 0 {
		c.Error(errors.NewBadRequestError("Workspace ID cannot be empty"))
		return
	}

	var req modelCredentialsPutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if req.APIKey == nil && req.AppSecret == nil {
		m, err := h.svc.GetModelByID(ctx, id)
		if err != nil || m == nil {
			c.Error(errors.NewNotFoundError("Model not found"))
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": dto.CredentialsResponse{
			Fields: map[string]dto.CredentialFieldMetadata{
				"api_key":    {Configured: m.Parameters.APIKey != ""},
				"app_secret": {Configured: m.Parameters.AppSecret != ""},
			},
		}})
		return
	}

	updated, err := h.svc.UpdateModelCredentials(ctx, id, req.APIKey, req.AppSecret)
	if err != nil {
		if err == service.ErrModelNotFound {
			c.Error(errors.NewNotFoundError("Model not found"))
			return
		}
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"model_id": secutils.SanitizeForLog(id)})
		c.Error(errors.NewInternalServerError("failed to update credentials: " + err.Error()))
		return
	}

	resp := dto.CredentialsResponse{
		Fields: map[string]dto.CredentialFieldMetadata{
			"api_key":    {Configured: updated.Parameters.APIKey != ""},
			"app_secret": {Configured: updated.Parameters.AppSecret != ""},
		},
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": resp})
}

func (h *ModelCredentialsHandler) DeleteField(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")
	field := c.Param("field")
	spaceID := c.GetUint64(types.SpaceIDContextKey.String())
	if spaceID == 0 {
		c.Error(errors.NewBadRequestError("Workspace ID cannot be empty"))
		return
	}
	if field != "api_key" && field != "app_secret" {
		c.Error(errors.NewBadRequestError("unknown credential field: " + secutils.SanitizeForLog(field)))
		return
	}
	if err := h.svc.ClearModelCredential(ctx, id, field); err != nil {
		if err == service.ErrModelNotFound {
			c.Error(errors.NewNotFoundError("Model not found"))
			return
		}
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id": secutils.SanitizeForLog(id),
			"field":    field,
		})
		c.Error(errors.NewInternalServerError("failed to clear credential: " + err.Error()))
		return
	}
	c.Status(http.StatusNoContent)
}
