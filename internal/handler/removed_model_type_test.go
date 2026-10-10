package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	apperrors "reliquary/internal/errors"
)

func TestModelHandlersRejectRemovedModelType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, modelType := range []string{"asr", "ASR"} {
		for _, action := range []string{"create", "update"} {
			t.Run(action+"/"+modelType, func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				body := `{"name":"removed","type":"` + modelType + `","source":"remote","parameters":{"provider":"generic"}}`
				c.Request = httptest.NewRequest(http.MethodPost, "/models/removed", strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Params = gin.Params{{Key: "id", Value: "removed"}}
				h := &ModelHandler{}
				if action == "create" {
					h.CreateModel(c)
				} else {
					h.UpdateModel(c)
				}
				require.Len(t, c.Errors, 1)
				var appErr *apperrors.AppError
				require.ErrorAs(t, c.Errors[0].Err, &appErr)
				require.Equal(t, apperrors.ErrValidation, appErr.Code)
				require.Equal(t, "不支持的模型类型: "+modelType, appErr.Message)
			})
		}
	}
}
