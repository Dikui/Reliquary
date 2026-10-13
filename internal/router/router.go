package router

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/dig"

	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/logger"
	"reliquary/internal/mcpserver"
	"reliquary/internal/middleware"
	"reliquary/internal/types/interfaces"
)

// RouterParams 路由参数
type RouterParams struct {
	dig.In

	Config                  *config.Config
	FileService             interfaces.FileService
	UserService             interfaces.UserService
	LibraryService          interfaces.LibraryService
	EntryService            interfaces.EntryService
	ChunkService            interfaces.ChunkService
	ModelService            interfaces.ModelService
	LibraryHandler          *handler.LibraryHandler
	EntryHandler            *handler.EntryHandler
	VaultHandler            *handler.VaultHandler       `optional:"true"`
	MaintenanceHandler      *handler.MaintenanceHandler `optional:"true"`
	SearchHandler           *handler.SearchHandler
	EntityHandler           *handler.EntityHandler
	EmbeddingRebuildHandler *handler.EmbeddingRebuildHandler `optional:"true"`
	TopicHandler            *handler.TopicHandler            `optional:"true"`
	FollowHandler           *handler.FollowHandler           `optional:"true"`
	OperationLogHandler     *handler.OperationLogHandler     `optional:"true"`
	MCPHandler              *mcpserver.Handler               `optional:"true"`
	SpaceHandler            *handler.SpaceHandler
	SpaceService            interfaces.SpaceService
	APIKeyService           interfaces.APIKeyService
	ChunkHandler            *handler.ChunkHandler
	ChatGPTHandler          *handler.ChatGPTHandler       `optional:"true"`
	ChatGPTPolicyHandler    *handler.ChatGPTPolicyHandler `optional:"true"`
	ModelHandler            *handler.ModelHandler
	ModelCredentialsHandler *handler.ModelCredentialsHandler
	AuthHandler             *handler.AuthHandler
	InitializationHandler   *handler.InitializationHandler
	SystemHandler           *handler.SystemHandler
	ResourceCatalog         interfaces.ResourceCatalog
}

