package handler

import (
	stderrors "errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/errors"
	"reliquary/internal/handler/dto"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

// AuthHandler implements HTTP request handlers for user authentication
// Provides functionality for user registration, login, logout, and token management
// through the REST API endpoints
type AuthHandler struct {
	userService   interfaces.UserService
	tenantService interfaces.TenantService
}

func NewAuthHandler(userService interfaces.UserService, tenantService interfaces.TenantService,
) *AuthHandler {
	return &AuthHandler{
		userService:   userService,
		tenantService: tenantService,
	}
}

// Register godoc
func (h *AuthHandler) Register(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start user registration")

	if enabled, err := h.userService.HasUsers(ctx); err != nil || enabled {
		logger.Warn(ctx, "Registration rejected: a user already exists")
		appErr := errors.NewForbiddenError("Registration is closed")
		c.Error(appErr)
		return
	}

	var req types.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse registration request parameters", err)
		appErr := errors.NewValidationError("Invalid registration parameters").WithDetails(err.Error())
		c.Error(appErr)
		return
	}
	req.Username = secutils.SanitizeForLog(req.Username)
	req.Email = secutils.SanitizeForLog(req.Email)
	// Password is intentionally NOT sanitized: SanitizeForLog replaces
	// \n, \r, \t and strips other control characters so a string is safe
	// to write into a log line. Applying it to a real password would
	// silently rewrite the credential before hashing, so registration
	// would succeed but login with the original password would fail.
	// Passwords must never be logged, so they don't need that defence.

	// Validate required fields
	if req.Username == "" || req.Email == "" || req.Password == "" {
		logger.Error(ctx, "Missing required registration fields")
		appErr := errors.NewValidationError("Username, email and password are required")
		c.Error(appErr)
		return
	}

	// Validate basic password requirements, also enforced by the service.
	if err := service.ValidatePassword(req.Password); err != nil {
		logger.Error(ctx, "Invalid password")
		appErr := errors.NewValidationError(err.Error())
		_ = c.Error(appErr)
		return
	}

	req.Username = secutils.SanitizeForLog(req.Username)
	req.Email = secutils.SanitizeForLog(req.Email)
	// Call service to register user
	user, err := h.userService.Register(ctx, &req)
	if err != nil {
		logger.Errorf(ctx, "Failed to register user: %v", err)
		if stderrors.Is(err, repository.ErrRegistrationClosed) {
			c.Error(errors.NewForbiddenError("Registration is closed"))
			return
		}
		appErr := errors.NewBadRequestError(err.Error())
		c.Error(appErr)
		return
	}

	// Return success response
	response := &types.RegisterResponse{
		Success: true,
		Message: "Registration successful",
		User:    user,
	}

	logger.Infof(ctx, "User registered successfully: %s", secutils.SanitizeForLog(user.Email))
	c.JSON(http.StatusCreated, response)
}

// Login godoc
func (h *AuthHandler) Login(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start user login")

	var req types.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse login request parameters", err)
		appErr := errors.NewValidationError("Invalid login parameters").WithDetails(err.Error())
		c.Error(appErr)
		return
	}
	email := secutils.SanitizeForLog(req.Email)

	// Validate required fields
	if req.Email == "" || req.Password == "" {
		logger.Error(ctx, "Missing required login fields")
		appErr := errors.NewValidationError("Email and password are required")
		c.Error(appErr)
		return
	}

	// Call service to authenticate user
	response, err := h.userService.Login(ctx, &req)
	if err != nil {
		logger.Errorf(ctx, "Failed to login user: %v", err)
		appErr := errors.NewUnauthorizedError("Login failed").WithDetails(err.Error())
		c.Error(appErr)
		return
	}

	// Check if login was successful
	if !response.Success {
		logger.Warnf(ctx, "Login failed: %s", response.Message)
		c.JSON(http.StatusUnauthorized, dto.NewAuthLoginResponse(response))
		return
	}

	// User is already in the correct format from service

	logger.Infof(ctx, "User logged in successfully, email: %s", email)
	c.JSON(http.StatusOK, dto.NewAuthLoginResponse(response))
}

// Logout godoc
func (h *AuthHandler) Logout(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start user logout")

	// Extract token from Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		logger.Error(ctx, "Missing Authorization header")
		appErr := errors.NewValidationError("Authorization header is required")
		c.Error(appErr)
		return
	}

	// Parse Bearer token
	tokenParts := strings.Split(authHeader, " ")
	if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
		logger.Error(ctx, "Invalid Authorization header format")
		appErr := errors.NewValidationError("Invalid Authorization header format")
		c.Error(appErr)
		return
	}

	token := tokenParts[1]

	// Revoke every outstanding session for this user so refresh tokens
	// cannot keep working after logout.
	err := h.userService.Logout(ctx, token)
	if err != nil {
		logger.Errorf(ctx, "Failed to revoke token: %v", err)
		appErr := errors.NewInternalServerError("Logout failed").WithDetails(err.Error())
		c.Error(appErr)
		return
	}

	logger.Info(ctx, "User logged out successfully")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Logout successful",
	})
}

