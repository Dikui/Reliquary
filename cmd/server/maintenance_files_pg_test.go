package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestMaintenanceModelsReportsEmbeddingRequirementFromVectorSources(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	require.NoError(t, service.EnsurePersonalSpace(ctx, db))
	library := types.Library{ID: "catalog", SpaceID: 1, Name: "Catalog", Directory: "catalog", EntityMode: types.EntityModeManaged, IndexingStrategy: types.DefaultIndexingStrategy()}
	require.NoError(t, db.Create(&library).Error)
	require.NoError(t, db.Create(&types.Entity{ID: "catalog-product", SpaceID: 1, LibraryID: library.ID, EntityType: "product", Kind: "product",
		CanonicalName: "Catalog product", NormalizedName: "catalog product", EmbeddingNeedsRebuild: true}).Error)
	check := func(t *testing.T, want bool) {
		t.Helper()
		var output bytes.Buffer
		require.NoError(t, runMaintenanceDatabase(ctx, db, "", []string{"models"}, bytes.NewReader(nil), &output))
		var result struct {
			EmbeddingRequired *bool `json:"embedding_required"`
		}
		require.NoError(t, json.Unmarshal(output.Bytes(), &result))
		require.NotNil(t, result.EmbeddingRequired)
		require.Equal(t, want, *result.EmbeddingRequired)
	}
	check(t, false)
	t.Run("extracted entity in a keyword-only library", func(t *testing.T) {
		extracted := types.Library{ID: "extract", SpaceID: 1, Name: "Extracted", Directory: "extract", IndexingStrategy: types.IndexingStrategy{KeywordEnabled: true}}
		require.NoError(t, db.Create(&extracted).Error)
		entity := types.Entity{ID: "company", SpaceID: 1, LibraryID: extracted.ID, EntityType: "company", CanonicalName: "Company", NormalizedName: "company"}
		require.NoError(t, db.Create(&entity).Error)
		check(t, true)
		require.NoError(t, db.Delete(&entity).Error)
	})
	entry := types.Entry{ID: "catalog-entry", SpaceID: 1, LibraryID: library.ID, Type: types.EntryTypeManual, SourceKind: "article", ParseStatus: types.ParseStatusCompleted}
	require.NoError(t, db.Create(&entry).Error)
	check(t, false)
	t.Run("enabled chunk", func(t *testing.T) {
		chunk := types.Chunk{ID: "catalog-chunk", SpaceID: 1, LibraryID: library.ID, EntryID: entry.ID, Content: "Specifications", ChunkType: types.ChunkTypeText}
		require.NoError(t, db.Create(&chunk).Error)
		check(t, true)
		require.NoError(t, db.Model(&chunk).Update("is_enabled", false).Error)
		check(t, false)
	})
	t.Run("completed article with a summary", func(t *testing.T) {
		require.NoError(t, db.Model(&entry).Update("structured_summary", gorm.Expr("'{}'::jsonb")).Error)
		check(t, true)
		require.NoError(t, db.Model(&entry).Update("structured_summary", nil).Error)
	})
	t.Run("eligible saved image", func(t *testing.T) {
		require.NoError(t, db.Create(&types.Model{ID: "image-vector", SpaceID: 1, Name: "Image vector", Type: types.ModelTypeEmbedding,
			Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{EmbeddingParameters: types.EmbeddingParameters{Dimension: 3, SupportsImages: true}}}).Error)
		require.NoError(t, db.Model(&types.Space{}).Where("id=1").Update("embedding_model_id", "image-vector").Error)
		require.NoError(t, db.Model(&entry).Updates(map[string]any{"vault_path": "catalog/article.md", "vault_body": "![Product](../attachments/catalog-entry/product.png)"}).Error)
		check(t, true)
		require.NoError(t, db.Model(&library).Update("indexing_strategy", types.IndexingStrategy{KeywordEnabled: true}).Error)
		check(t, false)
	})
}

