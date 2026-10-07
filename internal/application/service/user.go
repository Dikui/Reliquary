package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"reliquary/internal/config"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var (
	jwtSecretOnce sync.Once
	jwtSecret     string

	// ErrUserEmailExists is returned by Register when the target entity's
	// email already exists.
	ErrUserEmailExists = errors.New("user with this email already exists")

	// ErrUserUsernameExists is returned by Register when the target entity's
	// username already exists.
	ErrUserUsernameExists = errors.New("user with this username already exists")

	// ErrPasswordPolicy is returned when a newly chosen password does not
	// meet the product's public 8-32 character, letter-and-number contract.
	// It is exported so HTTP handlers can translate the failure to a 400
	// without exposing bcrypt or persistence errors.
	ErrPasswordPolicy = errors.New(
		"password must be 8-32 characters and contain at least one letter and one number")
	// ErrComplexPasswordPolicy is returned when the runtime complex-password
	// switch is on and the new password is missing a required character class.
	ErrComplexPasswordPolicy = errors.New(
		"password must be 8-32 characters and must contain uppercase and lowercase " +
			"letters, numbers, and special characters")

	// ErrInvalidOldPassword is returned by ChangePassword when the supplied
	// current password does not match the stored hash. Handlers map this to
	// a 400 so callers can prompt the user without treating it as a 500.
	ErrInvalidOldPassword = errors.New("invalid old password")

	// ErrSamePassword is returned when the new password equals the current
	// one so callers can reject no-op rotations that would still revoke
	// every session.
	ErrSamePassword = errors.New("new password must differ from current password")
)

// Machine-readable change-password failure reasons for HTTP details fields.
const (
	DetailInvalidOldPassword = "invalid_old_password"
	DetailPasswordPolicy     = "password_policy"
	DetailSamePassword       = "same_password"
)

// getJwtSecret retrieves the JWT secret from the environment, falling back to a securely generated random secret.
func getJwtSecret() string {
	jwtSecretOnce.Do(func() {
		if envSecret := strings.TrimSpace(os.Getenv("JWT_SECRET")); envSecret != "" {
			jwtSecret = envSecret
			return
		}

		randomBytes := make([]byte, 32)
		if _, err := rand.Read(randomBytes); err != nil {
			panic(fmt.Sprintf("failed to generate JWT secret: %v", err))
		}
		jwtSecret = base64.StdEncoding.EncodeToString(randomBytes)
	})

	return jwtSecret
}

// userService implements the UserService interface
type userService struct {
	userRepo         interfaces.UserRepository
	tokenRepo        interfaces.AuthTokenRepository
	tenantService    interfaces.TenantService
	memberService    interfaces.TenantMemberService
	config           *config.Config
	systemSettingSvc interfaces.SystemSettingService
}

// NewUserService creates a new user service instance
func NewUserService(
	configInfo *config.Config,
	userRepo interfaces.UserRepository,
	tokenRepo interfaces.AuthTokenRepository,
	tenantService interfaces.TenantService,
	memberService interfaces.TenantMemberService,
	systemSettingSvc interfaces.SystemSettingService,
) interfaces.UserService {
	return &userService{
		userRepo:         userRepo,
		tokenRepo:        tokenRepo,
		tenantService:    tenantService,
		memberService:    memberService,
		config:           configInfo,
		systemSettingSvc: systemSettingSvc,
	}
}

func (s *userService) complexPasswordEnabled(ctx context.Context) bool {
	return ResolveComplexPasswordEnabled(ctx, s.config, s.systemSettingSvc)
}

