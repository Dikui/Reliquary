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

// LibraryHandler defines the HTTP handler for library operations
type LibraryHandler struct {
	service interfaces.LibraryService
}

func NewLibraryHandler(service interfaces.LibraryService) *LibraryHandler {
	return &LibraryHandler{service: service}
}

func buildLibraryResponse(
	library *types.Library,
	extras map[string]interface{},
) interface{} {
	b, err := json.Marshal(library)
	if err != nil {
		return library
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return library
	}

	for k, v := range extras {
		m[k] = v
	}
	return m
}

// HybridSearch keeps the old route explicit while callers adopt article retrieval.
func (h *LibraryHandler) HybridSearch(c *gin.Context) {
	if _, _, _, err := h.validateAndGetLibrary(c); err != nil {
		c.Error(err)
		return
	}
	c.Error(apperrors.NewBadRequestError("chunk search and library scope are no longer supported; use POST /api/v1/search with query and article filters"))
}

func (h *LibraryHandler) validateAndGetLibrary(c *gin.Context) (*types.Library, string, uint64, error) {
	ctx := c.Request.Context()

	// Get space ID from context
	spaceID, exists := c.Get(types.SpaceIDContextKey.String())
	if !exists {
		logger.Error(ctx, "Failed to get space ID")
		return nil, "", 0, apperrors.NewUnauthorizedError("Unauthorized")
	}

	// Get library ID from URL parameter
	id := c.Param("id")
	var library *types.Library
	var err error
	if id == "" {
		library, err = h.service.GetLibrary(ctx)
	} else {
		library, err = h.service.GetLibraryByID(ctx, id)
	}
	if err != nil {
		// Missing IDs must retain their 404 response.
		if stderrors.Is(err, repository.ErrLibraryNotFound) {
			return nil, id, 0, apperrors.NewNotFoundError("library not found")
		}
		logger.ErrorWithFields(ctx, err, nil)
		return nil, id, 0, apperrors.NewInternalServerError(err.Error())
	}

	// Check 1: Verify space ownership (owner has full access)
	if library.SpaceID == spaceID.(uint64) {
		return library, library.ID, spaceID.(uint64), nil
	}

	// No permission: not owner and no shared access
	logger.Warnf(
		ctx,
		"Space has no permission to access this library, library ID: %s, "+
			"request space ID: %d, library space ID: %d",
		id, spaceID.(uint64), library.SpaceID,
	)
	return nil, id, 0, apperrors.NewForbiddenError("No permission to operate")
}

func (h *LibraryHandler) GetLibrary(c *gin.Context) {
	// Validate and get the library
	library, _, _, err := h.validateAndGetLibrary(c)
	if err != nil {
		c.Error(err)
		return
	}
	// Fill counts (entry_count, chunk_count, is_processing) so hover/detail shows correct numbers
	if fillErr := h.service.FillLibraryCounts(c.Request.Context(), library); fillErr != nil {
		logger.Warnf(c.Request.Context(), "Failed to fill Library counts for %s: %v", library.ID, fillErr)
	}
	var extras map[string]interface{}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": buildLibraryResponse(library, extras)})
}

// UpdateLibraryRequest defines the request body structure for updating a library
type UpdateLibraryRequest struct {
	Name        string                 `json:"name"        binding:"required"`
	Description string                 `json:"description"`
	Config      *types.LibraryConfig   `json:"config"`
	ModelTiers  *types.ModelTierConfig `json:"model_tiers"`
}

// UpdateLibrary godoc
func (h *LibraryHandler) UpdateLibrary(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start updating library")

	// Validate and get the library
	_, id, _, err := h.validateAndGetLibrary(c)
	if err != nil {
		c.Error(err)
		return
	}

	// Parse request body
	var req UpdateLibraryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(apperrors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}
	if req.ModelTiers != nil {
		if req.Config == nil {
			req.Config = &types.LibraryConfig{}
		}
		req.Config.ModelTiers = req.ModelTiers
	}

	logger.Infof(ctx, "Updating library, ID: %s, name: %s",
		secutils.SanitizeForLog(id), secutils.SanitizeForLog(req.Name))

	// Update the library
	library, err := h.service.UpdateLibrary(ctx, id, req.Name, req.Description, req.Config)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Library updated successfully, ID: %s",
		secutils.SanitizeForLog(id))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    buildLibraryResponse(library, nil),
	})
}