func TestMaintenanceFilesHTTPAndCLIShareResultsAndRejectConcurrentRebuild(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	require.NoError(t, service.EnsurePersonalSpace(ctx, db))
	_, err := repository.EnsureSystemLibraries(ctx, db, types.SingleSpaceID, "")
	require.NoError(t, err)
	var library types.Library
	require.NoError(t, db.Where("system_role=?", "personal").First(&library).Error)
	root := t.TempDir()
	t.Setenv("VAULT_ROOT", root)
	t.Setenv("LOCAL_STORAGE_BASE_DIR", "")
	vault, err := service.NewVaultService(db, &config.Config{Vault: &config.VaultConfig{Root: root}})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO entries(id,space_id,library_id,title,type,source,source_kind,parse_status,enable_status,vault_path,vault_body,created_at,updated_at)
 VALUES('files-written',1,?,'Rebuild fixture','manual','manual','mine','completed','enabled','personal/rebuild-fixture.md','# Original body',now(),now()),
 ('files-skipped',1,?,'Unmanaged fixture','manual','manual','mine','completed','enabled','personal/unmanaged.md','',now(),now())`, library.ID, library.ID).Error)
	require.NoError(t, vault.RefreshHeader(ctx, "files-skipped"))
	var output bytes.Buffer
	require.NoError(t, runMaintenanceDatabase(ctx, db, "", []string{"rebuild-files"}, bytes.NewReader(nil), &output))
	var cli service.VaultFileRebuildResult
	require.NoError(t, json.Unmarshal(output.Bytes(), &cli))
	require.Equal(t, service.VaultFileRebuildResult{Rebuilt: true, Written: 1, Skipped: 1}, cli)
	original, err := os.ReadFile(filepath.Join(root, "personal/rebuild-fixture.md"))
	require.NoError(t, err)
	require.Contains(t, string(original), "# Original body")
	require.NoError(t, os.Remove(filepath.Join(root, "personal/rebuild-fixture.md")))

	h := handler.NewVaultHandler(nil)
	h.SetArticles(vault)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.POST("/vault/rebuild", h.RebuildFiles)
	router.GET("/vault/rebuild", h.FileRebuildStatus)
	invoke := func(method string) *httptest.ResponseRecorder {
		requestCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, "/vault/rebuild", nil).WithContext(requestCtx))
		return response
	}
	other, err := service.NewVaultService(db, &config.Config{Vault: &config.VaultConfig{Root: root}})
	require.NoError(t, err)
	// Block the worker inside its first database query while the request returns.
	tx := db.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, tx.Exec("LOCK TABLE entries IN ACCESS EXCLUSIVE MODE").Error)
	defer tx.Rollback()
	accepted := invoke(http.MethodPost)
	require.Equal(t, http.StatusAccepted, accepted.Code, accepted.Body.String())
	require.Contains(t, accepted.Body.String(), `"status":"running"`)
	require.Equal(t, http.StatusConflict, invoke(http.MethodPost).Code)
	// A separate service instance, as used by CLI, must also see the lock.
	_, err = other.RebuildFilesWithResult(ctx)
	require.ErrorIs(t, err, service.ErrVaultRebuildRunning)
	require.NoError(t, tx.Rollback().Error)
	var final service.VaultFileRebuildStatus
	require.Eventually(t, func() bool {
		response := invoke(http.MethodGet)
		require.Equal(t, http.StatusOK, response.Code)
		var body struct {
			Data service.VaultFileRebuildStatus `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		final = body.Data
		return final.Status != "running"
	}, 10*time.Second, 10*time.Millisecond)
	require.Equal(t, "success", final.Status, final.Error)
	require.Equal(t, &cli, final.Result)
	restored, err := os.ReadFile(filepath.Join(root, "personal/rebuild-fixture.md"))
	require.NoError(t, err)
	require.Equal(t, original, restored)
	// Lock is released and unchanged files are counted as skipped.
	require.Eventually(t, func() bool {
		result, err := vault.RebuildFilesWithResult(ctx)
		if err == service.ErrVaultRebuildRunning {
			return false
		}
		require.NoError(t, err)
		require.Equal(t, &service.VaultFileRebuildResult{Rebuilt: true, Skipped: 2}, result)
		return true
	}, time.Second, 10*time.Millisecond)
	// An invalid entry path fails with partial counts, and a failed run can retry.
	require.NoError(t, db.Exec("UPDATE entries SET vault_path='../invalid.md' WHERE id='files-written'").Error)
	result, err := vault.RebuildFilesWithResult(ctx)
	require.ErrorContains(t, err, "entry path must belong")
	require.False(t, result.Rebuilt)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, 1, result.Skipped)
	require.Equal(t, http.StatusAccepted, invoke(http.MethodPost).Code)
	require.Eventually(t, func() bool { return vault.FileRebuildStatus().Status == "failed" }, 10*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, vault.FileRebuildStatus().Result.Failed)
	require.Contains(t, vault.FileRebuildStatus().Error, "entry path must belong")
}
