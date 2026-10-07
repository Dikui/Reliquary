package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	jwt "github.com/golang-jwt/jwt/v5"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"reliquary/internal/config"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

const (
	defaultExternalUserIDHeader    = "X-External-User-ID"
	defaultExternalUserTokenHeader = "X-External-User-Token"
	maxExternalUserIDLen           = 128
	maxExternalUserTokenTTL        = 24 * time.Hour
)

var (
	errMissingDirectHeader      = errors.New("missing external user id header")
	errInvalidExternalUserID    = errors.New("invalid external user id")
	errInvalidExternalUserToken = errors.New("invalid external user token")
)

// 无需认证的API列表
var noAuthAPI = map[string][]string{
	"/health":               {"GET"},
	"/api/v1/auth/register": {"POST"},
	"/api/v1/auth/login":    {"POST"},
	// Share-link surfaces accept a plaintext invite token from anonymous
	// callers (an invitee who hasn't registered yet). They are registered
	// as public routes in RegisterAuthRoutes and rate-limited by IP, so the
	// global Auth middleware must let them through — otherwise opening a
	// share link while logged out 401s and the frontend bounces the user to
	// /login instead of the register page (issue #1617).
	"/api/v1/auth/invitations/lookup": {"POST"},
	"/api/v1/auth/register-by-invite": {"POST"},
	"/api/v1/auth/config":             {"GET"},
	// MCP OAuth provider redirect: the third-party authorization server
	// redirects the browser here without a Reliquary bearer token. The request
	// is authenticated by the opaque, single-use `state` parameter instead.
	"/api/v1/mcp-oauth/callback": {"GET"},
	"/api/v1/auth/refresh":       {"POST"},
	// Feishu commonly issue a HEAD request
	// before GET to validate Content-Type / Content-Length when rendering
	// image previews — both verbs must be allowed for image links to work.
	"/api/v1/files/presigned": {"GET", "HEAD"},
}

// 检查请求是否在无需认证的API列表中
func isNoAuthAPI(path string, method string) bool {
	for api, methods := range noAuthAPI {
		// 如果以*结尾，按照前缀匹配，否则按照全路径匹配
		if strings.HasSuffix(api, "*") {
			if strings.HasPrefix(path, strings.TrimSuffix(api, "*")) && slices.Contains(methods, method) {
				return true
			}
		} else if path == api && slices.Contains(methods, method) {
			return true
		}
	}
	return false
}

// Auth 认证中间件。按顺序尝试三条通道：
//
//  1. 白名单（isNoAuthAPI）/ OPTIONS 预检 —— 直接放行；
//  2. Bearer JWT —— 成功则走 authenticateJWTUser 完成空间/角色解析；
//     校验失败不立即拒绝，继续尝试 X-API-Key（保持既有兼容行为：
//     携带过期 JWT 但同时带有效 API key 的客户端仍可通过）；
//  3. X-API-Key —— authenticateAPIKeyRequest。
//
// 三条通道都未命中时返回 401；若调用方提交过 Bearer token，错误消息
// 明确指出 token 无效而不是笼统的 "missing authentication"，方便客户端
// 区分「没登录」和「登录态过期」。
func Auth(
	tenantService interfaces.TenantService,
	userService interfaces.UserService,
	memberService interfaces.TenantMemberService,
	apiKeyService interfaces.TenantAPIKeyService,
	cfg *config.Config,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ignore OPTIONS request
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		// 检查请求是否在无需认证的API列表中
		if isNoAuthAPI(c.Request.URL.Path, c.Request.Method) {
			c.Next()
			return
		}

		// 尝试JWT Token认证
		bearerPresented := false
		if token, ok := bearerToken(c); ok {
			bearerPresented = true
			user, jwtTenantID, err := userService.ValidateToken(c.Request.Context(), token)
			if err == nil && user != nil {
				if authenticateJWTUser(c, tenantService, memberService, cfg, user, jwtTenantID) {
					c.Next()
				}
				return
			}
			logger.Warnf(c.Request.Context(), "[auth] bearer token rejected: %v", err)
		}

		// 尝试X-API-Key认证（兼容模式）
		if apiKey := c.GetHeader("X-API-Key"); apiKey != "" {
			if apiKeyService == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: API key service is not configured"})
				c.Abort()
				return
			}
			if authenticateAPIKeyRequest(c, tenantService, userService, apiKeyService, apiKey) {
				c.Next()
			}
			return
		}

		// 没有任何通道认证成功
		if bearerPresented {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: invalid or expired token"})
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: missing authentication"})
		}
		c.Abort()
	}
}

