package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestEntryListSourceKindFiltersPostgresRowsAndPagination(t *testing.T) {
	db := pgtest.Open(t)
	updated := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		id, kind, contentType, channel, status, library string
		starred                                         bool
		space                                           uint64
	}{
		{"article", "article", "event", "api", "completed", "library-1", true, 1},
		{"mine-event", "mine", "event", "api", "completed", "library-1", true, 1},
		{"mine-tutorial", "mine", "tutorial", "web", "completed", "library-1", false, 1},
		{"report", "report", "", "api", "completed", "library-1", false, 1},
		{"mine-deleting", "mine", "event", "api", "deleting", "library-1", true, 1},
		{"mine-other-base", "mine", "event", "api", "completed", "library-2", true, 1},
		{"mine-other-space", "mine", "event", "api", "completed", "library-1", true, 2},
	} {
		require.NoError(t, db.Create(&types.Entry{ID: row.id, SpaceID: row.space, LibraryID: row.library,
			SourceKind: row.kind, ContentType: row.contentType, Channel: row.channel, Starred: row.starred,
			Type: "manual", Source: "", Title: "filtertoken " + row.id, FileName: row.id + ".md", FileType: "md",
			ParseStatus: row.status, CreatedAt: updated, UpdatedAt: updated}).Error)
	}
	require.NoError(t, db.Model(&types.Entry{}).Where("id IN ?", []string{
		"article", "mine-event", "mine-deleting", "mine-other-base", "mine-other-space",
	}).UpdateColumn("incomplete", true).Error)
	gin.SetMode(gin.TestMode)
	h := &EntryHandler{libraryService: &stubArticleLibraryService{}, kgService: &classificationEntryService{repo: repository.NewEntryRepository(db)}}
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.SpaceIDContextKey.String(), uint64(1))
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.SpaceIDContextKey, uint64(1)))
		c.Next()
	})
	router.GET("/libraries/:id/entries", h.ListEntry)
	for _, test := range []struct {
		name, query string
		ids         []string
		total       int64
	}{
		{"all", "", []string{"article", "mine-event", "mine-tutorial", "report"}, 4},
		{"article", "source_kind=article", []string{"article"}, 1},
		{"mine", "source_kind=mine", []string{"mine-event", "mine-tutorial"}, 2},
		{"report", "source_kind=report", []string{"report"}, 1},
		{"empty", "source_kind=", []string{"article", "mine-event", "mine-tutorial", "report"}, 4},
		{"trimmed", "source_kind=%20mine%20", []string{"mine-event", "mine-tutorial"}, 2},
		{"combined", "source_kind=mine&source=api&content_type=event&starred=true&incomplete=true&file_type=md&parse_status=completed&keyword=filtertoken&start_time=2026-10-13T00:00:00Z&end_time=2026-10-14T00:00:00Z", []string{"mine-event"}, 1},
		{"missing-images", "incomplete=true", []string{"article", "mine-event"}, 2},
		{"without-missing-images", "incomplete=false", []string{"mine-tutorial", "report"}, 2},
		{"complete-mine", "source_kind=mine&incomplete=false", []string{"mine-tutorial"}, 1},
		{"other-channel", "source_kind=mine&source=web", []string{"mine-tutorial"}, 1},
		{"no-matches", "source_kind=report&content_type=tutorial", []string{}, 0},
		{"page-one", "source_kind=mine&page=1&page_size=1", nil, 2},
		{"page-two", "source_kind=mine&page=2&page_size=1", nil, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/libraries/library-1/entries?"+test.query, nil))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var result struct {
				Data     []types.Entry `json:"data"`
				Total    int64         `json:"total"`
				Page     int           `json:"page"`
				PageSize int           `json:"page_size"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.Equal(t, test.total, result.Total)
			ids := []string{}
			for _, row := range result.Data {
				ids = append(ids, row.ID)
				require.Equal(t, uint64(1), row.SpaceID)
				require.Equal(t, "library-1", row.LibraryID)
				require.NotEqual(t, types.ParseStatusDeleting, row.ParseStatus)
			}
			if test.ids != nil {
				require.ElementsMatch(t, test.ids, ids)
			} else {
				require.Len(t, ids, 1)
				require.Equal(t, 1, result.PageSize)
				require.Equal(t, "mine", result.Data[0].SourceKind)
			}
		})
	}
	for _, invalid := range []string{"MINE", "unknown", "mine,article", "mine%27%20OR%201%3D1"} {
		t.Run("invalid-"+invalid, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/libraries/library-1/entries?source_kind="+invalid, nil))
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
	t.Run("invalid-incomplete", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/libraries/library-1/entries?incomplete=invalid", nil))
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), "incomplete must be true or false")
	})
}
