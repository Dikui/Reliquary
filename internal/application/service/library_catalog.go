package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (s *libraryService) ListLibraries(ctx context.Context) ([]*types.Library, error) {
	return s.repo.ListLibraries(ctx, types.MustSpaceIDFromContext(ctx))
}

func (s *libraryService) CreateLibrary(ctx context.Context, request types.CreateLibraryRequest) (*types.Library, error) {
	library := &types.Library{ID: uuid.NewString(), SpaceID: types.MustSpaceIDFromContext(ctx), Name: request.Name, Description: request.Description, Template: request.Template, Directory: request.Directory, CreatedAt: time.Now(), UpdatedAt: time.Now(), ContentTypes: []types.LibraryContentType{}, RelationTypes: []types.LibraryRelationType{}}
	if library.Template == types.TemplateExternalTimely {
		library.SourceEvidenceRule = "link_required"
	}
	library.EnsureDefaults()
	library.ReviewCriteria = library.DefaultReviewCriteria()
	if err := types.ValidateLibrary(library); err != nil {
		return nil, apperrors.NewBadRequestError(err.Error())
	}
	if err := s.repo.CreateLibrary(ctx, library); err != nil {
		return nil, libraryWriteError(err)
	}
	return s.repo.GetLibraryByID(ctx, library.ID)
}

func libraryWriteError(err error) error {
	// Uniqueness and check violations are caller validation errors.
	type sqlState interface{ SQLState() string }
	var state sqlState
	if errors.As(err, &state) && (state.SQLState() == "23505" || state.SQLState() == "23514") {
		return apperrors.NewBadRequestError("library configuration conflicts with an existing directory or violates an immutable field")
	}
	return err
}

func (s *libraryService) ConfigureLibrary(ctx context.Context, id string, patch types.LibraryPatch) (*types.Library, error) {
	if patch.Template != nil || patch.Directory != nil || patch.SystemRole != nil {
		return nil, apperrors.NewBadRequestError("template, directory and system_role are immutable")
	}
	if patch.ReviewCriteria != nil || patch.SourceEvidenceRule != nil {
		return nil, apperrors.NewBadRequestError("review_criteria and source_evidence_rule are no longer configurable")
	}
	var result *types.Library
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked types.Library
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", id, types.MustSpaceIDFromContext(ctx)).First(&locked).Error; err != nil {
			return err
		}
		repo := repository.NewLibraryRepository(tx)
		library, err := repo.GetLibraryByID(ctx, id)
		if err != nil {
			return err
		}
		if patch.Name != nil {
			library.Name = *patch.Name
		}
		if patch.Description != nil {
			library.Description = *patch.Description
		}
		if patch.ModelTiers != nil {
			if patch.ModelTiers.ModelID(types.ModelTierEmbedding) != library.ModelTiers.ModelID(types.ModelTierEmbedding) {
				return apperrors.NewBadRequestError("configure search models in shared search settings")
			}
			models := *patch.ModelTiers
			models.Embedding = "" // The space validates its shared embedding setting separately.
			if err := ValidateModelTierConfig(ctx, tx, models); err != nil {
				return apperrors.NewBadRequestError(err.Error())
			}
			library.ModelTiers = *patch.ModelTiers
			library.SummaryModelID = library.ModelTiers.ModelID(types.ModelTierGeneration)
		}
		if patch.ChunkingConfig != nil {
			c := patch.ChunkingConfig
			if c.ChunkSize <= 0 || c.ChunkOverlap < 0 || c.ChunkOverlap >= c.ChunkSize {
				return apperrors.NewBadRequestError("chunk size must be positive and chunk overlap must be smaller than chunk size")
			}
			library.ChunkingConfig = *c
		}
		if patch.FeedEnabled != nil {
			library.FeedEnabled = *patch.FeedEnabled
		}
		if patch.ContentTypes != nil {
			library.ContentTypes = *patch.ContentTypes
			for i := range library.ContentTypes {
				library.ContentTypes[i].LibraryID = id
				library.ContentTypes[i].Position = i
			}
		}
		if patch.RelationTypes != nil {
			library.RelationTypes = *patch.RelationTypes
			for i := range library.RelationTypes {
				library.RelationTypes[i].LibraryID = id
				library.RelationTypes[i].Position = i
			}
		}
		if err := types.ValidateLibrary(library); err != nil {
			return apperrors.NewBadRequestError(err.Error())
		}
		library.UpdatedAt = time.Now()
		if err := repo.UpdateLibrary(ctx, library); err != nil {
			return libraryWriteError(err)
		}
		result = library
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = apperrors.NewNotFoundError("library not found")
	}
	return result, err
}

func (s *libraryService) DeleteLibrary(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var library types.Library
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", id, types.MustSpaceIDFromContext(ctx)).First(&library).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.NewNotFoundError("library not found")
			}
			return err
		}
		var count int64
		if err := tx.Unscoped().Model(&types.Entry{}).Where("library_id=?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return apperrors.NewBadRequestError("library contains entries")
		}
		// The row lock and foreign keys serialize creation against this empty-library deletion.
		return tx.Unscoped().Delete(&library).Error
	})
}