// bearerToken extracts the Bearer token from the Authorization header.
func bearerToken(c *gin.Context) (string, bool) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		return "", false
	}
	return strings.TrimPrefix(authHeader, "Bearer "), true
}

// authenticateJWTUser finishes authentication for a validated JWT user:
// it resolves the target tenant (X-Tenant-ID switch / JWT claim / first
// active membership), resolves the caller's role inside that tenant, and
// attaches the session context. Returns true when the request may proceed;
// on false the response has already been written and the request aborted.
func authenticateJWTUser(
	c *gin.Context,
	tenantService interfaces.TenantService,
	memberService interfaces.TenantMemberService,
	cfg *config.Config,
	user *types.User,
	jwtTenantID uint64,
) bool {
	ctx := c.Request.Context()

	targetTenantID, tenant, crossTenantSwitch, ok := resolveTargetTenant(c, tenantService, memberService, cfg, user, jwtTenantID)
	if !ok {
		return false
	}

	// 获取空间信息（X-Tenant-ID 切换路径已在 resolveTargetTenant 内取到，
	// 避免二次查库）。
	if tenant == nil {
		var err error
		tenant, err = tenantService.GetTenantByID(ctx, targetTenantID)
		if err != nil || tenant == nil {
			logger.Warnf(ctx, "[auth] tenant lookup failed: tenant=%d user=%s err=%v", targetTenantID, user.ID, err)
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Unauthorized: invalid workspace",
			})
			c.Abort()
			return false
		}
	}

	// 解析当前空间内的角色 (issue #1303)
	role, ok := resolveTenantRole(ctx, memberService, user, targetTenantID, crossTenantSwitch, cfg)
	if !ok {
		// 强制 RBAC 时，缺少 active membership 即拒绝；fail-open 路径已在
		// resolveTenantRole 内部处理。
		logger.Warnf(ctx, "User %s has no active membership in tenant %d", user.ID, targetTenantID)
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Forbidden: not a member of the target workspace",
		})
		c.Abort()
		return false
	}

	logger.Infof(ctx,
		"[auth] resolved role=%s for user=%s in tenant=%d (jwt_tenant=%d, header=%q, cross_switch=%v)",
		role, user.ID, targetTenantID, jwtTenantID, c.GetHeader("X-Tenant-ID"), crossTenantSwitch)
	applyAuthSession(c, authSession{
		User:        user,
		Principal:   types.Principal{Type: types.PrincipalWebUser, ID: user.ID},
		TenantID:    targetTenantID,
		Tenant:      tenant,
		Role:        role,
		SystemAdmin: user.IsSystemAdmin,
	})
	return true
}

// resolveTargetTenant requires both the user and token to belong to the team.
// A legacy X-Tenant-ID header is accepted only when it names that same team.
func resolveTargetTenant(c *gin.Context, tenantService interfaces.TenantService, memberService interfaces.TenantMemberService, cfg *config.Config, user *types.User, jwtTenantID uint64) (uint64, *types.Tenant, bool, bool) {
	if user == nil || user.TenantID != types.SingleTenantID || jwtTenantID != types.SingleTenantID {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid team workspace"})
		c.Abort()
		return 0, nil, false, false
	}
	if raw := c.GetHeader("X-Tenant-ID"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid team workspace header"})
			c.Abort()
			return 0, nil, false, false
		}
		if id != types.SingleTenantID {
			c.JSON(http.StatusForbidden, gin.H{"error": "only the team workspace is available"})
			c.Abort()
			return 0, nil, false, false
		}
	}
	return types.SingleTenantID, nil, false, true
}

func authenticateAPIKeyRequest(
	c *gin.Context,
	tenantService interfaces.TenantService,
	userService interfaces.UserService,
	apiKeyService interfaces.TenantAPIKeyService,
	apiKey string,
) bool {
	ctx := c.Request.Context()
	// AuthenticateAPIKey resolves the key by SHA-256 hash (see startup
	// BackfillMissingKeyHashes for legacy placeholder rows).
	key, err := apiKeyService.AuthenticateAPIKey(ctx, apiKey)
	if err != nil || key == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: invalid API key"})
		c.Abort()
		return false
	}

	tenantID := key.TenantIDValue()
	if tenantID != types.SingleTenantID {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid team API key scope"})
		c.Abort()
		return false
	}
	if raw := c.GetHeader("X-Tenant-ID"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id != types.SingleTenantID {
			c.JSON(http.StatusForbidden, gin.H{"error": "only the team workspace is available"})
			c.Abort()
			return false
		}
	}
	attachAPIKeyAuthContext(c, tenantService, userService, types.SingleTenantID, key)

	if c.IsAborted() {
		return false
	}
	// Per-route API-key authorization (full access + capabilities + KB scope)
	// is enforced by middleware.APIKeyRouteAuthorizer on the /api/v1 group.
	// Key-management and any other undeclared route is denied there.
	return true
}