// Register creates a new user account
func (s *userService) Register(ctx context.Context, req *types.RegisterRequest) (*types.User, error) {
	logger.Info(ctx, "Start user registration")

	// Validate input
	if req.Username == "" || req.Email == "" || req.Password == "" {
		return nil, errors.New("username, email and password are required")
	}

	// Check if user already exists
	existingUser, _ := s.userRepo.GetUserByEmail(ctx, req.Email)
	if existingUser != nil {
		return nil, ErrUserEmailExists
	}

	existingUser, _ = s.userRepo.GetUserByUsername(ctx, req.Username)
	if existingUser != nil {
		return nil, ErrUserUsernameExists
	}

	if err := ValidatePasswordPolicy(req.Password, s.complexPasswordEnabled(ctx)); err != nil {
		return nil, err
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		logger.Errorf(ctx, "Failed to hash password: %v", err)
		return nil, errors.New("failed to process password")
	}

	user := &types.User{
		ID: uuid.New().String(), Username: req.Username, Email: req.Email,
		PasswordHash: string(hashedPassword), TenantID: types.SingleTenantID,
		IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.userRepo.RegisterTeamUser(ctx, user, req.InvitationToken); err != nil {
		return nil, err
	}

	logger.Info(ctx, "User registered successfully")
	return user, nil
}

// Login authenticates a user and returns tokens
func (s *userService) Login(ctx context.Context, req *types.LoginRequest) (*types.LoginResponse, error) {
	logger.Info(ctx, "Start user login")
	// Get user by email
	user, err := s.userRepo.GetUserByEmail(ctx, req.Email)
	if err != nil {
		logger.Errorf(ctx, "Failed to get user by email: %v", err)
		return &types.LoginResponse{
			Success: false,
			Message: "Invalid email or password",
		}, nil
	}
	if user == nil {
		logger.Warn(ctx, "User not found for email")
		return &types.LoginResponse{
			Success: false,
			Message: "Invalid email or password",
		}, nil
	}

	// Check if user is active
	if !user.IsActive {
		logger.Warn(ctx, "User account is disabled")
		return &types.LoginResponse{
			Success: false,
			Message: "Account is disabled",
		}, nil
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		logger.Warn(ctx, "Password verification failed")
		return &types.LoginResponse{
			Success: false,
			Message: "Invalid email or password",
		}, nil
	}
	if user.TenantID != types.SingleTenantID || len(s.buildMembershipsForUser(ctx, user, nil)) == 0 {
		return &types.LoginResponse{Success: false, Message: "Not an active team member"}, nil
	}
	logger.Info(ctx, "Password verification successful")

	// Issue tokens for the team workspace.
	logger.Info(ctx, "Generating tokens")
	resolvedTenantID := types.SingleTenantID
	accessToken, refreshToken, err := s.generateTokensForTenant(ctx, user, resolvedTenantID)
	if err != nil {
		logger.Errorf(ctx, "Failed to generate tokens: %v", err)
		return &types.LoginResponse{
			Success: false,
			Message: "Login failed",
		}, nil
	}
	logger.Info(ctx, "Tokens generated successfully")

	// Include team metadata in the login response.
	var tenant *types.Tenant
	if resolvedTenantID > 0 {
		tenant, err = s.tenantService.GetTenantByID(ctx, resolvedTenantID)
		if err != nil {
			logger.Warn(ctx, "Failed to get tenant info")
		} else {
			logger.Info(ctx, "Tenant information retrieved successfully")
		}
	}

	memberships := s.buildMembershipsForUser(ctx, user, tenant)

	logger.Info(ctx, "User logged in successfully")
	return &types.LoginResponse{
		Success:      true,
		Message:      "Login successful",
		User:         user,
		ActiveTenant: tenant,
		Memberships:  memberships,
		Token:        accessToken,
		RefreshToken: refreshToken,
	}, nil
}

// BuildLoginMemberships projects the caller's active team role for the response.
func (s *userService) BuildLoginMemberships(
	ctx context.Context,
	user *types.User,
	activeTenant *types.Tenant,
) []types.Membership {
	return s.buildMembershipsForUser(ctx, user, activeTenant)
}

func (s *userService) buildMembershipsForUser(ctx context.Context, user *types.User, tenant *types.Tenant) []types.Membership {
	memberships := []types.Membership{}
	if s.memberService == nil || user == nil || user.TenantID != types.SingleTenantID {
		return memberships
	}
	member, err := s.memberService.GetMembership(ctx, user.ID, types.SingleTenantID)
	if err != nil || member == nil || member.Status != types.TenantMemberStatusActive || !member.Role.IsValid() {
		return memberships
	}
	name := ""
	if tenant != nil {
		name = tenant.Name
	}
	return append(memberships, types.Membership{TenantID: types.SingleTenantID, TenantName: name, Role: member.Role})
}

// GetUserByID gets a user by ID
func (s *userService) GetUserByID(ctx context.Context, id string) (*types.User, error) {
	return s.userRepo.GetUserByID(ctx, id)
}

// GetUsersByIDs proxies to the repository batch fetch. Returns an empty
// map for an empty input; missing ids are absent from the result.
func (s *userService) GetUsersByIDs(ctx context.Context, ids []string) (map[string]*types.User, error) {
	return s.userRepo.GetUsersByIDs(ctx, ids)
}

// GetUserByEmail gets a user by email
func (s *userService) GetUserByEmail(ctx context.Context, email string) (*types.User, error) {
	return s.userRepo.GetUserByEmail(ctx, email)
}

// GetUserByUsername gets a user by username
func (s *userService) GetUserByUsername(ctx context.Context, username string) (*types.User, error) {
	return s.userRepo.GetUserByUsername(ctx, username)
}

// GetUserByTenantID gets the first user (owner) of a tenant
func (s *userService) GetUserByTenantID(ctx context.Context, tenantID uint64) (*types.User, error) {
	return s.userRepo.GetUserByTenantID(ctx, tenantID)
}

// UpdateUser updates user information
func (s *userService) UpdateUser(ctx context.Context, user *types.User) error {
	user.UpdatedAt = time.Now()
	return s.userRepo.UpdateUser(ctx, user)
}

// ListSystemAdmins lists users with IsSystemAdmin=true. Thin pass-through
// to the repository; the handler enforces SystemAdmin gating, so the
// service does not duplicate the role check here.
func (s *userService) ListSystemAdmins(
	ctx context.Context, offset, limit int,
) ([]*types.User, int64, error) {
	return s.userRepo.ListSystemAdmins(ctx, offset, limit)
}

// RevokeSystemAdmin removes system-admin privileges through the
// repository's transactional guard so concurrent revokes cannot remove
// the final administrator.
func (s *userService) RevokeSystemAdmin(ctx context.Context, userID, actorID string) (*types.User, error) {
	return s.userRepo.RevokeSystemAdmin(ctx, userID, actorID)
}

// DeleteUser deletes a user
func (s *userService) DeleteUser(ctx context.Context, id string) error {
	return s.userRepo.DeleteUser(ctx, id)
}

// ChangePassword changes user password after verifying the current
// credential. The new password must satisfy ValidatePasswordPolicy so
// self-service rotation cannot introduce weaker passwords than
// registration / admin reset allow. On success every outstanding session
// is revoked so a stolen token cannot survive the rotation.
func (s *userService) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	// Verify old password before policy checks so callers with a wrong
	// current credential get a clear failure instead of a policy error.
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return ErrInvalidOldPassword
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(newPassword)); err == nil {
		return ErrSamePassword
	}

	if err := ValidatePasswordPolicy(newPassword, s.complexPasswordEnabled(ctx)); err != nil {
		return err
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.PasswordHash = string(hashedPassword)
	user.UpdatedAt = time.Now()

	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return err
	}

	// Invalidate every outstanding session so a stolen token cannot
	// survive a password rotation.
	return s.tokenRepo.RevokeTokensByUserID(ctx, userID)
}

