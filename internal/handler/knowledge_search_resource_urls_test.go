package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type stubKnowledgeSearchService struct {
	interfaces.KnowledgeSearchService
}

func (s *stubKnowledgeSearchService) Search(_ context.Context, _ types.ArticleSearchRequest) (*types.ArticleSearchResponse, error) {
	return &types.ArticleSearchResponse{Results: []*types.ArticleSearchResult{{ArticleID: "article",
		Snippets: []types.ArticleSearchSnippet{{Content: "chunk ![c](" + testResourceHandle + ")"}},
	}}}, nil
}

func TestSearchKnowledge_PublicResourceURLs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := &KnowledgeSearchHandler{
		service:     &stubKnowledgeSearchService{},
		fileService: &stubResourceFileService{url: "https://cdn.example.com/image.png"},
	}
	r.POST("/knowledge-search", h.SearchKnowledge)

	body := bytes.NewBufferString(`{"query":"diagram"}`)
	req := httptest.NewRequest(http.MethodPost, "/knowledge-search?resource_urls=public", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), testResourceHandle)
	assert.Contains(t, w.Body.String(), "cdn.example.com")
}

func TestSearchKnowledge_InvalidResourceURLMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := &KnowledgeSearchHandler{
		service:     &stubKnowledgeSearchService{},
		fileService: &stubResourceFileService{url: "https://cdn.example.com/image.png"},
	}
	r.POST("/knowledge-search", h.SearchKnowledge)

	body := bytes.NewBufferString(`{"query":"diagram"}`)
	req := httptest.NewRequest(http.MethodPost, "/knowledge-search?resource_urls=signed", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "resource_urls")
}

func TestSearchKnowledge_DefaultKeepsHandles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := &KnowledgeSearchHandler{
		service:     &stubKnowledgeSearchService{},
		fileService: &stubResourceFileService{url: "https://cdn.example.com/image.png"},
	}
	r.POST("/knowledge-search", h.SearchKnowledge)

	body := bytes.NewBufferString(`{"query":"diagram"}`)
	req := httptest.NewRequest(http.MethodPost, "/knowledge-search", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var resp struct {
		Data types.ArticleSearchResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Data.Results, 1)
	assert.Contains(t, resp.Data.Results[0].Snippets[0].Content, testResourceHandle)
}
