package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"reliquary/internal/middleware"
	"testing"
)

func TestKnowledgeContextEntityDetailRejectsInvalidHistoryFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.GET("/knowledge-context/entities/:entity_id", NewKnowledgeContextHandler(nil, nil).EntityDetail)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/knowledge-context/entities/object?include_history=invalid", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid history flag")
}