// AdminResetPassword replaces a user's password without checking the previous
// credential. Authorization and the cannot-reset-self rule live at the system
// admin HTTP boundary; this service owns the security-critical persistence and
// session invalidation so no caller can accidentally update only one of them.
func (s *userService) AdminResetPassword(ctx context.Context, userID, newPassword string) error {
	if err := ValidatePasswordPolicy(newPassword, s.complexPasswordEnabled(ctx)); err != nil {
		return err
	}

	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.PasswordHash = string(hashedPassword)
	user.UpdatedAt = time.Now()
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return err
	}

	return s.tokenRepo.RevokeTokensByUserID(ctx, userID)
}

// ValidatePassword validates user password
func (s *userService) ValidatePassword(ctx context.Context, userID string, password string) error {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	return bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
}

// GenerateTokens issues access and refresh tokens for the team workspace.
func (s *userService) GenerateTokens(
	ctx context.Context,
	user *types.User,
) (accessToken, refreshToken string, err error) {
	return s.generateTokensForTenant(ctx, user, types.SingleTenantID)
}

// generateTokensForTenant persists the tokens for one team identity.
func (s *userService) generateTokensForTenant(
	ctx context.Context,
	user *types.User,
	activeTenantID uint64,
) (accessToken, refreshToken string, err error) {
	// Generate access token (expires in 24 hours)
	accessClaims := jwt.MapClaims{
		"user_id":   user.ID,
		"email":     user.Email,
		"tenant_id": activeTenantID,
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
		"iat":       time.Now().Unix(),
		"type":      "access",
	}

	accessTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessToken, err = accessTokenObj.SignedString([]byte(getJwtSecret()))
	if err != nil {
		return "", "", err
	}

	// Generate refresh token (expires in 7 days)
	refreshClaims := jwt.MapClaims{
		"user_id": user.ID,
		"exp":     time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
		"type":    "refresh",
	}

	refreshTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshToken, err = refreshTokenObj.SignedString([]byte(getJwtSecret()))
	if err != nil {
		return "", "", err
	}

	// Store tokens in database
	accessTokenRecord := &types.AuthToken{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Token:     accessToken,
		TokenType: "access_token",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	refreshTokenRecord := &types.AuthToken{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Token:     refreshToken,
		TokenType: "refresh_token",
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_ = s.tokenRepo.CreateToken(ctx, accessTokenRecord)
	_ = s.tokenRepo.CreateToken(ctx, refreshTokenRecord)

	return accessToken, refreshToken, nil
}

// ValidateToken requires an active account and a token for the team workspace.
func (s *userService) ValidateToken(ctx context.Context, tokenString string) (*types.User, uint64, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(getJwtSecret()), nil
	})

	if err != nil || !token.Valid {
		return nil, 0, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, 0, errors.New("invalid token claims")
	}

	userID, ok := claims["user_id"].(string)
	if !ok {
		return nil, 0, errors.New("invalid user ID in token")
	}

	if isRefreshTokenClaims(claims) {
		return nil, 0, errors.New("refresh token cannot be used as access token")
	}

	// Check if token is revoked
	tokenRecord, err := s.tokenRepo.GetTokenByValue(ctx, tokenString)
	if err != nil || tokenRecord == nil || tokenRecord.IsRevoked {
		return nil, 0, errors.New("token is revoked")
	}
	if tokenRecord.TokenType == "refresh_token" {
		return nil, 0, errors.New("refresh token cannot be used as access token")
	}

	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, 0, err
	}

	// Extract active tenant from the JWT. Anything missing or unparseable
	// falls back to the user's home tenant so old tokens (and tokens issued
	// by code paths that don't yet set the claim) keep working.
	activeTenantID := tenantIDFromClaims(claims, user.TenantID)
	if activeTenantID != types.SingleTenantID || user.TenantID != types.SingleTenantID {
		return nil, 0, errors.New("invalid team workspace")
	}

	return user, activeTenantID, nil
}

