package router

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	filesvc "reliquary/internal/application/service/file"
	"reliquary/internal/config"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

// getRouteRegistrar is the minimal registration surface serveFiles* needs;
// both *gin.Engine and *gin.RouterGroup satisfy it, which keeps the file
// routes testable without building a full engine.
type getRouteRegistrar interface {
	GET(string, ...gin.HandlerFunc) gin.IRoutes
}

// messageFileLookup is the narrow message-service surface needed by the
// message-scoped file proxy. Keeping it small makes the authorization boundary
// independently testable.
type messageFileLookup interface {
	GetMessage(ctx context.Context, sessionID, messageID string) (*types.Message, error)
}

// sharedAgentFileLookup verifies that a source workspace's agent is still
// shared to the caller. Revoking the share therefore also revokes historical
// message-file access.

// localStorageBaseDir resolves LOCAL_STORAGE_BASE_DIR with the container
// default.
func localStorageBaseDir() string {
	baseDir := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	if baseDir == "" {
		baseDir = "/data/files"
	}
	return baseDir
}

// localStorageAbsDir is localStorageBaseDir made absolute — the form the
// storage resolvers expect.
func localStorageAbsDir() string {
	absDir, _ := filepath.Abs(localStorageBaseDir())
	return absDir
}

// parseStorageTarget splits a stored file path into its optional storage
// backend ID and provider scheme (e.g. "backend://3/local://7/x.png" ->
// ("3", "local"); "s3://7/x.png" -> ("", "s3")).
func parseStorageTarget(filePath string) (backendID, provider string) {
	backendID, providerPath, scoped := types.ParseStorageBackendPath(filePath)
	if !scoped {
		providerPath = filePath
	}
	return backendID, types.ParseProviderScheme(providerPath)
}

// requireFilePathQuery validates the file_path query parameter every proxy
// route accepts. On failure the 400 response has been written and ok=false.
func requireFilePathQuery(c *gin.Context) (string, bool) {
	filePath := strings.TrimSpace(c.Query("file_path"))
	if filePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing required parameter: file_path"})
		return "", false
	}
	if strings.Contains(filePath, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file path"})
		return "", false
	}
	return filePath, true
}

// resolveCatalogResource maps a logical resource path onto its physical path
// when the catalog knows it, enforcing that the resource belongs to
// ownerTenantID. isResource reports whether the path resolved to a registered
// resource (whose tenant is then authoritative). On !ok the response has been
// written.
func resolveCatalogResource(
	c *gin.Context,
	catalog interfaces.ResourceCatalog,
	filePath string,
	ownerTenantID uint64) (resolved string, isResource, ok bool) {
	if catalog == nil {
		return filePath, false, true
	}
	resolvedPath, resource, err := catalog.ResolvePath(c.Request.Context(), filePath)
	if err != nil {
		c.Status(http.StatusNotFound)
		return "", false, false
	}
	if resource == nil {
		return filePath, false, true
	}
	if resource.TenantID != ownerTenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: resource not accessible"})
		return "", false, false
	}
	return resolvedPath, true, true
}

// resolveFileService picks the file service for (tenant, backendID, provider)
// — via the storage resolver when wired, else directly from the tenant's
// storage config. No fallback; used by the presigned surfaces where a
// missing tenant config must surface as an error.
func resolveFileService(
	ctx context.Context,
	tenant *types.Tenant,
	backendID, provider, absDir string,
	storageResolver interfaces.StorageBackendResolver) (interfaces.FileService, string, error) {
	if storageResolver != nil {
		return storageResolver.ResolveFileService(ctx, tenant, backendID, provider, absDir)
	}
	return filesvc.NewFileServiceFromStorageConfig(provider, tenant.StorageEngineConfig, absDir)
}

