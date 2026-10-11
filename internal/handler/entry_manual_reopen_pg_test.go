package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/application/service/file"
	"reliquary/internal/config"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestManualDraftHTTPReopenReadsSavedBodyWithoutRestart(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Personal space"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-1", SpaceID: 1, Name: "Articles", Type: types.KnowledgeBaseTypeDocument}).Error)
	bases := &stubArticleKBService{}
	entries, err := service.NewEntryService(db, &config.Config{}, repository.NewEntryRepository(db), bases,
		repository.NewSpaceRepository(db), nil, nil, nil, file.NewLocalFileService(t.TempDir(), ""), nil, nil,
		&ingestHandlerQueue{}, nil, nil, docparser.NewImageResolver(), nil)
	require.NoError(t, err)
	handler := NewEntryHandler(nil, entries, bases, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.SpaceIDContextKey.String(), uint64(1))
		ctx := context.WithValue(c.Request.Context(), types.SpaceIDContextKey, uint64(1))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.POST("/knowledge-bases/:id/manual", handler.CreateManualEntry)
	router.PUT("/entries/:id/manual", handler.UpdateManualEntry)
	router.GET("/entries/:id", handler.GetEntry)
	router.GET("/entries/:id/preview", handler.PreviewEntryFile)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, req)
		return response
	}
	created := request(http.MethodPost, "/knowledge-bases/kb-1/manual", `{"title":"Old title","content":"# Old body\n\nOriginal draft.","status":"draft"}`)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var result struct {
		Data types.Entry `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &result))
	path := "/entries/" + result.Data.ID
	first := request(http.MethodGet, path+"/preview", "")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.Equal(t, "# Old body\n\nOriginal draft.", first.Body.String())
	updated := request(http.MethodPut, path+"/manual", `{"title":"New title","content":"# New body\n\nSaved in the same process.","status":"draft"}`)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	detail := request(http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &result))
	require.Equal(t, "New title", result.Data.Title)
	metadata, err := result.Data.ManualMetadata()
	require.NoError(t, err)
	require.Empty(t, metadata.Content, "the editor must read the stored original through preview")
	second := request(http.MethodGet, path+"/preview", "")
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.Equal(t, "# New body\n\nSaved in the same process.", second.Body.String())
	// A stable preview URL must reach HTTP again when the editor reopens.
	// Otherwise a browser can reuse the old response even though fresh reads work.
	require.Equal(t, "no-store", first.Header().Get("Cache-Control"))
	require.Equal(t, "no-store", second.Header().Get("Cache-Control"))
}