func isRefreshTokenClaims(claims jwt.MapClaims) bool {
	tokenType, ok := claims["type"].(string)
	return ok && tokenType == "refresh"
}

func userIDFromSignedToken(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(getJwtSecret()), nil
	}, jwt.WithoutClaimsValidation())
	if err != nil || token == nil || !token.Valid {
		return "", errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid token claims")
	}

	userID, ok := claims["user_id"].(string)
	if !ok || strings.TrimSpace(userID) == "" {
		return "", errors.New("invalid user ID in token")
	}
	return userID, nil
}

// tenantIDFromClaims pulls the active tenant ID out of a parsed JWT
// claim map. Returns fallback when the claim is missing or has an
// unrecognised type. Extracted as a free function so it can be unit
// tested without standing up the full userService dependency graph.
//
// JSON numbers come back as float64 from jwt.MapClaims; the int64 /
// uint64 branches cover legacy code paths and tests that build claims
// directly. Negative values are treated as missing.
func tenantIDFromClaims(claims jwt.MapClaims, fallback uint64) uint64 {
	raw, ok := claims["tenant_id"]
	if !ok {
		return fallback
	}
	switch v := raw.(type) {
	case float64:
		if v > 0 {
			return uint64(v)
		}
	case int64:
		if v > 0 {
			return uint64(v)
		}
	case uint64:
		if v > 0 {
			return v
		}
	}
	return fallback
}

