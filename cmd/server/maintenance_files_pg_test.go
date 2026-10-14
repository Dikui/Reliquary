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
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestMaintenanceFilesHTTPAndCLIShareResultsAndRejectConcurrentRebuild(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	require.NoError(t, service.EnsurePersonalSpace(ctx, db))
	_, err := repository.EnsureSystemLibraries(ctx, db, types.SingleSpaceID, "")
	require.NoError(t, err)
	var library types.Library
	require.NoError(t, db.Where("system_role=?", "mine").First(&library).Error)
	root := t.TempDir()
	t.Setenv("VAULT_ROOT", root)
	t.Setenv("LOCAL_STORAGE_BASE_DIR", "")
	vault, err := service.NewVaultService(db, &config.Config{Vault: &config.VaultConfig{Root: root}})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO entries(id,space_id,library_id,title,type,source,source_kind,parse_status,enable_status,vault_path,vault_body,created_at,updated_at)
 VALUES('files-written',1,?,'Rebuild fixture','manual','manual','mine','completed','enabled','mine/rebuild-fixture.md','# Original body',now(),now()),
 ('files-skipped',1,?,'Unmanaged fixture','manual','manual','mine','completed','enabled','mine/unmanaged.md','',now(),now())`, library.ID, library.ID).Error)
	require.NoError(t, vault.RefreshHeader(ctx, "files-skipped"))
	var output bytes.Buffer
	require.NoError(t, runMaintenanceDatabase(ctx, db, "", []string{"rebuild-files"}, bytes.NewReader(nil), &output))
	var cli service.VaultFileRebuildResult
	require.NoError(t, json.Unmarshal(output.Bytes(), &cli))
	require.Equal(t, service.VaultFileRebuildResult{Rebuilt: true, Written: 1, Skipped: 1}, cli)
	original, err := os.ReadFile(filepath.Join(root, "mine/rebuild-fixture.md"))
	require.NoError(t, err)
	require.Contains(t, string(original), "# Original body")
	require.NoError(t, os.Remove(filepath.Join(root, "mine/rebuild-fixture.md")))

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
	other, err := service.NewVaultService(db, &config.Config{Vault: &config.VaultConfig{Root: root}})
	require.NoError(t, err)
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
	restored, err := os.ReadFile(filepath.Join(root, "mine/rebuild-fixture.md"))
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
