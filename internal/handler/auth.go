package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"reliquary/internal/errors"
	"reliquary/internal/handler/dto"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// AuthHandler reports the sole user and workspace attached by the auth middleware.
type AuthHandler struct {
	userService  interfaces.UserService
	spaceService interfaces.SpaceService
}

func NewAuthHandler(userService interfaces.UserService, spaceService interfaces.SpaceService,
) *AuthHandler {
	return &AuthHandler{
		userService:  userService,
		spaceService: spaceService,
	}
}

func (h *AuthHandler) GetCurrentUser(c *gin.Context) {
	ctx := c.Request.Context()

	// Get current user from service (which extracts from context)
	user, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		logger.Errorf(ctx, "Failed to get current user: %v", err)
		appErr := errors.NewUnauthorizedError("Failed to get user information").WithDetails(err.Error())
		c.Error(appErr)
		return
	}

	// Return metadata for the personal workspace.
	var space *types.Space
	activeSpaceID, _ := types.SpaceIDFromContext(ctx)
	if activeSpaceID == 0 {
		activeSpaceID = user.SpaceID
	}
	if activeSpaceID > 0 {
		space, err = h.spaceService.GetSpaceByID(ctx, activeSpaceID)
		if err != nil {
			logger.Warnf(ctx, "Failed to get space info for user %s, space ID %d: %v", user.Email, activeSpaceID, err)
			// Don't fail the request if space info is not available
		}
	}
	userInfo := user.ToUserInfo()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"user":  userInfo,
			"space": dto.NewSpaceResponse(ctx, space),
		},
	})
}
