package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Customer installation's subscription endpoint always requires an API key.
func TestAgentModelRequiresAPIKey(t *testing.T) {
	router := gin.New()
	router.POST("/agent/models/:model_id/responses", (&AgentModelHandler{}).Respond)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/agent/models/model/responses", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusUnauthorized, response.Code)
}
