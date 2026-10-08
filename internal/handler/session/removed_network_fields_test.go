package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type missingSessionForRemovedFields struct{ interfaces.SessionService }

func (*missingSessionForRemovedFields) GetOwnedSession(context.Context, string) (*types.Session, error) {
	return nil, errors.New("missing session")
}

func TestQARequestRemovedNetworkFieldsFollowUnknownFieldRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, extra := range []string{
		`"unknown_option": {"ignored": true}`,
		`"web_search_enabled": true, "web_search_max_results": 5`,
		`"web_search_enabled": {"ignored": true}, "web_search_provider_id": 42`,
	} {
		t.Run(extra, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Params = gin.Params{{Key: "session_id", Value: "missing"}}
			c.Request = httptest.NewRequest(http.MethodPost, "/knowledge-chat/missing", strings.NewReader(`{"query":"question",`+extra+`}`))
			c.Request.Header.Set("Content-Type", "application/json")
			h := &Handler{sessionService: &missingSessionForRemovedFields{}}
			_, _, err := h.parseQARequest(c, "test")
			var appErr *apperrors.AppError
			require.ErrorAs(t, err, &appErr)
			// Binding accepts unknown fields and reaches the normal session lookup.
			require.Equal(t, http.StatusNotFound, appErr.HTTPCode)
			require.Equal(t, "Session not found", appErr.Message)
		})
	}
}
