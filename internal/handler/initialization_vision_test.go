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

	apperrors "reliquary/internal/errors"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type initializationVisionModels struct {
	interfaces.ModelService
	selected *types.Model
}

func (s initializationVisionModels) GetModelByID(_ context.Context, id string) (*types.Model, error) {
	if id == "chat" {
		return &types.Model{ID: id, Type: types.ModelTypeKnowledgeQA}, nil
	}
	return s.selected, nil
}

type initializationVisionKB struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s initializationVisionKB) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type initializationVisionKBRepo struct {
	interfaces.KnowledgeBaseRepository
	saved *types.KnowledgeBase
}

func (s *initializationVisionKBRepo) UpdateKnowledgeBase(_ context.Context, kb *types.KnowledgeBase) error {
	copy := *kb
	s.saved = &copy
	return nil
}

func TestUpdateKBConfigValidatesVisionModelBeforeSaving(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name   string
		model  *types.Model
		status int
	}{
		{name: "dedicated VLLM", model: &types.Model{Type: types.ModelTypeVLLM}, status: http.StatusOK},
		{name: "vision conversation", model: &types.Model{Type: types.ModelTypeKnowledgeQA, Parameters: types.ModelParameters{SupportsVision: true}}, status: http.StatusOK},
		{name: "text-only conversation", model: &types.Model{Type: types.ModelTypeKnowledgeQA}, status: http.StatusBadRequest},
		{name: "missing model", status: http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			kb := &types.KnowledgeBase{ID: "kb-1", SummaryModelID: "previous-chat"}
			repo := &initializationVisionKBRepo{}
			handler := &InitializationHandler{
				modelService: initializationVisionModels{selected: tt.model},
				kbService:    initializationVisionKB{kb: kb}, kbRepository: repo,
			}
			router := gin.New()
			router.Use(middleware.ErrorHandler())
			router.PUT("/initialization/config/:kbId", handler.UpdateKBConfig)
			request := httptest.NewRequest(http.MethodPut, "/initialization/config/kb-1", strings.NewReader(`{"llmModelId":"chat","multimodal":{"enabled":true},"vlm_config":{"enabled":true,"model_id":"selected"}}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tt.status, response.Code, response.Body.String())
			if tt.status == http.StatusOK {
				require.NotNil(t, repo.saved)
				require.Equal(t, types.VLMConfig{Enabled: true, ModelID: "selected"}, repo.saved.VLMConfig)
			} else {
				require.Nil(t, repo.saved)
				require.Equal(t, "previous-chat", kb.SummaryModelID)
				var body map[string]any
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				require.Contains(t, response.Body.String(), "图片处理模型")
				expectedCode := apperrors.ErrImageProcessingModelUnsupported
				if tt.model == nil {
					expectedCode = apperrors.ErrImageProcessingModelNotFound
				}
				require.EqualValues(t, expectedCode, body["error"].(map[string]any)["code"])
			}
		})
	}
}
