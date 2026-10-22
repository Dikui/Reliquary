package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

type reprocessLibraryService struct {
	stubArticleLibraryService
	library *types.Library
}

func (s *reprocessLibraryService) GetLibraryByID(context.Context, string) (*types.Library, error) {
	return s.library, nil
}

func TestHTTPReprocessCancelAndResumeAcrossLibraryTemplates(t *testing.T) {
	for _, template := range []string{types.TemplateOwn, types.TemplateExternalTimely, types.TemplateExternalDurable, types.TemplateGenerated} {
		t.Run(template, func(t *testing.T) {
			db := pgtest.Open(t)
			require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Owner"}).Error)
			library := &types.Library{ID: "library-1", SpaceID: 1, Name: "Fixture", Template: template, Type: types.LibraryTypeDocument}
			if template == types.TemplateExternalTimely {
				library.SourceEvidenceRule = "link_required"
			}
			require.NoError(t, db.Create(library).Error)
			kind := "article"
			if template == types.TemplateOwn {
				kind = "mine"
			}
			if template == types.TemplateGenerated {
				kind = "report"
			}
			entry := &types.Entry{ID: "reprocess", SpaceID: 1, LibraryID: library.ID, Type: "manual", SourceKind: kind, Title: "Saved", VaultBody: "# Complete saved content", ParseStatus: types.ParseStatusFailed, Description: "Caller summary", SummaryStatus: types.SummaryStatusCompleted}
			require.NoError(t, db.Create(entry).Error)
			queue := &ingestHandlerQueue{}
			bases := &reprocessLibraryService{library: library}
			svc, err := service.NewEntryService(db, &config.Config{}, repository.NewEntryRepository(db), bases, repository.NewSpaceRepository(db), nil, nil, nil, nil, nil, nil, queue, nil, nil, nil, nil)
			require.NoError(t, err)
			h := NewEntryHandler(nil, svc, bases, queue, nil)
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
				c.Set(types.SpaceIDContextKey.String(), uint64(1))
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.SpaceIDContextKey, uint64(1)))
				c.Next()
			})
			router.POST("/entries/:id/reparse", h.ReparseEntry)
			router.POST("/entries/:id/cancel", h.CancelEntryParse)
			router.PUT("/entries/:id", h.UpdateEntry)
			for _, action := range []string{"reparse", "cancel", "reparse"} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/entries/reprocess/"+action, nil))
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			}
			require.Len(t, queue.tasks, 2)
			var saved types.Entry
			require.NoError(t, db.First(&saved, "id=?", entry.ID).Error)
			require.Equal(t, types.ParseStatusPending, saved.ParseStatus)
			if template == types.TemplateOwn {
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPut, "/entries/reprocess", strings.NewReader(`{"title":"Updated own title","custom_metadata":{"owner":"me"}}`))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(response, request)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				require.NoError(t, db.First(&saved, "id=?", entry.ID).Error)
				require.Equal(t, "Updated own title", saved.Title)
				require.JSONEq(t, `{"owner":"me"}`, string(saved.CustomMetadata))
			}
			require.Empty(t, saved.Description)
			require.Equal(t, types.SummaryStatusPending, saved.SummaryStatus)
		})
	}
}
