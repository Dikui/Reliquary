package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type articleSearchHandlerService struct {
	interfaces.KnowledgeSearchService
	request types.ArticleSearchRequest
	calls   int
	failure error
}

func (s *articleSearchHandlerService) Search(_ context.Context, request types.ArticleSearchRequest) (*types.ArticleSearchResponse, error) {
	s.calls++
	s.request = request
	if s.failure != nil {
		return nil, s.failure
	}
	if strings.TrimSpace(request.Query) == "" {
		return nil, apperrors.NewBadRequestError("query is required")
	}
	return &types.ArticleSearchResponse{Results: []*types.ArticleSearchResult{{ArticleID: "article", Title: "Article",
		Source: "article", ProcessingMode: "summary_only", Summary: &types.KnowledgeSummary{Overview: "Summary"},
		Snippets: []types.ArticleSearchSnippet{{Content: "Summary"}}, Entities: []types.ArticleSearchEntity{}, Topics: []types.ArticleSearchTopic{},
		SimilarReports: []*types.ArticleSearchResult{{ArticleID: "similar"}}, VaultPath: "articles/article.md", OriginalURL: "https://example.org/article"}}, ExcludedMissingPublishedAt: 2}, nil
}

func (s *articleSearchHandlerService) Options(context.Context) (*types.ArticleSearchOptions, error) {
	if s.failure != nil {
		return nil, s.failure
	}
	return &types.ArticleSearchOptions{Entities: []types.ArticleSearchEntity{{ID: "entity", CanonicalName: "Entity", Aliases: []string{"Alias"}}}, Topics: []types.ArticleSearchTopic{{ID: "topic", Name: "Topic"}}}, nil
}

func articleSearchHandlerRouter(svc interfaces.KnowledgeSearchService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	handler := NewKnowledgeSearchHandler(svc, nil)
	router.POST("/knowledge-search", handler.SearchKnowledge)
	router.GET("/knowledge-search/options", handler.Options)
	return router
}

func TestArticleSearchHTTPSharedRequestAndArticleResponse(t *testing.T) {
	svc := &articleSearchHandlerService{}
	router := articleSearchHandlerRouter(svc)
	request := httptest.NewRequest(http.MethodPost, "/knowledge-search", strings.NewReader(`{"query":"query","content_types":["event","tutorial"],"entity_ids":["entity"],"topic_ids":["topic"],"sources":["article","mine"],"published_from":"2026-10-01","published_to":"2026-10-03","collapse_similar":false,"limit":20}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "query", svc.request.Query)
	require.Equal(t, []string{"event", "tutorial"}, svc.request.ContentTypes)
	require.Equal(t, []string{"entity"}, svc.request.EntityIDs)
	require.Equal(t, []string{"topic"}, svc.request.TopicIDs)
	require.Equal(t, []string{"article", "mine"}, svc.request.Sources)
	require.Equal(t, "2026-10-03", svc.request.PublishedTo)
	require.NotNil(t, svc.request.CollapseSimilar)
	require.False(t, *svc.request.CollapseSimilar)
	require.Equal(t, 20, svc.request.Limit)
	var envelope struct {
		Success bool                        `json:"success"`
		Data    types.ArticleSearchResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.True(t, envelope.Success)
	require.EqualValues(t, 2, envelope.Data.ExcludedMissingPublishedAt)
	require.Len(t, envelope.Data.Results, 1)
	result := envelope.Data.Results[0]
	require.Equal(t, "article", result.ArticleID)
	require.Equal(t, "summary_only", result.ProcessingMode)
	require.Equal(t, "Summary", result.Summary.Overview)
	require.Len(t, result.SimilarReports, 1)
	require.Equal(t, "similar", result.SimilarReports[0].ArticleID)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/knowledge-search/options", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "Alias")
	require.Contains(t, response.Body.String(), "Topic")
}

func TestArticleSearchHTTPRejectsOldAndUnknownParameters(t *testing.T) {
	for _, field := range []string{"knowledge_base_ids", "knowledge_base_id", "knowledge_ids", "query_text", "query_embedding", "match_count", "only_recommended", "starred", "unknown"} {
		t.Run(field, func(t *testing.T) {
			svc := &articleSearchHandlerService{}
			request := httptest.NewRequest(http.MethodPost, "/knowledge-search", strings.NewReader(`{"query":"query","`+field+`":null}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			articleSearchHandlerRouter(svc).ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "unsupported search parameter")
			require.Contains(t, response.Body.String(), field)
			require.Zero(t, svc.calls)
		})
	}
	for _, body := range []string{`{`, `{"query":"query"}{"query":"extra"}`, `{"query":"query"}true`, `{"query":["wrong type"]}`} {
		svc := &articleSearchHandlerService{}
		request := httptest.NewRequest(http.MethodPost, "/knowledge-search", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		articleSearchHandlerRouter(svc).ServeHTTP(response, request)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Zero(t, svc.calls)
	}
}

func TestHybridSearchHTTPRejectsRemovedKnowledgeBaseScope(t *testing.T) {
	for _, field := range []string{"knowledge_base_ids", "knowledge_base_id", "scope"} {
		svc := &hybridSearchTestService{}
		response := performHybridSearchRequest(svc, `{"query_text":"query","`+field+`":[]}`)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), "POST /api/v1/knowledge-search")
		require.Zero(t, svc.searchCalls)
	}
}

func TestArticleSearchHTTPDependencyErrorsAreSafe(t *testing.T) {
	for _, failure := range []error{
		fmt.Errorf("EmbedBatch API error: Http Status 503 <html>upstream secret</html>"),
		apperrors.NewInternalServerError("database credentials upstream secret").WithDetails("<html>upstream secret</html>"),
		fmt.Errorf("wrapped: %w", apperrors.NewServiceUnavailableError("<html>upstream secret</html>")),
	} {
		for _, path := range []string{"/knowledge-search", "/knowledge-search/options"} {
			method := http.MethodPost
			if strings.HasSuffix(path, "/options") {
				method = http.MethodGet
			}
			response := httptest.NewRecorder()
			articleSearchHandlerRouter(&articleSearchHandlerService{failure: failure}).ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(`{"query":"query"}`)))
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			var body struct {
				Success bool               `json:"success"`
				Error   apperrors.AppError `json:"error"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.False(t, body.Success)
			require.Equal(t, apperrors.ErrKnowledgeSearchUnavailable, body.Error.Code)
			require.Equal(t, "Knowledge search is temporarily unavailable. Please retry.", body.Error.Message)
			require.Nil(t, body.Error.Details)
			require.NotContains(t, response.Body.String(), "upstream")
			require.NotContains(t, response.Body.String(), "EmbedBatch")
			require.NotContains(t, response.Body.String(), "html")
		}
	}
}
