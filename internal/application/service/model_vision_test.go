package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type visionModelServiceStub struct {
	interfaces.ModelService
	model *types.Model
}

func (s visionModelServiceStub) GetModelByID(context.Context, string) (*types.Model, error) {
	if s.model == nil {
		return nil, ErrModelNotFound
	}
	return s.model, nil
}

func requireVisionBadRequest(t *testing.T, err error) {
	t.Helper()
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok, "expected application error, got %v", err)
	require.Equal(t, http.StatusBadRequest, appErr.HTTPCode)
	require.Contains(t, appErr.Message, "图片处理模型")
}

func TestValidateVLMConfigModelCapabilities(t *testing.T) {
	for _, tt := range []struct {
		name   string
		model  *types.Model
		accept bool
	}{
		{name: "dedicated VLLM", model: &types.Model{Type: types.ModelTypeVLLM}, accept: true},
		{name: "vision language model", model: &types.Model{Type: types.ModelTypeKnowledgeQA, Parameters: types.ModelParameters{SupportsVision: true}}, accept: true},
		{name: "text language model", model: &types.Model{Type: types.ModelTypeKnowledgeQA}},
		{name: "embedding with vision flag", model: &types.Model{Type: types.ModelTypeEmbedding, Parameters: types.ModelParameters{SupportsVision: true}}},
		{name: "missing model"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVLMConfig(context.Background(), visionModelServiceStub{model: tt.model}, types.VLMConfig{Enabled: true, ModelID: "selected-model"})
			if tt.accept {
				require.NoError(t, err)
			} else {
				requireVisionBadRequest(t, err)
				appErr, _ := apperrors.IsAppError(err)
				if tt.model == nil {
					require.Equal(t, apperrors.ErrImageProcessingModelNotFound, appErr.Code)
				} else {
					require.Equal(t, apperrors.ErrImageProcessingModelUnsupported, appErr.Code)
				}
			}
		})
	}
	require.NoError(t, ValidateVLMConfig(context.Background(), nil, types.VLMConfig{}))
	require.NoError(t, ValidateVLMConfig(context.Background(), nil, types.VLMConfig{ModelName: "legacy", BaseURL: "https://example.com/v1"}))
	// A supplied model ID is checked even when its enabled flag is false.
	requireVisionBadRequest(t, ValidateVLMConfig(context.Background(), visionModelServiceStub{model: &types.Model{Type: types.ModelTypeKnowledgeQA}}, types.VLMConfig{ModelID: "text-only"}))
}

func TestGetVLMModelRejectsTextOnlyLanguageModel(t *testing.T) {
	ctx := newCreateKnowledgeFileContext()
	svc := &modelService{repo: &stubModelRepoForDelete{model: &types.Model{ID: "text-only", Type: types.ModelTypeKnowledgeQA}}}
	model, err := svc.GetVLMModel(ctx, "text-only")
	require.Nil(t, model)
	requireVisionBadRequest(t, err)
}

func TestKnowledgeBaseCreationRejectsTextOnlyVLM(t *testing.T) {
	repo := newFakeKBRepo()
	svc := &knowledgeBaseService{repo: repo, modelService: visionModelServiceStub{model: &types.Model{Type: types.ModelTypeKnowledgeQA}}}
	kb, err := svc.CreateKnowledgeBase(newCreateKnowledgeFileContext(), &types.KnowledgeBase{VLMConfig: types.VLMConfig{Enabled: true, ModelID: "text-only"}})
	require.Nil(t, kb)
	requireVisionBadRequest(t, err)
	require.Empty(t, repo.rows)
}

func TestImageUploadRejectsTextOnlyVLMBeforeStorage(t *testing.T) {
	for _, override := range []bool{false, true} {
		name := "knowledge base config"
		if override {
			name = "upload override"
		}
		t.Run(name, func(t *testing.T) {
			config := types.VLMConfig{Enabled: true, ModelID: "text-only"}
			kb := &types.KnowledgeBase{ID: "kb-1"}
			var overrides *types.KnowledgeProcessOverrides
			if override {
				overrides = &types.KnowledgeProcessOverrides{VLMConfig: &config}
			} else {
				kb.VLMConfig = config
			}
			fileSvc := &createKnowledgeFileServiceStub{}
			queue := &createKnowledgeTaskEnqueuerStub{}
			svc := &knowledgeService{
				kbService: &createKnowledgeFileKBServiceStub{kb: kb}, fileSvc: fileSvc, task: queue,
				modelService: visionModelServiceStub{model: &types.Model{Type: types.ModelTypeKnowledgeQA}},
			}
			knowledge, err := svc.CreateKnowledgeFromFile(newCreateKnowledgeFileContext(), kb.ID, newMultipartFileHeader(t, "image.png", "png bytes"), nil, nil, "", "", overrides)
			require.Nil(t, knowledge)
			requireVisionBadRequest(t, err)
			require.Zero(t, fileSvc.saveCalls)
			require.Zero(t, queue.calls)
		})
	}
}

func TestValidateVLMConfigUnavailableHasStableCode(t *testing.T) {
	err := ValidateVLMConfig(context.Background(), nil, types.VLMConfig{ModelID: "selected"})
	requireVisionBadRequest(t, err)
	appErr, _ := apperrors.IsAppError(err)
	require.Equal(t, apperrors.ErrImageProcessingModelUnavailable, appErr.Code)
}
