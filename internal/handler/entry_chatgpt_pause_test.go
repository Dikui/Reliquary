package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type chatGPTPausedArticleEditor struct {
	interfaces.EntryService
	err error
}

func (s *chatGPTPausedArticleEditor) CorrectContentType(context.Context, string, string, bool) (*types.Entry, error) {
	return nil, s.err
}
func (s *chatGPTPausedArticleEditor) ResolveProcessingReview(context.Context, string, string, string, bool) (*types.Entry, error) {
	return nil, s.err
}

func TestChatGPTSynchronousUserOperationsReturnRetryablePause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, reason := range []string{"subscription_sharing_usage_limit_exceeded", "subscription_sharing_usage_unavailable", "daily_limit", "needs_login"} {
		for _, operation := range []struct{ method, path, body string }{
			{http.MethodPut, "/entries/article/content-type", `{"content_type":"research"}`},
			{http.MethodPost, "/entries/article/processing-review", `{"action":"correct","content_type":"research"}`},
		} {
			t.Run(reason+operation.path, func(t *testing.T) {
				retry := time.Now().Add(5 * time.Minute).UTC()
				h := &EntryHandler{kgService: &chatGPTPausedArticleEditor{err: fmt.Errorf("fixture generation: %w", &types.ChatGPTDeferredError{Reason: reason, RetryAt: retry})}}
				router := gin.New()
				router.Use(middleware.ErrorHandler())
				router.PUT("/entries/:id/content-type", h.CorrectContentType)
				router.POST("/entries/:id/processing-review", h.ResolveProcessingReview)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(operation.method, operation.path, strings.NewReader(operation.body)))
				require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
				seconds, err := strconv.Atoi(response.Header().Get("Retry-After"))
				require.NoError(t, err)
				require.Greater(t, seconds, 0)
				require.LessOrEqual(t, seconds, 300)
				var result struct {
					Success bool `json:"success"`
					Error   struct {
						Code    string `json:"code"`
						Message string `json:"message"`
						Details struct {
							Reason  string    `json:"reason"`
							RetryAt time.Time `json:"retry_at"`
							Manual  bool      `json:"manual_retry_required"`
						} `json:"details"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				require.False(t, result.Success)
				require.Equal(t, "chatgpt_generation_paused", result.Error.Code)
				require.Contains(t, result.Error.Message, "article is unchanged")
				require.Equal(t, reason, result.Error.Details.Reason)
				require.Equal(t, retry, result.Error.Details.RetryAt)
				require.True(t, result.Error.Details.Manual)
			})
		}
	}
}
