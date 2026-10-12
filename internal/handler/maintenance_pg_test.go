package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

type maintenanceCountedBody struct{ reads int }

func (b *maintenanceCountedBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (*maintenanceCountedBody) Close() error               { return nil }

func TestMaintenanceRejectsCollectionBeforeReadingUploadBody(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, maintenanceops.Pause(context.Background(), db, "snapshot", "backup", nil))
	h := NewMaintenanceHandler(db)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler(), h.CollectionGate)
	for _, path := range []string{"/api/v1/ingest", "/api/v1/ingest/assets"} {
		r.POST(path, func(c *gin.Context) { t.Error("paused collection handler executed") })
	}
	for _, path := range []string{"/api/v1/ingest", "/api/v1/ingest/assets"} {
		body := &maintenanceCountedBody{}
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Body = body
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
		require.Zero(t, body.reads)
	}
	r.GET("/api/v1/search/options", func(c *gin.Context) { c.Status(http.StatusOK) })
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/search/options", nil))
	require.Equal(t, http.StatusOK, response.Code)
}

func TestMaintenanceStatusAndScheduleRequireWebUserForUpdates(t *testing.T) {
	db := pgtest.Open(t)
	h := NewMaintenanceHandler(db)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.GET("/api/v1/maintenance/status", h.Status)
	r.PUT("/api/v1/maintenance/configure", h.Configure)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/maintenance/status", nil))
	require.Equal(t, http.StatusOK, response.Code)
	var output struct {
		Success bool                 `json:"success"`
		Data    maintenanceops.State `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &output))
	require.True(t, output.Success)
	require.Equal(t, "never", output.Data.BackupStatus)
	input := `{"backup_schedule":"0 3 * * 0","drill_schedule":"0 4 1 * *","schedule_timezone":"UTC","backup_directory":"/external/backups","automatic_enabled":true}`
	for _, principal := range []types.Principal{{Type: types.PrincipalAPIKey, ID: "collector"}, {Type: types.PrincipalWebUser, ID: "owner"}} {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/maintenance/configure", strings.NewReader(input))
		request.Header.Set("Content-Type", "application/json")
		request = request.WithContext(types.WithPrincipal(request.Context(), principal))
		response = httptest.NewRecorder()
		r.ServeHTTP(response, request)
		if principal.Type == types.PrincipalAPIKey {
			require.Equal(t, http.StatusForbidden, response.Code)
		} else {
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "/external/backups")
		}
	}
}
