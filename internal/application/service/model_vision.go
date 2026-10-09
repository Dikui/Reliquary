package service

import (
	"context"
	"errors"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func validateImageProcessingModel(model *types.Model) error {
	if !model.SupportsImageProcessing() {
		return apperrors.NewImageProcessingModelUnsupportedError()
	}
	return nil
}

// ValidateVLMConfig rejects unavailable or text-only models before saving configuration or uploading content.
func ValidateVLMConfig(ctx context.Context, models interfaces.ModelService, config types.VLMConfig) error {
	if config.ModelID == "" {
		return nil
	}
	if models == nil {
		return apperrors.NewImageProcessingModelUnavailableError()
	}
	model, err := models.GetModelByID(ctx, config.ModelID)
	if errors.Is(err, ErrModelNotFound) || (err == nil && model == nil) {
		return apperrors.NewImageProcessingModelNotFoundError(config.ModelID)
	}
	if err != nil {
		return err
	}
	return validateImageProcessingModel(model)
}

func (s *knowledgeService) validateProcessVLMConfig(ctx context.Context, kb *types.KnowledgeBase, overrides *types.KnowledgeProcessOverrides) error {
	return ValidateVLMConfig(ctx, s.modelService, ResolveProcessConfig(kb, overrides).VLMConfig)
}