// RefreshToken refreshes access token using refresh token
func (s *userService) RefreshToken(
	ctx context.Context,
	refreshTokenString string,
) (accessToken, newRefreshToken string, err error) {
	token, err := jwt.Parse(refreshTokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(getJwtSecret()), nil
	})

	if err != nil || !token.Valid {
		return "", "", errors.New("invalid refresh token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", errors.New("invalid token claims")
	}

	tokenType, ok := claims["type"].(string)
	if !ok || tokenType != "refresh" {
		return "", "", errors.New("not a refresh token")
	}

	userID, ok := claims["user_id"].(string)
	if !ok {
		return "", "", errors.New("invalid user ID in token")
	}

	// Check if token is revoked
	tokenRecord, err := s.tokenRepo.GetTokenByValue(ctx, refreshTokenString)
	if err != nil || tokenRecord == nil || tokenRecord.IsRevoked {
		return "", "", errors.New("refresh token is revoked")
	}
	if tokenRecord.TokenType != "refresh_token" {
		return "", "", errors.New("not a refresh token")
	}

	// Get user
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return "", "", err
	}

	// Revoke old refresh token
	tokenRecord.IsRevoked = true
	_ = s.tokenRepo.UpdateToken(ctx, tokenRecord)

	// Generate new tokens
	return s.GenerateTokens(ctx, user)
}

// Logout invalidates every outstanding session for the user identified by
// the presented JWT. Access and refresh tokens are both accepted so clients
// can end the session without refreshing first; expired tokens are allowed
// so logout still works after the access token TTL.
func (s *userService) Logout(ctx context.Context, tokenString string) error {
	userID, err := userIDFromSignedToken(tokenString)
	if err != nil {
		return err
	}
	return s.tokenRepo.RevokeTokensByUserID(ctx, userID)
}

// RevokeToken revokes a token
func (s *userService) RevokeToken(ctx context.Context, tokenString string) error {
	tokenRecord, err := s.tokenRepo.GetTokenByValue(ctx, tokenString)
	if err != nil {
		return err
	}

	tokenRecord.IsRevoked = true
	tokenRecord.UpdatedAt = time.Now()

	return s.tokenRepo.UpdateToken(ctx, tokenRecord)
}

// GetCurrentUser gets current user from context
func (s *userService) GetCurrentUser(ctx context.Context) (*types.User, error) {
	user, ok := ctx.Value(types.UserContextKey).(*types.User)
	if !ok {
		return nil, errors.New("user not found in context")
	}

	return user, nil
}

// SearchUsers searches users by username or email
func (s *userService) SearchUsers(ctx context.Context, query string, limit int) ([]*types.User, error) {
	if query == "" {
		return []*types.User{}, nil
	}
	return s.userRepo.SearchUsers(ctx, query, limit)
}

// rsaPublicKey rebuilds an *rsa.PublicKey from the base64url modulus/exponent of
// a JWK (RFC 7518 §6.3.1).
func decodeJWKBase64(value string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(value); err == nil && len(b) > 0 {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(value)
}

func (s *userService) HasTeamUsers(ctx context.Context) (bool, error) {
	return s.userRepo.HasTeamUsers(ctx)
}
