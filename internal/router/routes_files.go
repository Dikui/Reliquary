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
// ownerSpaceID. isResource reports whether the path resolved to a registered
// resource (whose space is then authoritative). On !ok the response has been
// written.
func resolveCatalogResource(
	c *gin.Context,
	catalog interfaces.ResourceCatalog,
	filePath string,
	ownerSpaceID uint64) (resolved string, isResource, ok bool) {
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
	if resource.SpaceID != ownerSpaceID {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: resource not accessible"})
		return "", false, false
	}
	return resolvedPath, true, true
}

// localFileService verifies that the reference belongs to the local filesystem.
func localFileService(filePath, absDir string, fileService interfaces.FileService) (interfaces.FileService, bool) {
	if strings.Contains(filePath, "://") && !strings.HasPrefix(filePath, "local://") {
		return nil, false
	}
	if fileService != nil {
		return fileService, true
	}
	return filesvc.NewLocalFileService(absDir, os.Getenv("APP_EXTERNAL_URL")), true
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

		space, _ := c.Request.Context().Value(types.SpaceInfoContextKey).(*types.Space)
		if space == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: workspace context missing"})
			return
		}
		filePath, resourceResolved, ok := resolveCatalogResource(c, resourceCatalog, filePath, space.ID)
		if !ok {
			return
		}

		// A registered resource's space is authoritative. Physical provider
		// paths remain an internal locator and are not required to encode access
		// control metadata (some cloud layouts contain other numeric segments).
		if !resourceResolved {
			if err := secutils.ValidateStoragePathSpace(filePath, space.ID); err != nil {
				logger.Warnf(c.Request.Context(),
					"[Router] /files denied cross-space or invalid path: space_id=%d file_path=%q err=%v",
					space.ID, filePath, err)
				c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: file path not accessible"})
				return
			}
		}

		fileSvc, ok := localFileService(filePath, absDir, globalFileService)
		if !ok {
			c.Status(http.StatusBadRequest)
			return
		}

		reader, err := fileSvc.GetFile(c.Request.Context(), filePath)
		if err != nil {
			logger.Warnf(c.Request.Context(), "[Router] /files get file failed: space_id=%d provider=%s path=%q err=%v",
				space.ID, "local", filePath, err)
			c.Status(http.StatusNotFound)
			return
		}

		contentType, inline := secutils.SafeContentTypeByFilename(filePath)
		streamStoredFile(c, reader, contentType, inline, "public, max-age=86400", "/files")
	}
}

func serveFiles(r getRouteRegistrar, globalFileService interfaces.FileService) {
	serveFilesWithResources(r, globalFileService, nil)
}

// serveFilesWithResources registers the space-scoped storage proxy.
// It is registered after auth middleware, so space context comes from
// authentication.
//
// Route:
//   - GET /files?file_path=<provider://...>
func serveFilesWithResources(
	r getRouteRegistrar,
	globalFileService interfaces.FileService,
	resourceCatalog interfaces.ResourceCatalog) {
	logger.Infof(context.Background(), "[Router] Serving files from /files")
	r.GET(
		"/files",

		newFileServeHandler(globalFileService, resourceCatalog))
}

