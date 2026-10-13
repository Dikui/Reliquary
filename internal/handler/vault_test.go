package handler

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
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestReportsRESTWritesVaultFileAndIndexesImmediately(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID)
	require.NoError(t, service.EnsurePersonalSpace(ctx, db))
	library, err := repository.EnsureSystemLibraries(ctx, db, types.SingleSpaceID, "")
	require.NoError(t, err)
	library, err = repository.NewLibraryRepository(db).GetSystemLibraryByRole(ctx, types.SingleSpaceID, "ai-reports")
	require.NoError(t, err)
	library.IndexingStrategy = types.IndexingStrategy{KeywordEnabled: true}
	require.NoError(t, db.Save(library).Error)
	root := t.TempDir()
	engine := retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(db))
	documents, err := service.NewVaultDocumentService(db, nil, engine, root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, documents.Close()) })
	router := gin.New()
	router.ContextWithFallback = true
	router.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(ctx); c.Next() }, middleware.ErrorHandler())
	handler := NewVaultHandler(documents)
	router.POST("/api/v1/reports", handler.CreateReport)
	router.GET("/api/v1/vault/status", handler.Status)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/reports", bytes.NewBufferString(`{"title":"测试报告","markdown":"# 测试报告\n\nReportvaultmarker records verified findings."}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var result struct {
		Data types.Entry `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, "report", result.Data.SourceKind)
	require.Equal(t, "测试报告", result.Data.Title)
	require.Contains(t, result.Data.VaultPath, "reports/"+time.Now().UTC().Format("2006-01")+"/测试报告-")
	content, err := os.ReadFile(filepath.Join(root, result.Data.VaultPath))
	require.NoError(t, err)
	require.Contains(t, string(content), "Reportvaultmarker")
	hits, err := engine.Retrieve(ctx, []types.RetrieveParams{{RetrieverType: types.KeywordsRetrieverType, LibraryIDs: []string{library.ID}, Query: "Reportvaultmarker", TopK: 10}})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.NotEmpty(t, hits[0].Results)
	require.Equal(t, result.Data.ID, hits[0].Results[0].EntryID)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/vault/status", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var status struct {
		Data service.VaultStatus `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	require.Equal(t, root, status.Data.Root)
	require.EqualValues(t, 1, status.Data.Reports.IndexedFiles)
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/reports", bytes.NewBufferString(`{"title":"bad","markdown":"x","related_article_ids":["missing"]}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
}
