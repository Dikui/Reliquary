package handler

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestVaultAttachmentRESTReadsCopiedImageAndRestrictsArticlePath(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
	require.NoError(t, service.EnsureSingleTeamSpace(ctx, db))
	kb, err := repository.EnsureTenantKnowledgeBase(ctx, db, types.SingleTenantID, "")
	require.NoError(t, err)
	root := t.TempDir()
	v, err := service.NewVaultService(db, &config.Config{Vault: &config.VaultConfig{Root: root}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, v.Close()) })
	article := &types.Knowledge{ID: uuid.NewString(), TenantID: types.SingleTenantID, KnowledgeBaseID: kb.ID, Title: "Attachment", SourceKind: "article", Type: "url", Source: "https://example.org/image", ParseStatus: types.ParseStatusCompleted, ContentType: "event", ProcessingMode: "full"}
	require.NoError(t, db.Create(article).Error)
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3ioAAAAASUVORK5CYII="
	body := "# Attachment\n\n![image](data:image/png;base64," + png + ")"
	require.NoError(t, v.WriteArticle(ctx, article, body, nil))
	refs := docparser.MarkdownImageReferences(article.VaultBody)
	require.Len(t, refs, 1)
	h := NewVaultHandler(nil)
	h.SetArticles(v)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(ctx); c.Next() }, middleware.ErrorHandler())
	router.GET("/api/v1/knowledge/:id/vault-attachment", h.Attachment)
	requestPath := "/api/v1/knowledge/" + article.ID + "/vault-attachment?" + url.Values{"path": {refs[0].URL}}.Encode()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, requestPath, nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	expected, err := base64.StdEncoding.DecodeString(png)
	require.NoError(t, err)
	require.Equal(t, expected, response.Body.Bytes())
	require.Equal(t, "image/png", response.Header().Get("Content-Type"))
	for _, path := range []string{"../../../outside.png", "../../attachments/other/image.png", "/etc/passwd"} {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/"+article.ID+"/vault-attachment?"+url.Values{"path": {path}}.Encode(), nil))
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
	link := filepath.Join(root, "attachments", article.ID, "linked.png")
	require.NoError(t, os.Symlink(filepath.Join(root, article.VaultPath), link))
	// Remove the deliberately unsafe fixture before the vault's final flush.
	t.Cleanup(func() { require.NoError(t, os.Remove(link)) })
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/"+article.ID+"/vault-attachment?"+url.Values{"path": {"../../attachments/" + article.ID + "/linked.png"}}.Encode(), nil))
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
}