// resolveTenantFileServiceWithFallback is resolveFileService plus the
// fallback rule shared by /files and the KB-scoped proxy: when tenant-level
// resolution fails and the requested provider matches the process-global
// STORAGE_TYPE, serve through the global file service instead of failing.
// Returns ok=false (already logged) when no service can be resolved; the
// caller decides the HTTP status.
func resolveTenantFileServiceWithFallback(
	ctx context.Context,
	logTag string,
	tenant *types.Tenant,
	backendID, provider, absDir string,
	storageResolver interfaces.StorageBackendResolver,
	globalFileService interfaces.FileService) (fileSvc interfaces.FileService, resolvedProvider string, ok bool) {
	var err error
	if storageResolver != nil {
		fileSvc, resolvedProvider, err = storageResolver.ResolveFileService(ctx, tenant, backendID, provider, absDir)
	} else if tenant.StorageEngineConfig != nil {
		fileSvc, resolvedProvider, err = filesvc.NewFileServiceFromStorageConfig(provider, tenant.StorageEngineConfig, absDir)
	} else {
		err = http.ErrMissingFile
	}
	if err == nil {
		return fileSvc, resolvedProvider, true
	}

	globalStorageType := strings.ToLower(strings.TrimSpace(os.Getenv("STORAGE_TYPE")))
	if globalStorageType == "" {
		globalStorageType = "local"
	}
	if provider == globalStorageType && globalFileService != nil {
		logger.Warnf(ctx, "[Router] %s tenant storage config missing or invalid, fallback to global file service: tenant_id=%d provider=%s err=%v",
			logTag, tenant.ID, provider, err)
		return globalFileService, globalStorageType, true
	}
	logger.Warnf(ctx, "[Router] %s resolve file service failed without fallback: tenant_id=%d provider=%s global_storage_type=%s err=%v",
		logTag, tenant.ID, provider, globalStorageType, err)
	return nil, "", false
}

// streamStoredFile writes the shared success response of every file proxy:
// safe content type, nosniff, disposition for non-inline types, the route's
// cache policy, then the body (skipped for HEAD). Closes reader.
func streamStoredFile(c *gin.Context, reader io.ReadCloser, contentType string, inline bool, cacheControl, logTag string) {
	defer reader.Close()
	c.Header("Content-Type", contentType)
	c.Header("X-Content-Type-Options", "nosniff")
	if !inline {
		c.Header("Content-Disposition", "attachment")
	}
	c.Header("Cache-Control", cacheControl)
	c.Status(http.StatusOK)
	if c.Request.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(c.Writer, reader); err != nil {
		logger.Warnf(c.Request.Context(), "[Router] %s write response failed: %v", logTag, err)
	}
}

// newFileServeHandler builds a file proxy that verifies the active team owns the path.
func newFileServeHandler(
	globalFileService interfaces.FileService,
	storageResolver interfaces.StorageBackendResolver,
	resourceCatalogs ...interfaces.ResourceCatalog) gin.HandlerFunc {
	resourceCatalog := firstResourceCatalog(resourceCatalogs)
	absDir := localStorageAbsDir()
	if info, err := os.Stat(absDir); err != nil || !info.IsDir() {
		if err := os.MkdirAll(absDir, 0o755); err != nil {
			logger.Warnf(context.Background(), "[Router] Cannot create local storage dir %s: %v", absDir, err)
		}
	}

	return func(c *gin.Context) {
		filePath, ok := requireFilePathQuery(c)
		if !ok {
			return
		}

		tenant, _ := c.Request.Context().Value(types.TenantInfoContextKey).(*types.Tenant)
		if tenant == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: workspace context missing"})
			return
		}
		filePath, resourceResolved, ok := resolveCatalogResource(c, resourceCatalog, filePath, tenant.ID)
		if !ok {
			return
		}

		// A registered resource's tenant is authoritative. Physical provider
		// paths remain an internal locator and are not required to encode access
		// control metadata (some cloud layouts contain other numeric segments).
		if !resourceResolved {
			if err := secutils.ValidateStoragePathTenant(filePath, tenant.ID); err != nil {
				logger.Warnf(c.Request.Context(),
					"[Router] /files denied cross-tenant or invalid path: tenant_id=%d file_path=%q err=%v",
					tenant.ID, filePath, err)
				c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: file path not accessible"})
				return
			}
		}

		backendID, provider := parseStorageTarget(filePath)
		fileSvc, resolvedProvider, ok := resolveTenantFileServiceWithFallback(
			c.Request.Context(), "/files", tenant, backendID, provider, absDir, storageResolver, globalFileService)
		if !ok {
			c.Status(http.StatusBadRequest)
			return
		}

		reader, err := fileSvc.GetFile(c.Request.Context(), filePath)
		if err != nil {
			logger.Warnf(c.Request.Context(), "[Router] /files get file failed: tenant_id=%d provider=%s path=%q err=%v",
				tenant.ID, resolvedProvider, filePath, err)
			c.Status(http.StatusNotFound)
			return
		}

		contentType, inline := secutils.SafeContentTypeByFilename(filePath)
		streamStoredFile(c, reader, contentType, inline, "public, max-age=86400", "/files")
	}
}

