package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type hybridSearchTestService struct {
	interfaces.KnowledgeBaseService
	searchCalls  int
	searchParams types.SearchParams
	results      []*types.SearchResult
}

func (s *hybridSearchTestService) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: id, SpaceID: 1}, nil
}

func (s *hybridSearchTestService) HybridSearch(
	_ context.Context,
	_ string,
	params types.SearchParams,
) ([]*types.SearchResult, error) {
	s.searchCalls++
	s.searchParams = params
	if s.results != nil {
		return s.results, nil
	}
	return []*types.SearchResult{}, nil
}

func newHybridSearchTestRouter(svc interfaces.KnowledgeBaseService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Use(func(c *gin.Context) {
		c.Set(types.SpaceIDContextKey.String(), uint64(1))
		c.Set(types.UserIDContextKey.String(), "u-test")
		c.Next()
	})
	handler := &KnowledgeBaseHandler{service: svc}
	router.POST("/knowledge-bases/:id/hybrid-search", handler.HybridSearch)
	return router
}

func TestHybridSearchRequiresEntrySearchEndpoint(t *testing.T) {
	for _, body := range []string{`{}`, `{"query_text":"query","match_count":3}`, `{"query":"query"}`, `{"entry_ids":["article"]}`, `{"query_embedding":[1,0,0],"disable_keywords_match":true}`} {
		svc := &hybridSearchTestService{}
		response := performHybridSearchRequest(svc, body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", response.Code, response.Body.String())
		}
		if svc.searchCalls != 0 {
			t.Fatalf("old route reached HybridSearch %d time(s)", svc.searchCalls)
		}
		if !strings.Contains(response.Body.String(), "POST /api/v1/search") {
			t.Fatalf("expected new endpoint guidance, got %s", response.Body.String())
		}
	}
}

func performHybridSearchRequest(svc interfaces.KnowledgeBaseService, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/knowledge-bases/kb-1/hybrid-search",
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	newHybridSearchTestRouter(svc).ServeHTTP(response, request)
	return response
}
