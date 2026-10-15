package service

import (
	"context"
	"errors"
	"net/http"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// ListVectorSets exposes only selectors accepted by the existing run resolver.
func (s *EvaluationService) ListVectorSets(ctx context.Context) ([]searchVectorSet, error) {
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok {
		return nil, apperrors.NewUnauthorizedError("workspace required")
	}
	var space types.Space
	if err := s.db.WithContext(ctx).First(&space, "id=?", spaceID).Error; err != nil {
		return nil, evaluationError(err)
	}
	active := searchVectorSet{Selector: "active", ModelID: space.EmbeddingModelID}
	var model types.Model
	if err := s.db.WithContext(ctx).Where("id=? AND (space_id=? OR is_builtin=true)", active.ModelID, spaceID).Find(&model).Error; err != nil {
		return nil, err
	}
	if model.ID != "" {
		active.Dimension = model.Parameters.EmbeddingParameters.Dimension
		active.ProfileHash = types.EmbeddingProfileHash(model.ID, active.Dimension, &model)
	}
	rows := []searchVectorSet{active}
	var runs []types.EmbeddingRebuildRun
	if err := s.db.WithContext(ctx).Where("space_id=? AND candidate=true AND status='completed' AND completed_count=total_count AND failed_count=0", spaceID).Order("created_at DESC,id").Find(&runs).Error; err != nil {
		return nil, err
	}
	for _, run := range runs {
		selector := "candidate:" + run.ID
		if run.SwitchedAt != nil {
			selector = "archive:" + run.ID
		}
		set, err := resolveSearchVectorSet(ctx, s.db, spaceID, selector, false)
		if err != nil {
			var appErr *apperrors.AppError
			if errors.As(err, &appErr) && appErr.HTTPCode == http.StatusBadRequest {
				continue
			}
			return nil, err
		}
		rows = append(rows, set)
	}
	return rows, nil
}
