package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type promptLibraryLookup struct {
	interfaces.LibraryService
	db *gorm.DB
}

func (s promptLibraryLookup) GetLibraryByID(ctx context.Context, id string) (*types.Library, error) {
	var library types.Library
	err := s.db.WithContext(ctx).Where("id=?", id).First(&library).Error
	return &library, err
}

func TestPromptMigrationAndLibraryIsolation(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, config.LoadPromptTemplates("../../config"))
	t.Cleanup(func() { config.SetPromptOverrides(nil) })
	const a = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	const b = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Prompt test','personal');
 INSERT INTO libraries(id,space_id,name,template,directory,embedding_model_id,summary_model_id,source_evidence_rule) VALUES
 ('`+a+`',1,'Library A','external_timely','prompt-a','','','link_required'),('`+b+`',1,'Library B','own','prompt-b','','','');
 INSERT INTO system_settings(key,value,value_type,category) VALUES
 ('prompt.article_processing','"Original custom understanding"','string','prompts'),
 ('prompt.evaluation_questions','"Original custom evaluation"','string','prompts');`).Error)
	migration, err := os.ReadFile(filepath.Join("../../migrations/versioned", "000047_library_prompts.up.sql"))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(migration)).Error)
	// Running the data-only upgrade again cannot replace an independent edit.
	require.NoError(t, db.Exec(string(migration)).Error)
	settings := service.NewSystemSettingService(repository.NewSystemSettingRepository(db), &config.Config{})
	h := handler.NewPromptHandler(promptLibraryLookup{db: db}, settings)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.UserIDContextKey, "test-user"))
	})
	r.GET("/prompts", h.List)
	r.PUT("/prompts/:id", h.Update)
	r.DELETE("/prompts/:id", h.Update)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		encoded, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	out := request(http.MethodGet, "/prompts?library_id="+a, nil)
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	var rows []types.SystemSetting
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &rows))
	require.Len(t, rows, len(config.PromptScopeIDs(true)))
	require.Contains(t, config.Prompt("article_processing", a), "Original custom understanding")
	require.Contains(t, config.Prompt("article_processing", a), "fact_allowed")
	require.Contains(t, config.Prompt("evaluation_questions_multi_library"), "Original custom evaluation")
	require.Equal(t, http.StatusOK, request(http.MethodPut, "/prompts/article_processing?library_id="+a, map[string]any{"value": "Library A independent prompt"}).Code)
	require.Equal(t, "Library A independent prompt", config.Prompt("article_processing", a))
	require.Contains(t, config.Prompt("article_processing", b), "Original custom understanding")
	require.Equal(t, http.StatusBadRequest, request(http.MethodPut, "/prompts/article_processing", map[string]any{"value": "wrong scope"}).Code)
	require.Equal(t, http.StatusNotFound, request(http.MethodGet, "/prompts?library_id=missing", nil).Code)
	require.Equal(t, http.StatusOK, request(http.MethodDelete, "/prompts/article_processing?library_id="+a, nil).Code)
	require.Equal(t, config.PromptDefaults()["article_processing"], config.Prompt("article_processing", a))
	require.Contains(t, config.Prompt("article_processing", b), "Original custom understanding")
	var original string
	require.NoError(t, db.Raw(`SELECT value#>>'{}' FROM system_settings WHERE key='prompt.article_processing'`).Scan(&original).Error)
	require.Equal(t, "Original custom understanding", original)
}