func serveFiles(r getRouteRegistrar, globalFileService interfaces.FileService, resolvers ...interfaces.StorageBackendResolver) {
	var storageResolver interfaces.StorageBackendResolver
	if len(resolvers) > 0 {
		storageResolver = resolvers[0]
	}
	serveFilesWithResources(r, globalFileService, storageResolver, nil)
}

// serveFilesWithResources registers the tenant-scoped storage proxy.
// It is registered after auth middleware, so tenant context comes from
// authentication.
//
// Route:
//   - GET /files?file_path=<provider://...>
func serveFilesWithResources(
	r getRouteRegistrar,
	globalFileService interfaces.FileService,
	storageResolver interfaces.StorageBackendResolver,
	resourceCatalog interfaces.ResourceCatalog) {
	logger.Infof(context.Background(), "[Router] Serving files from /files")
	r.GET(
		"/files",

		newFileServeHandler(globalFileService, storageResolver, resourceCatalog))
}

func serveResourceGrants(
	r *gin.Engine,
	resourceCatalog interfaces.ResourceCatalog,
	tenantService interfaces.TenantService,
	globalFileService interfaces.FileService,
	storageResolver interfaces.StorageBackendResolver) {
	if resourceCatalog == nil || tenantService == nil {
		return
	}
	handler := func(c *gin.Context) {
		ctx := c.Request.Context()
		resource, err := resourceCatalog.ResolveAccessGrant(ctx, c.Param("token"))
		if err != nil || resource == nil {
			c.Status(http.StatusNotFound)
			return
		}
		tenant, err := tenantService.GetTenantByID(ctx, resource.TenantID)
		if err != nil || tenant == nil {
			c.Status(http.StatusNotFound)
			return
		}

		// The grant row carries its own backend ID; only the provider scheme
		// comes from the physical path.
		_, provider := parseStorageTarget(resource.PhysicalPath)
		var fileSvc interfaces.FileService
		if storageResolver != nil {
			fileSvc, _, err = storageResolver.ResolveFileService(
				ctx, tenant, resource.StorageBackendID, provider, localStorageBaseDir())
		} else {
			fileSvc = globalFileService
		}
		if err != nil || fileSvc == nil {
			logger.Warnf(ctx, "[Router] resource grant storage resolution failed: resource_id=%s err=%v", resource.ID, err)
			c.Status(http.StatusNotFound)
			return
		}
		reader, err := fileSvc.GetFile(ctx, resource.PhysicalPath)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}

		fileName := resource.OriginalName
		if fileName == "" {
			fileName = resource.PhysicalPath
		}
		contentType, inline := secutils.SafeContentTypeByFilename(fileName)
		if resource.MimeType != "" && inline {
			contentType = resource.MimeType
		}
		streamStoredFile(c, reader, contentType, inline, "private, max-age=300",
			"resource grant (resource_id="+resource.ID+")")
	}
	r.GET("/r/:token", handler)
	r.HEAD("/r/:token", handler)
}

func serveKBScopedFiles(
	r *gin.RouterGroup,
	g *accessGuards,
	tenantService interfaces.TenantService,
	globalFileService interfaces.FileService,
	storageResolver interfaces.StorageBackendResolver,
	resourceCatalogs ...interfaces.ResourceCatalog) {
	logger.Infof(context.Background(), "[Router] Serving KB-scoped files from /knowledge-bases/:id/files")
	r.Handle(http.MethodGet, "/knowledge-bases/:id/files", g.KBAccessRead("id"), newKBScopedFileServeHandlerWithResources(
		tenantService,
		globalFileService,
		storageResolver,
		firstResourceCatalog(resourceCatalogs)))
}

func newKBScopedFileServeHandler(
	tenantService interfaces.TenantService,
	globalFileService interfaces.FileService,
	resolvers ...interfaces.StorageBackendResolver) gin.HandlerFunc {
	var storageResolver interfaces.StorageBackendResolver
	if len(resolvers) > 0 {
		storageResolver = resolvers[0]
	}
	return newKBScopedFileServeHandlerWithResources(tenantService, globalFileService, storageResolver, nil)
}

