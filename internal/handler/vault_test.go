package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestVaultStatusIncludesArchivedGeneratedMaterials(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID)
	require.NoError(t, service.EnsurePersonalSpace(ctx, db))
	library, err := repository.EnsureSystemLibraries(ctx, db, types.SingleSpaceID, "")
	require.NoError(t, err)
	library, err = repository.NewLibraryRepository(db).GetSystemLibraryByRole(ctx, types.SingleSpaceID, "ai-output")
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
	router.GET("/api/v1/vault/status", handler.Status)
	report, err := documents.CreateReport(ctx, "测试报告", "# 测试报告\n\nReportvaultmarker records verified findings.", nil)
	require.NoError(t, err)
	entry := report.Entry
	require.Equal(t, "report", entry.SourceKind)
	require.Equal(t, "测试报告", entry.Title)
	require.Contains(t, entry.VaultPath, library.Directory+"/测试报告-")
	var saved types.Entry
	require.NoError(t, db.First(&saved, "id=?", entry.ID).Error)
	require.Contains(t, saved.VaultBody, "Reportvaultmarker")
	hits, err := engine.Retrieve(ctx, []types.RetrieveParams{{RetrieverType: types.KeywordsRetrieverType, LibraryIDs: []string{library.ID}, Query: "Reportvaultmarker", TopK: 10}})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.NotEmpty(t, hits[0].Results)
	require.Equal(t, entry.ID, hits[0].Results[0].EntryID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/vault/status", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var status struct {
		Data service.VaultStatus `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	require.Equal(t, root, status.Data.Root)
	require.Len(t, status.Data.Libraries, 5)
	for _, item := range status.Data.Libraries {
		if item.LibraryID == library.ID {
			require.EqualValues(t, 1, item.ArchivedEntries)
		}
	}
}