// RefreshToken godoc
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start token refresh")

	var req struct {
		RefreshToken string `json:"refreshToken" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse refresh token request", err)
		appErr := errors.NewValidationError("Invalid refresh token request").WithDetails(err.Error())
		c.Error(appErr)
		return
	}

	// Call service to refresh token
	accessToken, newRefreshToken, err := h.userService.RefreshToken(ctx, req.RefreshToken)
	if err != nil {
		logger.Errorf(ctx, "Failed to refresh token: %v", err)
		appErr := errors.NewUnauthorizedError("Token refresh failed").WithDetails(err.Error())
		c.Error(appErr)
		return
	}

	logger.Info(ctx, "Token refreshed successfully")
	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"message":       "Token refreshed successfully",
		"access_token":  accessToken,
		"refresh_token": newRefreshToken,
	})
}

// GetCurrentUser godoc
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
	var tenant *types.Tenant
	activeTenantID, _ := types.TenantIDFromContext(ctx)
	if activeTenantID == 0 {
		activeTenantID = user.TenantID
	}
	if activeTenantID > 0 {
		tenant, err = h.tenantService.GetTenantByID(ctx, activeTenantID)
		if err != nil {
			logger.Warnf(ctx, "Failed to get tenant info for user %s, tenant ID %d: %v", user.Email, activeTenantID, err)
			// Don't fail the request if tenant info is not available
		}
	}
	userInfo := user.ToUserInfo()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"user":   userInfo,
			"tenant": dto.NewTenantResponse(ctx, tenant),
		},
	})
}

// ChangePassword godoc
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start password change")

	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse password change request", err)
		appErr := errors.NewValidationError("Invalid password change request").WithDetails(err.Error())
		c.Error(appErr)
		return
	}

	// Get current user
	user, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		logger.Errorf(ctx, "Failed to get current user: %v", err)
		appErr := errors.NewUnauthorizedError("Failed to get user information").WithDetails(err.Error())
		c.Error(appErr)
		return
	}

	// Change password. Policy is enforced in the service after the old
	// password is verified so a wrong current credential is not masked
	// by a complexity error.
	err = h.userService.ChangePassword(ctx, user.ID, req.OldPassword, req.NewPassword)
	if err != nil {
		switch {
		case stderrors.Is(err, service.ErrInvalidPassword):
			appErr := errors.NewValidationError("Invalid password").
				WithDetails(service.DetailInvalidPassword)
			_ = c.Error(appErr)
			return
		case stderrors.Is(err, service.ErrInvalidOldPassword):
			appErr := errors.NewBadRequestError("Current password is incorrect").
				WithDetails(service.DetailInvalidOldPassword)
			c.Error(appErr)
			return
		case stderrors.Is(err, service.ErrSamePassword):
			appErr := errors.NewValidationError("New password must differ from current password").
				WithDetails(service.DetailSamePassword)
			c.Error(appErr)
			return
		default:
			logger.Errorf(ctx, "Failed to change password: %v", err)
			appErr := errors.NewBadRequestError("Password change failed").WithDetails(err.Error())
			c.Error(appErr)
			return
		}
	}

	logger.Infof(ctx, "Password changed successfully for user: %s", user.Email)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Password changed successfully",
	})
}

// GetAuthConfig godoc
//
// GetAuthConfig is intentionally a no-auth endpoint: the frontend reads
// it on app load to decide whether to show the Register tab and which
// password complexity rules to apply. We expose only what the UI
// strictly needs; other config stays internal.
func (h *AuthHandler) GetAuthConfig(c *gin.Context) {
	hasUsers, err := h.userService.HasUsers(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"success": true, "registration_enabled": err == nil && !hasUsers, "minimum_password_length": service.MinimumPasswordLength})
}

// tenantNameOrEmpty returns the tenant name when available.
func tenantNameOrEmpty(t *types.Tenant) string {
	if t == nil {
		return ""
	}
	return t.Name
}

// ValidateToken godoc
func (h *AuthHandler) ValidateToken(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start token validation")

	// Extract token from Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		logger.Error(ctx, "Missing Authorization header")
		appErr := errors.NewValidationError("Authorization header is required")
		c.Error(appErr)
		return
	}

	// Parse Bearer token
	tokenParts := strings.Split(authHeader, " ")
	if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
		logger.Error(ctx, "Invalid Authorization header format")
		appErr := errors.NewValidationError("Invalid Authorization header format")
		c.Error(appErr)
		return
	}

	token := tokenParts[1]

	// Validate token
	user, _, err := h.userService.ValidateToken(ctx, token)
	if err != nil {
		logger.Errorf(ctx, "Failed to validate token: %v", err)
		appErr := errors.NewUnauthorizedError("Token validation failed").WithDetails(err.Error())
		c.Error(appErr)
		return
	}

	logger.Infof(ctx, "Token validated successfully for user: %s", user.Email)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Token is valid",
		"user":    user.ToUserInfo(),
	})
}