// NewRouter 创建新的路由
func NewRouter(params RouterParams) *gin.Engine {
	r := gin.New()
	r.ContextWithFallback = true

	// Trusted proxies: gin defaults to trusting ALL proxies, which makes
	// c.ClientIP() honor a client-supplied X-Forwarded-For. Public, unauthed
	// embed endpoints rate-limit per (channel, ClientIP), so a spoofed XFF would
	// trivially bypass the limiter. Restrict to the fronting proxy network so
	// only the real client IP (appended by nginx) is returned. Configurable via
	// RELIQUARY_TRUSTED_PROXIES (comma-separated CIDRs/IPs).
	if err := r.SetTrustedProxies(trustedProxies()); err != nil {
		logger.Errorf(context.Background(), "[Router] failed to set trusted proxies: %v", err)
	}

	// CORS 中间件应放在最前面。
	// 注意：通配符 AllowOrigins 下浏览器会拒绝一切带凭据（cookie）的跨域
	// 请求（CORS 规范禁止 "*" 与 credentials 组合），因此 AllowCredentials
	// 实际只对未来改为回显具体 Origin 时才生效；当前认证全部走显式的
	// Authorization / X-API-Key 头，不依赖 ambient 凭据。若引入 cookie
	// 认证，必须先把 AllowOrigins 换成受控清单。
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-API-Key", "X-Request-ID"},
		ExposeHeaders:    []string{"Content-Length", "Access-Control-Allow-Origin"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// 基础中间件（不需要认证）
	r.Use(middleware.RequestID())
	r.Use(middleware.Language())
	r.Use(middleware.Logger())
	r.Use(middleware.Recovery())
	r.Use(middleware.ErrorHandler())

	// 健康检查（不需要认证）
	r.GET("/health", func(c *gin.Context) {
		response := gin.H{"status": "ok"}
		if params.SystemHandler != nil {
			response["queue_connection"] = params.SystemHandler.QueueConnectionHealth()
		}
		c.JSON(200, response)
	})

	if params.ChatGPTHandler != nil {
		r.GET("/auth/callback", params.ChatGPTHandler.Callback)
		r.GET("/auth/chatgpt/continue", params.ChatGPTHandler.ContinueLogin)
	}
	serveResourceGrants(r, params.ResourceCatalog, params.SpaceService, params.FileService)

	// 认证中间件
	r.Use(middleware.Auth(params.SpaceService, params.UserService, params.APIKeyService))
	if params.MaintenanceHandler != nil {
		r.Use(params.MaintenanceHandler.CollectionGate)
	}
	if params.MCPHandler != nil {
		r.Any("/mcp", gin.WrapH(params.MCPHandler))
	}

	// 本地文件访问代理（需要认证）
	serveFilesWithResources(r, params.FileService, params.ResourceCatalog)

	// Presigned file access: no auth required, signature-verified.
	servePresignedFiles(r, params.SpaceService)

	// Diagnostic preview of presigned URLs behind authentication middleware.
	servePresignedPreview(r, params.Config, params.ResourceCatalog)

	// 需要认证的API路由
	v1 := r.Group("/api/v1")
	{
		accessGuards := newAccessGuards(params.LibraryService, params.EntryService, params.ChunkService)

		RegisterAuthRoutes(v1, params.AuthHandler, accessGuards)
		RegisterSpaceRoutes(v1, params.SpaceHandler, accessGuards)
		RegisterLibraryRoutes(v1, params.LibraryHandler, accessGuards)
		serveLibraryScopedFiles(
			v1,
			accessGuards,
			params.SpaceService,
			params.FileService,
			params.ResourceCatalog)
		RegisterEntryRoutes(v1, params.EntryHandler, accessGuards)
		if params.VaultHandler != nil {
			v1.GET("/vault/status", params.VaultHandler.Status)
			v1.POST("/reports", params.VaultHandler.CreateReport)
			v1.GET("/entries/:id/vault-attachment", accessGuards.LibraryExistsFromEntryIDParam("id"), params.VaultHandler.Attachment)
		}
		if params.MaintenanceHandler != nil {
			v1.GET("/maintenance/status", params.MaintenanceHandler.Status)
			v1.PUT("/maintenance/configure", params.MaintenanceHandler.Configure)
		}
		v1.POST("/search", params.SearchHandler.SearchEntry)
		v1.GET("/search/options", params.SearchHandler.Options)
		RegisterEntityRoutes(v1, params.EntityHandler)
		RegisterEmbeddingRebuildRoutes(v1, params.EmbeddingRebuildHandler)
		if params.TopicHandler != nil {
			RegisterTopicRoutes(v1, params.TopicHandler, accessGuards)
		}
		if params.FollowHandler != nil {
			RegisterFollowRoutes(v1, params.FollowHandler)
		}
		if params.OperationLogHandler != nil {
			v1.GET("/operation-logs", params.OperationLogHandler.List)
		}
		RegisterChunkRoutes(v1, params.ChunkHandler, accessGuards)
		RegisterChatGPTRoutes(v1, params.ChatGPTHandler, params.ChatGPTPolicyHandler)
		RegisterModelRoutes(v1, params.ModelHandler, params.ModelCredentialsHandler, accessGuards)
		RegisterInitializationRoutes(v1, params.InitializationHandler, accessGuards)
		params.SystemHandler.BindDeploymentCapabilities(deploymentCapabilitiesFromRouter(params))
		RegisterSystemRoutes(v1, params.SystemHandler, accessGuards)
		RegisterChunkerDebugRoutes(v1, accessGuards)

		// Fail fast if any declared API-key policy points at a route
		// template that does not actually exist (typo / path drift). A
		// stale template would silently 403 every API key on that route,
		// so we panic at startup instead of shipping a dead policy.
	}

	return r
}

// trustedProxies returns the proxy CIDRs/IPs whose X-Forwarded-For headers
// gin should trust when resolving the client IP. Defaults to loopback and
// private ranges (covers the bundled nginx in a container network); override
// with RELIQUARY_TRUSTED_PROXIES (comma-separated). An explicit empty value
// disables proxy trust entirely so ClientIP() returns the direct peer.
func trustedProxies() []string {
	raw, ok := os.LookupEnv("RELIQUARY_TRUSTED_PROXIES")
	if !ok {
		return []string{
			"127.0.0.0/8",
			"::1/128",
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"fc00::/7",
		}
	}
	proxies := make([]string, 0)
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			proxies = append(proxies, p)
		}
	}
	return proxies
}