func firstResourceCatalog(catalogs []interfaces.ResourceCatalog) interfaces.ResourceCatalog {
	if len(catalogs) == 0 {
		return nil
	}
	return catalogs[0]
}

func newKBScopedFileServeHandlerWithResources(
	tenantService interfaces.TenantService,
	globalFileService interfaces.FileService,
	storageResolver interfaces.StorageBackendResolver,
	resourceCatalog interfaces.ResourceCatalog) gin.HandlerFunc {
	absDir := localStorageAbsDir()

	return func(c *gin.Context) {
		ctx := c.Request.Context()

		filePath, ok := requireFilePathQuery(c)
		if !ok {
			return
		}

		ownerTenantID, ok := types.TenantIDFromContext(ctx)
		if !ok || ownerTenantID == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: workspace context missing"})
			return
		}
		filePath, _, ok = resolveCatalogResource(c, resourceCatalog, filePath, ownerTenantID)
		if !ok {
			return
		}

		if err := secutils.ValidateKBScopedStoragePath(filePath, ownerTenantID); err != nil {
			logger.Warnf(ctx, "[Router] /knowledge-bases/:id/files denied path not allowed for KB proxy: owner_tenant_id=%d file_path=%q err=%v",
				ownerTenantID, filePath, err)
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: file path not accessible"})
			return
		}

		tenant, err := tenantService.GetTenantByID(ctx, ownerTenantID)
		if err != nil || tenant == nil {
			logger.Warnf(ctx, "[Router] /knowledge-bases/:id/files owner tenant lookup failed: owner_tenant_id=%d err=%v",
				ownerTenantID, err)
			c.Status(http.StatusNotFound)
			return
		}

		backendID, provider := parseStorageTarget(filePath)
		fileSvc, resolvedProvider, ok := resolveTenantFileServiceWithFallback(
			ctx, "/knowledge-bases/:id/files", tenant, backendID, provider, absDir, storageResolver, globalFileService)
		if !ok {
			c.Status(http.StatusBadRequest)
			return
		}

		reader, err := fileSvc.GetFile(ctx, filePath)
		if err != nil {
			logger.Warnf(ctx, "[Router] /knowledge-bases/:id/files get file failed: owner_tenant_id=%d provider=%s path=%q err=%v",
				ownerTenantID, resolvedProvider, filePath, err)
			c.Status(http.StatusNotFound)
			return
		}

		contentType, inline := secutils.SafeContentTypeByFilename(filePath)
		// Cross-tenant shared content — keep it private so shared proxies /
		// CDNs do not cache one tenant's view for another.
		streamStoredFile(c, reader, contentType, inline, "private, max-age=86400", "/knowledge-bases/:id/files")
	}
}