func serveResourceGrants(
	r *gin.Engine,
	resourceCatalog interfaces.ResourceCatalog,
	spaceService interfaces.SpaceService,
	globalFileService interfaces.FileService,
) {
	if resourceCatalog == nil || spaceService == nil {
		return
	}
	handler := func(c *gin.Context) {
		ctx := c.Request.Context()
		resource, err := resourceCatalog.ResolveAccessGrant(ctx, c.Param("token"))
		if err != nil || resource == nil {
			c.Status(http.StatusNotFound)
			return
		}
		space, err := spaceService.GetSpaceByID(ctx, resource.SpaceID)
		if err != nil || space == nil {
			c.Status(http.StatusNotFound)
			return
		}

		fileSvc, ok := localFileService(resource.PhysicalPath, localStorageAbsDir(), globalFileService)
		if !ok || fileSvc == nil {
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
	spaceService interfaces.SpaceService,
	globalFileService interfaces.FileService,
	resourceCatalogs ...interfaces.ResourceCatalog) {
	logger.Infof(context.Background(), "[Router] Serving KB-scoped files from /knowledge-bases/:id/files")
	r.Handle(http.MethodGet, "/knowledge-bases/:id/files", g.KBExists("id"), newKBScopedFileServeHandlerWithResources(
		spaceService,
		globalFileService,
		firstResourceCatalog(resourceCatalogs)))
}

func newKBScopedFileServeHandler(
	spaceService interfaces.SpaceService,
	globalFileService interfaces.FileService,
) gin.HandlerFunc {
	return newKBScopedFileServeHandlerWithResources(spaceService, globalFileService, nil)
}

func firstResourceCatalog(catalogs []interfaces.ResourceCatalog) interfaces.ResourceCatalog {
	if len(catalogs) == 0 {
		return nil
	}
	return catalogs[0]
}

func newKBScopedFileServeHandlerWithResources(
	spaceService interfaces.SpaceService,
	globalFileService interfaces.FileService,
	resourceCatalog interfaces.ResourceCatalog) gin.HandlerFunc {
	absDir := localStorageAbsDir()

	return func(c *gin.Context) {
		ctx := c.Request.Context()

		filePath, ok := requireFilePathQuery(c)
		if !ok {
			return
		}

		ownerSpaceID, ok := types.SpaceIDFromContext(ctx)
		if !ok || ownerSpaceID == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: workspace context missing"})
			return
		}
		filePath, _, ok = resolveCatalogResource(c, resourceCatalog, filePath, ownerSpaceID)
		if !ok {
			return
		}

		if err := secutils.ValidateKBScopedStoragePath(filePath, ownerSpaceID); err != nil {
			logger.Warnf(ctx, "[Router] /knowledge-bases/:id/files denied path not allowed for KB proxy: owner_space_id=%d file_path=%q err=%v",
				ownerSpaceID, filePath, err)
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: file path not accessible"})
			return
		}

		space, err := spaceService.GetSpaceByID(ctx, ownerSpaceID)
		if err != nil || space == nil {
			logger.Warnf(ctx, "[Router] /knowledge-bases/:id/files owner space lookup failed: owner_space_id=%d err=%v",
				ownerSpaceID, err)
			c.Status(http.StatusNotFound)
			return
		}

		fileSvc, ok := localFileService(filePath, absDir, globalFileService)
		if !ok {
			c.Status(http.StatusBadRequest)
			return
		}

		reader, err := fileSvc.GetFile(ctx, filePath)
		if err != nil {
			logger.Warnf(ctx, "[Router] /knowledge-bases/:id/files get file failed: owner_space_id=%d provider=%s path=%q err=%v",
				ownerSpaceID, "local", filePath, err)
			c.Status(http.StatusNotFound)
			return
		}

		contentType, inline := secutils.SafeContentTypeByFilename(filePath)
		// Cross-space shared content — keep it private so shared proxies /
		// CDNs do not cache one space's view for another.
		streamStoredFile(c, reader, contentType, inline, "private, max-age=86400", "/knowledge-bases/:id/files")
	}
}

func servePresignedFiles(r *gin.Engine, spaceService interfaces.SpaceService) {
	handler := presignedFileHandler(spaceService, localStorageAbsDir())
	r.GET("/api/v1/files/presigned", handler)
	r.HEAD("/api/v1/files/presigned", handler)
}

func presignedFileHandler(spaceService interfaces.SpaceService, absDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		clientIP := c.ClientIP()
		userAgent := c.Request.UserAgent()

		filePath := strings.TrimSpace(c.Query("file_path"))
		spaceIDStr := strings.TrimSpace(c.Query("space_id"))
		expiresStr := strings.TrimSpace(c.Query("expires"))
		sig := strings.TrimSpace(c.Query("sig"))

		if filePath == "" || spaceIDStr == "" || expiresStr == "" || sig == "" {
			logger.Warnf(ctx, "[Router] /files/presigned missing params: client_ip=%s ua=%q file_path=%q space_id=%q expires=%q has_sig=%v",
				clientIP, userAgent, filePath, spaceIDStr, expiresStr, sig != "")
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing required parameters"})
			return
		}
		if strings.Contains(filePath, "..") {
			logger.Warnf(ctx, "[Router] /files/presigned rejected path traversal: client_ip=%s file_path=%q", clientIP, filePath)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file path"})
			return
		}

		spaceID, err := strconv.ParseUint(spaceIDStr, 10, 64)
		if err != nil {
			logger.Warnf(ctx, "[Router] /files/presigned invalid space_id: client_ip=%s space_id=%q err=%v", clientIP, spaceIDStr, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid space_id"})
			return
		}

		if !secutils.VerifyFileURLSig(filePath, spaceID, expiresStr, sig) {
			logger.Warnf(ctx, "[Router] /files/presigned sig invalid or expired: client_ip=%s ua=%q space_id=%d file_path=%q expires=%s",
				clientIP, userAgent, spaceID, filePath, expiresStr)
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired signature"})
			return
		}

		_, err = spaceService.GetSpaceByID(ctx, spaceID)
		if err != nil {
			logger.Warnf(ctx, "[Router] /files/presigned space lookup failed: client_ip=%s space_id=%d err=%v", clientIP, spaceID, err)
			c.Status(http.StatusNotFound)
			return
		}

		fileSvc, ok := localFileService(filePath, absDir, nil)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported file storage reference"})
			return
		}

		// HEAD short-circuits the body read inside streamStoredFile. We still
		// need to confirm the object exists via GetFile: skipping it entirely
		// for HEAD would risk reporting 200 for a signed URL that no longer
		// points at a real object, making subsequent GETs from the same client
		// mysteriously fail.
		reader, err := fileSvc.GetFile(ctx, filePath)
		if err != nil {
			logger.Warnf(ctx, "[Router] /files/presigned get file failed: client_ip=%s space_id=%d provider=%s path=%q err=%v",
				clientIP, spaceID, "local", filePath, err)
			c.Status(http.StatusNotFound)
			return
		}

		contentType, inline := secutils.SafeContentTypeByFilename(filePath)
		streamStoredFile(c, reader, contentType, inline, "public, max-age=86400", "/files/presigned")
	}
}

