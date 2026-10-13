package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type classificationEntryService struct {
	interfaces.EntryService
	repo interfaces.EntryRepository
}

func (s *classificationEntryService) ListPagedEntryByLibraryID(ctx context.Context, libraryID string, page *types.Pagination, filter types.EntryListFilter) (*types.PageResult, error) {
	rows, total, err := s.repo.ListPagedEntryByLibraryID(ctx, uint64(1), libraryID, page, filter)
	return &types.PageResult{Data: rows, Total: total, Page: page.Page, PageSize: page.PageSize}, err
}

func TestEntryListNormalizesContentTypeFilterWithPostgres(t *testing.T) {
	db := pgtest.Open(t)
	for _, contentType := range []string{types.ContentTypeEvent, types.ContentTypeTutorial, ""} {
		require.NoError(t, db.Create(&types.Entry{SpaceID: 1, LibraryID: "library-1", Type: "file", Title: "Article",
			Source: "manual", ParseStatus: types.ParseStatusCompleted, ContentType: contentType}).Error)
	}
	gin.SetMode(gin.TestMode)
	h := &EntryHandler{libraryService: &stubArticleLibraryService{}, kgService: &classificationEntryService{repo: repository.NewEntryRepository(db)}}
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.SpaceIDContextKey.String(), uint64(1))
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.SpaceIDContextKey, uint64(1)))
		c.Next()
	})
	router.GET("/libraries/:id/entries", h.ListEntry)
	for _, item := range []struct {
		value, expectedType string
		status              int
	}{{" event ", types.ContentTypeEvent, http.StatusOK}, {" unclassified ", "", http.StatusOK}, {"EVENT", "", http.StatusBadRequest}} {
		t.Run(item.value, func(t *testing.T) {
			response := httptest.NewRecorder()
			path := "/libraries/library-1/entries?content_type=" + url.QueryEscape(item.value)
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, item.status, response.Code, response.Body.String())
			if item.status == http.StatusOK {
				var result struct {
					Data  []types.Entry `json:"data"`
					Total int64         `json:"total"`
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				require.Equal(t, int64(1), result.Total)
				require.Len(t, result.Data, 1)
				require.Equal(t, item.expectedType, result.Data[0].ContentType)
			}
		})
	}
}