// newMessageScopedFileServeHandler verifies session access and team ownership of message files.
func newMessageScopedFileServeHandler(
	messageService messageFileLookup,
	tenantService interfaces.TenantService,
	globalFileService interfaces.FileService,
	storageResolver interfaces.StorageBackendResolver,
	resourceCatalog interfaces.ResourceCatalog) gin.HandlerFunc {
	absDir := localStorageAbsDir()

	return func(c *gin.Context) {
		ctx := c.Request.Context()
		filePath, ok := requireFilePathQuery(c)
		if !ok {
			return
		}

		callerTenantID, ok := types.TenantIDFromContext(ctx)
		if !ok || callerTenantID == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: workspace context missing"})
			return
		}
		if messageService == nil {
			c.Status(http.StatusNotFound)
			return
		}

		message, err := messageService.GetMessage(ctx, c.Param("id"), c.Param("message_id"))
		if err != nil || message == nil {
			// Do not reveal whether a message exists outside the caller's session.
			c.Status(http.StatusNotFound)
			return
		}

		resolvedPath := filePath
		var resource *types.StoredResource
		if resourceCatalog != nil {
			resolvedPath, resource, err = resourceCatalog.ResolvePath(ctx, filePath)
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
		}

		ownerTenantID := callerTenantID
		if resource != nil {
			ownerTenantID = resource.TenantID

		}

		if ownerTenantID != callerTenantID {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: resource outside team"})
			return
		}

		// Registered resources carry authoritative tenant ownership. Legacy
		// provider paths must still encode the authorized owner tenant.
		if resource == nil {
			if err := secutils.ValidateStoragePathTenant(resolvedPath, ownerTenantID); err != nil {
				logger.Warnf(ctx,
					"[Router] message files denied cross-tenant or invalid path: owner_tenant_id=%d file_path=%q err=%v",
					ownerTenantID, resolvedPath, err)
				c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: file path not accessible"})
				return
			}
		}

		ownerTenant, err := tenantService.GetTenantByID(ctx, ownerTenantID)
		if err != nil || ownerTenant == nil {
			c.Status(http.StatusNotFound)
			return
		}

		backendID, provider := parseStorageTarget(resolvedPath)
		fileSvc, resolvedProvider, ok := resolveTenantFileServiceWithFallback(
			ctx,
			"/sessions/:id/messages/:message_id/files",
			ownerTenant,
			backendID,
			provider,
			absDir,
			storageResolver,
			globalFileService)
		if !ok {
			c.Status(http.StatusBadRequest)
			return
		}

		reader, err := fileSvc.GetFile(ctx, resolvedPath)
		if err != nil {
			logger.Warnf(ctx,
				"[Router] message files get file failed: owner_tenant_id=%d provider=%s path=%q err=%v",
				ownerTenantID, resolvedProvider, resolvedPath, err)
			c.Status(http.StatusNotFound)
			return
		}

		contentType, inline := secutils.SafeContentTypeByFilename(resolvedPath)
		if resource != nil && resource.MimeType != "" && inline {
			contentType = resource.MimeType
		}
		streamStoredFile(c, reader, contentType, inline, "private, max-age=86400", "message files")
	}
}

// serveMessageScopedFiles registers the authenticated proxy used by the chat
// renderer for assistant-message resources. The chat API-key capability is
// sufficient because GetMessage enforces ownership of the API key's session.
func serveMessageScopedFiles(
	r *gin.RouterGroup,
	g *accessGuards,
	messageService interfaces.MessageService,
	tenantService interfaces.TenantService,
	globalFileService interfaces.FileService,
	storageResolver interfaces.StorageBackendResolver,
	resourceCatalog interfaces.ResourceCatalog) {
	r.Handle(http.MethodGet, "/sessions/:id/messages/:message_id/files", newMessageScopedFileServeHandler(
		messageService,
		tenantService,
		globalFileService,
		storageResolver,
		resourceCatalog))
}

func servePresignedFiles(r *gin.Engine, tenantService interfaces.TenantService, storageResolver interfaces.StorageBackendResolver) {
	handler := presignedFileHandler(tenantService, localStorageAbsDir(), storageResolver)
	r.GET("/api/v1/files/presigned", handler)
	r.HEAD("/api/v1/files/presigned", handler)
}