func attachAPIKeyAuthContext(
	c *gin.Context,
	tenantService interfaces.TenantService,
	userService interfaces.UserService,
	tenantID uint64,
	key *types.TenantAPIKey,
) {
	t, err := tenantService.GetTenantByID(c.Request.Context(), tenantID)
	if err != nil {
		logger.Warnf(c.Request.Context(), "[auth] API key tenant lookup failed: tenant=%d err=%v", tenantID, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: invalid API key"})
		c.Abort()
		return
	}

	var user *types.User
	var principal types.Principal

	user, err = userService.GetUserByTenantID(c.Request.Context(), tenantID)
	if err != nil || user == nil {
		user = &types.User{
			ID:       fmt.Sprintf("system-%d", tenantID),
			Username: fmt.Sprintf("system-%d", tenantID),
			Email:    fmt.Sprintf("system-%d@api-key.local", tenantID),
			TenantID: tenantID,
			IsActive: true,
		}
		logger.Infof(c.Request.Context(),
			"No user found for tenant %d via API key, using synthetic system user %s", tenantID, user.ID)
	}

	var principalErr error
	principal, principalErr = resolveAPIPrincipal(c.Request.Context(), t, c.Request.Header)
	if principalErr != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": apiPrincipalAuthErrorMessage(principalErr)})
		c.Abort()
		return
	}
	// This role context exists only for legacy guard compatibility after
	// RequireRole short-circuits API-key principals. The API key's real
	// authority is FullAccess + Capabilities + KnowledgeBaseIDs.
	apiKeyTenantRoleContext := types.TenantRoleViewer
	fullAccess := key != nil && key.FullAccess
	if fullAccess {
		apiKeyTenantRoleContext = types.TenantRoleOwner
	}
	session := authSession{
		User:      user,
		Principal: principal,
		TenantID:  tenantID,
		Tenant:    t,
		Role:      apiKeyTenantRoleContext,
	}
	if key != nil {
		session.APIKeyScope = &types.TenantAPIKeyScope{
			KeyID:            key.ID,
			FullAccess:       fullAccess,
			KnowledgeBaseIDs: key.KnowledgeBaseIDs,
			Capabilities:     key.Capabilities,
		}
	}
	applyAuthSession(c, session)
}

func resolveAPIPrincipal(ctx context.Context, tenant *types.Tenant, header http.Header) (types.Principal, error) {
	tenantID := uint64(0)
	if tenant != nil {
		tenantID = tenant.ID
	}
	fallback := types.Principal{
		Type: types.PrincipalAPITenant,
		ID:   strconv.FormatUint(tenantID, 10),
	}
	if tenant == nil || tenantID == 0 {
		return fallback, nil
	}
	cfg := tenant.APIPrincipalConfig
	if cfg == nil || cfg.Mode == "" || cfg.Mode == types.APIPrincipalModeTenant {
		return fallback, nil
	}
	switch cfg.Mode {
	case types.APIPrincipalModeDirect:
		externalUserID := strings.TrimSpace(header.Get(defaultExternalUserIDHeader))
		if externalUserID == "" {
			if cfg.RequireDirectHeader {
				return types.Principal{}, errMissingDirectHeader
			}
			return fallback, nil
		}
		if err := validateExternalUserID(externalUserID); err != nil {
			return types.Principal{}, fmt.Errorf("%w: %v", errInvalidExternalUserID, err)
		}
		return types.Principal{
			Type: types.PrincipalAPIExternalUser,
			ID:   strconv.FormatUint(tenantID, 10) + ":" + externalUserID,
		}, nil
	case types.APIPrincipalModeSignedToken:
		externalUserID, err := verifyExternalUserJWT(header.Get(defaultExternalUserTokenHeader), tenantID, cfg.HMACSecret)
		if err != nil || externalUserID == "" {
			logger.Warnf(ctx, "invalid external user token for tenant=%d: %v", tenantID, err)
			return types.Principal{}, fmt.Errorf("%w: %w", errInvalidExternalUserToken, err)
		}
		if err := validateExternalUserID(externalUserID); err != nil {
			return types.Principal{}, fmt.Errorf("%w: %v", errInvalidExternalUserID, err)
		}
		return types.Principal{
			Type: types.PrincipalAPIExternalUser,
			ID:   strconv.FormatUint(tenantID, 10) + ":" + externalUserID,
		}, nil
	default:
		return fallback, nil
	}
}

