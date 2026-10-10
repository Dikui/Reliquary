package handler

import (
	stderrors "errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type ResourceFavoriteHandler struct {
	service interfaces.ResourceFavoriteService
}

func NewResourceFavoriteHandler(svc interfaces.ResourceFavoriteService) *ResourceFavoriteHandler {
	return &ResourceFavoriteHandler{service: svc}
}

// favoriteContext scopes favorites to the personal space for login and API Key callers.
func favoriteContext(c *gin.Context) (uint64, bool) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.Error(apperrors.NewUnauthorizedError("workspace ID not found"))
		return 0, false
	}
	return tenantID, true
}

// ListFavorites godoc
func (h *ResourceFavoriteHandler) ListFavorites(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, ok := favoriteContext(c)
	if !ok {
		return
	}
	resourceType := c.Query("type")

	list, err := h.service.List(ctx, tenantID, resourceType)
	if err != nil {
		if stderrors.Is(err, service.ErrFavoriteInvalidType) {
			c.Error(apperrors.NewBadRequestError(err.Error()))
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// AddFavoriteRequest is the body for POST /user/favorites — kept symmetric
// with the DELETE path parameters so the frontend can build a single API
// helper that switches verb on toggle.
type AddFavoriteRequest struct {
	ResourceType string `json:"type"`
	ResourceID   string `json:"id"`
}

// AddFavorite godoc
func (h *ResourceFavoriteHandler) AddFavorite(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, ok := favoriteContext(c)
	if !ok {
		return
	}
	var req AddFavoriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid request body").WithDetails(err.Error()))
		return
	}
	if err := h.service.Add(ctx, tenantID, req.ResourceType, req.ResourceID); err != nil {
		if stderrors.Is(err, service.ErrFavoriteInvalidType) || stderrors.Is(err, service.ErrFavoriteEmptyID) || stderrors.Is(err, service.ErrFavoriteInvalidTag) {
			c.Error(apperrors.NewBadRequestError(err.Error()))
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RemoveFavorite godoc
func (h *ResourceFavoriteHandler) RemoveFavorite(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, ok := favoriteContext(c)
	if !ok {
		return
	}
	resourceType := c.Param("type")
	resourceID := c.Param("id")

	if err := h.service.Remove(ctx, tenantID, resourceType, resourceID); err != nil {
		if stderrors.Is(err, service.ErrFavoriteInvalidType) || stderrors.Is(err, service.ErrFavoriteEmptyID) {
			c.Error(apperrors.NewBadRequestError(err.Error()))
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