func servePresignedPreview(r *gin.Engine, cfg *config.Config, resourceCatalog interfaces.ResourceCatalog) {
	absDir := localStorageAbsDir()

	r.GET("/api/v1/files/presigned-preview",

		func(c *gin.Context) {
			ctx := c.Request.Context()
			filePath, ok := requireFilePathQuery(c)
			if !ok {
				return
			}

			space, _ := ctx.Value(types.SpaceInfoContextKey).(*types.Space)
			if space == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: workspace context missing"})
				return
			}

			if strings.HasPrefix(filePath, types.ResourceScheme) && resourceCatalog == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "resource catalog unavailable"})
				return
			}
			resolvedPath, resourceResolved, ok := resolveCatalogResource(c, resourceCatalog, filePath, space.ID)
			if !ok {
				return
			}
			if !resourceResolved {
				if err := secutils.ValidateStoragePathSpace(resolvedPath, space.ID); err != nil {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: file path not accessible"})
					return
				}
			}

			fileSvc, ok := localFileService(resolvedPath, absDir, nil)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported file storage reference"})
				return
			}
			fileSvc = filesvc.NewResourceCatalogFileService(fileSvc, resourceCatalog)

			httpURL, err := fileSvc.GetFileURL(ctx, filePath)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":    err.Error(),
					"provider": "local",
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
				"provider":  "local",
				"url":       httpURL,
				"rewritten": rewritten,
				"hint":      hint,
			})
		})
}