func verifyExternalUserJWT(tokenString string, tenantID uint64, secret string) (string, error) {
	tokenString = strings.TrimSpace(tokenString)
	secret = strings.TrimSpace(secret)
	if tokenString == "" {
		return "", errors.New("missing external user token")
	}
	if secret == "" {
		return "", errors.New("external user token secret is not configured")
	}
	claims := jwt.MapClaims{}
	parser := jwt.NewParser(
		jwt.WithAudience("reliquary"),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	token, err := parser.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return "", err
	}
	if token == nil || !token.Valid {
		return "", errors.New("invalid external user token")
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return "", errors.New("missing expiration")
	}
	if time.Until(exp.Time) > maxExternalUserTokenTTL {
		return "", fmt.Errorf("token lifetime exceeds %s", maxExternalUserTokenTTL)
	}
	if nbf, nbfErr := claims.GetNotBefore(); nbfErr == nil && nbf != nil && time.Now().Before(nbf.Time) {
		return "", errors.New("token not yet valid")
	}
	if got := principalTenantIDFromClaims(claims); got != tenantID {
		return "", fmt.Errorf("workspace mismatch: got %d want %d", got, tenantID)
	}
	sub, _ := claims["sub"].(string)
	sub = strings.TrimSpace(sub)
	if sub == "" {
		return "", errors.New("missing subject")
	}
	return sub, nil
}

func validateExternalUserID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("empty external user id")
	}
	if len(id) > maxExternalUserIDLen {
		return fmt.Errorf("external user id too long (max %d)", maxExternalUserIDLen)
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return errors.New("external user id contains invalid characters")
		}
	}
	return nil
}

func apiPrincipalAuthErrorMessage(err error) string {
	switch {
	case errors.Is(err, errMissingDirectHeader):
		return "Unauthorized: missing external user id header"
	case errors.Is(err, errInvalidExternalUserID):
		return "Unauthorized: invalid external user id"
	case errors.Is(err, errInvalidExternalUserToken):
		return "Unauthorized: invalid external user token"
	default:
		return "Unauthorized: invalid external user token"
	}
}

func principalTenantIDFromClaims(claims jwt.MapClaims) uint64 {
	v, ok := claims["tenant_id"]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		if t <= 0 {
			return 0
		}
		return uint64(t)
	case int64:
		if t <= 0 {
			return 0
		}
		return uint64(t)
	case uint64:
		return t
	case json.Number:
		n, err := strconv.ParseUint(t.String(), 10, 64)
		if err != nil {
			return 0
		}
		return n
	case string:
		n, err := strconv.ParseUint(strings.TrimSpace(t), 10, 64)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// resolveTenantRole determines the caller's TenantRole inside targetTenantID.
//
// Order of resolution:
//  1. Active TenantMember row → return that role.
//  2. Cross-tenant superuser switch (X-Tenant-ID with CanAccessAllTenants=true)
//     → grant Admin in the target tenant. Org admins are intentionally not
//     promoted to Owner; tenant deletion / API-key rotation should always
//     stay with a real Owner inside the target tenant. Cross-tenant access
//     is also never allowed to trigger the orphan-tenant auto-promotion
//     below — a superuser only visits, never claims ownership.
//  3. No membership but the tenant currently has zero active members AND
//     the caller is authenticating into their own home tenant (i.e.
//     targetTenantID == user.TenantID and this is not a cross-tenant
//     switch). This is the API-key-only orphan-tenant self-heal path:
//     the registrant becomes Owner of the tenant their own user record
//     points to. Any other path (cross-tenant switch, JWT minted for a
//     foreign tenant, etc.) is intentionally excluded to avoid silent
//     ownership grabs.
//  4. Otherwise → return ok=false. Caller decides:
//     - When EnableRBAC=true (or cfg unavailable): treat as 403.
//     - When EnableRBAC=false: fail open with Admin so existing deployments
//     don't break in the rollout window where memberships might lag user
//     records.
//
// The boolean second return value reports whether enforcement should reject
// the request. It is true whenever a usable role was found OR fail-open
// applies; false only when we want callers to abort with 403.
func resolveTenantRole(ctx context.Context, memberService interfaces.TenantMemberService, user *types.User, targetTenantID uint64, crossTenantSwitch bool, cfg *config.Config) (types.TenantRole, bool) {
	if memberService == nil || user == nil || targetTenantID != types.SingleTenantID {
		return "", false
	}
	member, err := memberService.GetMembership(ctx, user.ID, types.SingleTenantID)
	if err != nil || member == nil || member.Status != types.TenantMemberStatusActive || !member.Role.IsValid() {
		return "", false
	}
	return member.Role, true
}