func presignedFileHandler(tenantService interfaces.TenantService, absDir string, resolvers ...interfaces.StorageBackendResolver) gin.HandlerFunc {
	var storageResolver interfaces.StorageBackendResolver
	if len(resolvers) > 0 {
		storageResolver = resolvers[0]
	}
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		clientIP := c.ClientIP()
		userAgent := c.Request.UserAgent()

		filePath := strings.TrimSpace(c.Query("file_path"))
		tenantIDStr := strings.TrimSpace(c.Query("tenant_id"))
		expiresStr := strings.TrimSpace(c.Query("expires"))
		sig := strings.TrimSpace(c.Query("sig"))

		if filePath == "" || tenantIDStr == "" || expiresStr == "" || sig == "" {
			logger.Warnf(ctx, "[Router] /files/presigned missing params: client_ip=%s ua=%q file_path=%q tenant_id=%q expires=%q has_sig=%v",
				clientIP, userAgent, filePath, tenantIDStr, expiresStr, sig != "")
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing required parameters"})
			return
		}
		if strings.Contains(filePath, "..") {
			logger.Warnf(ctx, "[Router] /files/presigned rejected path traversal: client_ip=%s file_path=%q", clientIP, filePath)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file path"})
			return
		}

		tenantID, err := strconv.ParseUint(tenantIDStr, 10, 64)
		if err != nil {
			logger.Warnf(ctx, "[Router] /files/presigned invalid tenant_id: client_ip=%s tenant_id=%q err=%v", clientIP, tenantIDStr, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tenant_id"})
			return
		}

		if !secutils.VerifyFileURLSig(filePath, tenantID, expiresStr, sig) {
			logger.Warnf(ctx, "[Router] /files/presigned sig invalid or expired: client_ip=%s ua=%q tenant_id=%d file_path=%q expires=%s",
				clientIP, userAgent, tenantID, filePath, expiresStr)
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired signature"})
			return
		}

		tenant, err := tenantService.GetTenantByID(ctx, tenantID)
		if err != nil {
			logger.Warnf(ctx, "[Router] /files/presigned tenant lookup failed: client_ip=%s tenant_id=%d err=%v", clientIP, tenantID, err)
			c.Status(http.StatusNotFound)
			return
		}

		backendID, provider := parseStorageTarget(filePath)
		fileSvc, resolvedProvider, err := resolveFileService(ctx, tenant, backendID, provider, absDir, storageResolver)
		if err != nil {
			logger.Warnf(ctx, "[Router] /files/presigned resolve file service failed: client_ip=%s tenant_id=%d provider=%s err=%v",
				clientIP, tenantID, provider, err)
			c.Status(http.StatusBadRequest)
			return
		}

		// HEAD short-circuits the body read inside streamStoredFile. We still
		// need to confirm the object exists via GetFile: skipping it entirely
		// for HEAD would risk reporting 200 for a signed URL that no longer
		// points at a real object, making subsequent GETs from the same client
		// mysteriously fail.
		reader, err := fileSvc.GetFile(ctx, filePath)
		if err != nil {
			logger.Warnf(ctx, "[Router] /files/presigned get file failed: client_ip=%s tenant_id=%d provider=%s path=%q err=%v",
				clientIP, tenantID, resolvedProvider, filePath, err)
			c.Status(http.StatusNotFound)
			return
		}

		contentType, inline := secutils.SafeContentTypeByFilename(filePath)
		streamStoredFile(c, reader, contentType, inline, "public, max-age=86400", "/files/presigned")
	}
}

func servePresignedPreview(r *gin.Engine, cfg *config.Config, storageResolver interfaces.StorageBackendResolver, resourceCatalog interfaces.ResourceCatalog) {
	absDir := localStorageAbsDir()

	r.GET("/api/v1/files/presigned-preview",

		func(c *gin.Context) {
			ctx := c.Request.Context()
			filePath, ok := requireFilePathQuery(c)
			if !ok {
				return
			}

			tenant, _ := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
			if tenant == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: workspace context missing"})
				return
			}

			if strings.HasPrefix(filePath, types.ResourceScheme) && resourceCatalog == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "resource catalog unavailable"})
				return
			}
			resolvedPath, resourceResolved, ok := resolveCatalogResource(c, resourceCatalog, filePath, tenant.ID)
			if !ok {
				return
			}
			if !resourceResolved {
				if err := secutils.ValidateStoragePathTenant(resolvedPath, tenant.ID); err != nil {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: file path not accessible"})
					return
				}
			}

			backendID, provider := parseStorageTarget(resolvedPath)
			fileSvc, resolvedProvider, err := resolveFileService(ctx, tenant, backendID, provider, absDir, storageResolver)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":    err.Error(),
					"provider": provider,
					"hint":     "workspace storage config is missing or incomplete for this provider",
				})
				return
			}

			httpURL, err := fileSvc.GetFileURL(ctx, filePath)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":    err.Error(),
					"provider": resolvedProvider,
					"hint":     "GetFileURL failed; for local storage this usually means APP_EXTERNAL_URL is unset",
				})
				return
			}

			// Detect the "no-op" case where local storage falls back to the
			// provider:// path because APP_EXTERNAL_URL is missing. Surfacing
			// this explicitly is the whole point of the endpoint.
			rewritten := httpURL != filePath
			hint := ""
			if !rewritten {
				hint = "URL unchanged; for local storage set APP_EXTERNAL_URL to enable presigned HTTP URLs"
			}

			c.JSON(http.StatusOK, gin.H{
				"file_path": filePath,
				"provider":  resolvedProvider,
				"url":       httpURL,
				"rewritten": rewritten,
				"hint":      hint,
			})
		})
}
