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

type stubSearchService struct {
	interfaces.SearchService
}

func (s *stubSearchService) Search(_ context.Context, _ types.EntrySearchRequest) (*types.EntrySearchResponse, error) {
	return &types.EntrySearchResponse{Results: []*types.EntrySearchResult{{EntryID: "article",
		Snippets: []types.EntrySearchSnippet{{Content: "chunk ![c](" + testResourceHandle + ")"}},
		// Medium regression, task 10: public URL mode also covers historical
		// version evidence nested in a merged file result.
		OtherVersionMatches: []*types.EntrySearchResult{{EntryID: "old-version", Snippets: []types.EntrySearchSnippet{{Content: "old ![o](" + testResourceHandle + ")", Context: "parent ![p](" + testResourceHandle + ")"}}}},
	}}}, nil
}

func TestSearchEntry_PublicResourceURLs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := &SearchHandler{
		service:     &stubSearchService{},
		fileService: &stubResourceFileService{url: "https://cdn.example.com/image.png"},
	}
	r.POST("/search", h.SearchEntry)

	body := bytes.NewBufferString(`{"query":"diagram"}`)
	req := httptest.NewRequest(http.MethodPost, "/search?resource_urls=public", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), testResourceHandle)
	assert.Contains(t, w.Body.String(), "cdn.example.com")
}

func TestSearchEntry_InvalidResourceURLMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := &SearchHandler{
		service:     &stubSearchService{},
		fileService: &stubResourceFileService{url: "https://cdn.example.com/image.png"},
	}
	r.POST("/search", h.SearchEntry)

	body := bytes.NewBufferString(`{"query":"diagram"}`)
	req := httptest.NewRequest(http.MethodPost, "/search?resource_urls=signed", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "resource_urls")
}

func TestSearchEntry_DefaultKeepsHandles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := &SearchHandler{
		service:     &stubSearchService{},
		fileService: &stubResourceFileService{url: "https://cdn.example.com/image.png"},
	}
	r.POST("/search", h.SearchEntry)

	body := bytes.NewBufferString(`{"query":"diagram"}`)
	req := httptest.NewRequest(http.MethodPost, "/search", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var resp struct {
		Data types.EntrySearchResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Data.Results, 1)
	assert.Contains(t, resp.Data.Results[0].Snippets[0].Content, testResourceHandle)
	assert.Contains(t, resp.Data.Results[0].OtherVersionMatches[0].Snippets[0].Content, testResourceHandle)
}
