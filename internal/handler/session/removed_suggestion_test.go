package session

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQARequestIgnoresLegacySuggestionAttribution(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/knowledge-chat/session", strings.NewReader(`{"query":"Tell me about the document","suggestion_attribution":{"set_id":"old","question_index":99}}`))
	c.Request.Header.Set("Content-Type", "application/json")
	var req CreateKnowledgeQARequest
	require.NoError(t, c.ShouldBindJSON(&req))
	require.Equal(t, "Tell me about the document", req.Query)
	encoded, err := json.Marshal(req)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "suggestion_attribution")
}
