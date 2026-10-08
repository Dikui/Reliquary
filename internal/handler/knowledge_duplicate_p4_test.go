package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type duplicateManualKnowledgeService struct{ interfaces.KnowledgeService }

func (duplicateManualKnowledgeService) CreateKnowledgeFromManual(context.Context, string, *types.ManualKnowledgePayload, string) (*types.Knowledge, error) {
	existing := &types.Knowledge{ID: "original", KnowledgeBaseID: "kb-1", DuplicateMatchMethod: "simhash"}
	return existing, &types.DuplicateKnowledgeError{Message: "Message already exists", Knowledge: existing}
}

func TestP4DuplicateManualKnowledgeHTTPReturnsExistingMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.UserIDContextKey, "personal-user"))
		c.Next()
	})
	handler := &KnowledgeHandler{kbService: &stubFolderKBService{}, kgService: duplicateManualKnowledgeService{}}
	router.POST("/knowledge-bases/:id/knowledge/manual", handler.CreateManualKnowledge)
	request := httptest.NewRequest(http.MethodPost, "/knowledge-bases/kb-1/knowledge/manual", strings.NewReader(`{"title":"Reprint","content":"body","status":"publish"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"code":"duplicate_manual"`)
	require.Contains(t, recorder.Body.String(), `"id":"original"`)
	require.Contains(t, recorder.Body.String(), `"match_method":"simhash"`)
}

func TestDuplicateKnowledgeHTTPMatchMethodDescribesCurrentIntake(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, duplicateType := range []string{"manual", "url", "file"} {
		for _, method := range []string{"url", "content_hash", "simhash", "file_hash", ""} {
			t.Run(duplicateType+"/"+method, func(t *testing.T) {
				existing := &types.Knowledge{ID: "original", KnowledgeBaseID: "kb-1", DuplicateMatchMethod: method,
					RepostSources: []types.KnowledgeRepostSource{{MatchMethod: "simhash"}}}
				for _, returned := range []*types.Knowledge{existing, nil} {
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/intake", nil)
					handler := &KnowledgeHandler{}
					require.True(t, handler.handleDuplicateKnowledgeError(c, &types.DuplicateKnowledgeError{Message: "duplicate", Knowledge: existing}, returned, duplicateType))
					require.Equal(t, http.StatusConflict, recorder.Code)
					var body map[string]any
					require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
					require.Equal(t, "duplicate_"+duplicateType, body["code"])
					require.Equal(t, "original", body["data"].(map[string]any)["id"])
					if method == "" {
						require.NotContains(t, body, "match_method", "historical repost methods do not describe the current attempt")
					} else {
						require.Equal(t, method, body["match_method"])
					}
				}
			})
		}
	}
}
