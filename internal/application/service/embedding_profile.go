package service

import (
	"context"
	"errors"
	"fmt"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type embeddingProfiles struct {
	db     *gorm.DB
	models interfaces.ModelService
}
type embeddingProfile struct {
	modelID   string
	dimension int
	hash      string
	embedder  embedding.Embedder
}

func (r *embeddingProfiles) profileMetadata(ctx context.Context, id string, spaceID uint64) (*types.Model, error) {
	var metadata types.Model
	err := r.db.WithContext(ctx).Where("id=? AND (space_id=? OR is_builtin=true)", id, spaceID).First(&metadata).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if metadata.Type != types.ModelTypeEmbedding || metadata.Status != types.ModelStatusActive {
		return nil, errors.New("library embedding model must be active and have Embedding type")
	}
	return &metadata, nil
}

func (r *embeddingProfiles) profile(ctx context.Context, spaceID uint64) (embeddingProfile, error) {
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, spaceID)
	id, err := ResolveModelTier(ctx, r.db, nil, types.ModelTierEmbedding)
	if err != nil {
		return embeddingProfile{}, err
	}
	return r.profileForModel(ctx, spaceID, id, true)
}

func (r *embeddingProfiles) profileForModel(
	ctx context.Context,
	spaceID uint64,
	id string,
	active bool,
) (embeddingProfile, error) {
	profile := embeddingProfile{modelID: id}
	if id == "" {
		return profile, errors.New("library embedding model is not configured")
	}
	if r.models == nil {
		return profile, errors.New("embedding model service is unavailable")
	}
	before, err := r.profileMetadata(ctx, id, spaceID)
	if err != nil {
		return profile, err
	}
	model, err := r.models.GetEmbeddingModel(types.WithModelTier(ctx, types.ModelTierEmbedding), id)
	if err != nil {
		return profile, fmt.Errorf("library embedding model is unavailable: %w", err)
	}
	if model == nil || model.GetDimensions() < 1 {
		return profile, errors.New("embedding model has no usable dimensions")
	}
	profile.dimension = model.GetDimensions()
	after, err := r.profileMetadata(ctx, id, spaceID)
	if err != nil {
		return profile, err
	}
	currentID, err := ResolveModelTier(ctx, r.db, nil, types.ModelTierEmbedding)
	if err != nil {
		return profile, err
	}
	if (active && currentID != id) ||
		types.EmbeddingProfileHash(
			id,
			profile.dimension,
			before,
		) != types.EmbeddingProfileHash(
			id,
			profile.dimension,
			after,
		) ||
		(before != nil && after != nil && !before.UpdatedAt.Equal(after.UpdatedAt)) {
		return profile, errors.New("embedding profile changed while constructing its provider client")
	}
	if after != nil && after.Parameters.EmbeddingParameters.Dimension > 0 &&
		after.Parameters.EmbeddingParameters.Dimension != profile.dimension {
		return profile, fmt.Errorf(
			"embedding model dimension %d does not match configured dimension %d",
			profile.dimension,
			after.Parameters.EmbeddingParameters.Dimension,
		)
	}
	profile.embedder = model
	profile.hash = types.EmbeddingProfileHash(id, profile.dimension, after)
	return profile, nil
}

func (r *embeddingProfiles) checkProfile(tx *gorm.DB, spaceID uint64, profile embeddingProfile) error {
	var space types.Space
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id",
		"embedding_model_id").Where("id=?",
		spaceID).First(&space).Error; err != nil {
		return err
	}
	if space.EmbeddingModelID != profile.modelID {
		return errors.New("space embedding profile changed during rebuild")
	}
	var metadata types.Model
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id=? AND (space_id=? OR is_builtin=true)", profile.modelID, spaceID).
		First(&metadata).
		Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var stored *types.Model
	if err == nil {
		stored = &metadata
	}
	if types.EmbeddingProfileHash(profile.modelID, profile.dimension, stored) != profile.hash {
		return errors.New("embedding model configuration changed during rebuild")
	}
	return nil
}

// checkEmbeddingProfile is called inside the short persistence transaction.
// A client created before a model edit cannot publish vectors under its ID.
func checkEmbeddingProfile(ctx context.Context, tx *gorm.DB, embedder embedding.Embedder, spaceID uint64) error {
	profile, ok := embedder.(embedding.ProfiledEmbedder)
	if !ok || profile.GetProfileHash() == "" {
		return nil
	}
	var space types.Space
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Select("embedding_model_id").Where("id=?", spaceID).First(&space).Error; err != nil {
		return err
	}
	if space.EmbeddingModelID != embedder.GetModelID() {
		return fmt.Errorf("space embedding model changed during vector generation")
	}
	var model types.Model
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND (space_id=? OR is_builtin=true)", embedder.GetModelID(), spaceID).First(&model).Error; err != nil {
		return err
	}
	if types.EmbeddingProfileHash(model.ID, model.Parameters.EmbeddingParameters.Dimension, &model) != profile.GetProfileHash() {
		return fmt.Errorf("embedding model profile changed during vector generation")
	}
	return nil
}
