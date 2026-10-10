package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRetrievalConfigRejectsInvalidSimilarityRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct{ body, field string }{
		{`{"similar_reports_threshold":-0.1}`, "similar_reports_threshold"},
		{`{"similar_reports_threshold":1.1}`, "similar_reports_threshold"},
		{`{"similar_reports_window_days":-1}`, "similar_reports_window_days"},
		{`{"similar_reports_window_days":366}`, "similar_reports_window_days"},
	} {
		t.Run(test.body, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPut, "/space/kv/retrieval-config", strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			(&TenantHandler{}).updateTenantRetrievalConfigInternal(c)
			require.Len(t, c.Errors, 1)
			require.Contains(t, c.Errors[0].Error(), test.field)
		})
	}
}
