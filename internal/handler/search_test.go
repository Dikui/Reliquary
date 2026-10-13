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

type entrySearchHandlerService struct {
	interfaces.SearchService
	request types.EntrySearchRequest
	calls   int
	failure error
}

func (s *entrySearchHandlerService) Search(_ context.Context, request types.EntrySearchRequest) (*types.EntrySearchResponse, error) {
	s.calls++
	s.request = request
	if s.failure != nil {
		return nil, s.failure
	}
	if strings.TrimSpace(request.Query) == "" {
		return nil, apperrors.NewBadRequestError("query is required")
	}
	return &types.EntrySearchResponse{Results: []*types.EntrySearchResult{{EntryID: "article", Title: "Article",
		Source: "article", ProcessingMode: "summary_only", Summary: &types.EntrySummary{Overview: "Summary"},
		Snippets: []types.EntrySearchSnippet{{Content: "Summary"}}, Entities: []types.EntrySearchEntity{}, Topics: []types.EntrySearchTopic{},
		SimilarReports: []*types.EntrySearchResult{{EntryID: "similar"}}, VaultPath: "articles/article.md", OriginalURL: "https://example.org/article"}}, ExcludedMissingPublishedAt: 2}, nil
}

func (s *entrySearchHandlerService) Options(context.Context) (*types.EntrySearchOptions, error) {
	if s.failure != nil {
		return nil, s.failure
	}
	return &types.EntrySearchOptions{Entities: []types.EntrySearchEntity{{ID: "entity", CanonicalName: "Entity", Aliases: []string{"Alias"}}}, Topics: []types.EntrySearchTopic{{ID: "topic", Name: "Topic"}}}, nil
}

func entrySearchHandlerRouter(svc interfaces.SearchService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	handler := NewSearchHandler(svc, nil)
	router.POST("/search", handler.SearchEntry)
	router.GET("/search/options", handler.Options)
	return router
}

func TestEntrySearchHTTPSharedRequestAndArticleResponse(t *testing.T) {
	svc := &entrySearchHandlerService{}
	router := entrySearchHandlerRouter(svc)
	request := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(`{"query":"query","content_types":["event","tutorial"],"entity_ids":["entity"],"topic_ids":["topic"],"sources":["article","mine"],"published_from":"2026-10-01","published_to":"2026-10-03","collapse_similar":false,"limit":20}`))
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
		Success bool                      `json:"success"`
		Data    types.EntrySearchResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.True(t, envelope.Success)
	require.EqualValues(t, 2, envelope.Data.ExcludedMissingPublishedAt)
	require.Len(t, envelope.Data.Results, 1)
	result := envelope.Data.Results[0]
	require.Equal(t, "article", result.EntryID)
	require.Equal(t, "summary_only", result.ProcessingMode)
	require.Equal(t, "Summary", result.Summary.Overview)
	require.Len(t, result.SimilarReports, 1)
	require.Equal(t, "similar", result.SimilarReports[0].EntryID)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/search/options", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "Alias")
	require.Contains(t, response.Body.String(), "Topic")
}

func TestEntrySearchHTTPRejectsOldAndUnknownParameters(t *testing.T) {
	for _, field := range []string{"library_ids", "library_id", "entry_ids", "query_text", "query_embedding", "match_count", "only_recommended", "unknown"} {
		t.Run(field, func(t *testing.T) {
			svc := &entrySearchHandlerService{}
			request := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(`{"query":"query","`+field+`":null}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			entrySearchHandlerRouter(svc).ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "unsupported search parameter")
			require.Contains(t, response.Body.String(), field)
			require.Zero(t, svc.calls)
		})
	}
	for _, body := range []string{`{`, `{"query":"query"}{"query":"extra"}`, `{"query":"query"}true`, `{"query":["wrong type"]}`} {
		svc := &entrySearchHandlerService{}
		request := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		entrySearchHandlerRouter(svc).ServeHTTP(response, request)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Zero(t, svc.calls)
	}
}

func TestHybridSearchHTTPRejectsRemovedLibraryScope(t *testing.T) {
	for _, field := range []string{"library_ids", "library_id", "scope"} {
		svc := &hybridSearchTestService{}
		response := performHybridSearchRequest(svc, `{"query_text":"query","`+field+`":[]}`)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), "POST /api/v1/search")
		require.Zero(t, svc.searchCalls)
	}
}

func TestEntrySearchHTTPDependencyErrorsAreSafe(t *testing.T) {
	for _, failure := range []error{
		fmt.Errorf("EmbedBatch API error: Http Status 503 <html>upstream secret</html>"),
		apperrors.NewInternalServerError("database credentials upstream secret").WithDetails("<html>upstream secret</html>"),
		fmt.Errorf("wrapped: %w", apperrors.NewServiceUnavailableError("<html>upstream secret</html>")),
	} {
		for _, path := range []string{"/search", "/search/options"} {
			method := http.MethodPost
			if strings.HasSuffix(path, "/options") {
				method = http.MethodGet
			}
			response := httptest.NewRecorder()
			entrySearchHandlerRouter(&entrySearchHandlerService{failure: failure}).ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(`{"query":"query"}`)))
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			var body struct {
				Success bool               `json:"success"`
				Error   apperrors.AppError `json:"error"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.False(t, body.Success)
			require.Equal(t, apperrors.ErrSearchUnavailable, body.Error.Code)
			require.Equal(t, "Entry search is temporarily unavailable. Please retry.", body.Error.Message)
			require.Nil(t, body.Error.Details)
			require.NotContains(t, response.Body.String(), "upstream")
			require.NotContains(t, response.Body.String(), "EmbedBatch")
			require.NotContains(t, response.Body.String(), "html")
		}
	}
}
