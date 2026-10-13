package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func TestErrorHandler_PreservesStructuredModelUsageDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ErrorHandler())
	router.DELETE("/models/:id", func(c *gin.Context) {
		_ = c.Error(apperrors.NewModelInUseError(
			"model is used by 2 library(s); reconfigure or remove those references before deleting",
			types.ModelUsageDetails{
				Libraries: []types.ModelUsageResource{
					{
						ID:       "library-1",
						Name:     "Product docs",
						Bindings: []types.ModelUsageBinding{types.ModelUsageBinding("generation_model_tier")},
					},
					{
						ID:   "library-2",
						Name: "Engineering",
						Bindings: []types.ModelUsageBinding{
							types.ModelUsageBindingEmbeddingModel,
							types.ModelUsageBindingSummaryModel,
						},
					},
				},
			},
		))
	})

	request := httptest.NewRequest(http.MethodDelete, "/models/model-1", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	var body struct {
		Success bool `json:"success"`
		Error   struct {
			Code    apperrors.ErrorCode     `json:"code"`
			Message string                  `json:"message"`
			Details types.ModelUsageDetails `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.False(t, body.Success)
	assert.Equal(t, apperrors.ErrModelInUse, body.Error.Code)
	assert.NotEmpty(t, body.Error.Message)
	require.Len(t, body.Error.Details.Libraries, 2)
	assert.Equal(t, "Product docs", body.Error.Details.Libraries[0].Name)
	assert.Equal(t,
		[]types.ModelUsageBinding{types.ModelUsageBindingEmbeddingModel, types.ModelUsageBindingSummaryModel},
		body.Error.Details.Libraries[1].Bindings,
	)
}
