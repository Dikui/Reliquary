package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/graph"
	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (s *libraryService) ListLibraries(ctx context.Context) ([]*types.Library, error) {
	return s.repo.ListLibraries(ctx, types.MustSpaceIDFromContext(ctx))
}

func (s *libraryService) CreateLibrary(ctx context.Context, request types.CreateLibraryRequest) (*types.Library, error) {
	request.Template = strings.TrimSpace(request.Template)
	if request.Template != "" && request.Template != types.TemplateStandard {
		return nil, apperrors.NewBadRequestError("template must be standard")
	}
	library := &types.Library{ID: uuid.NewString(), SpaceID: types.MustSpaceIDFromContext(ctx), Name: request.Name, Description: request.Description, Template: request.Template, Directory: request.Directory, CreatedAt: time.Now(), UpdatedAt: time.Now(), ContentTypes: []types.LibraryContentType{}, RelationTypes: []types.LibraryRelationType{}}
	library.EntityMode, library.EntitySchema = request.EntityMode, request.EntitySchema
	library.ProductLibraryIDs, library.DocumentRelationTypes = request.ProductLibraryIDs, request.DocumentRelationTypes
	if request.Color != nil {
		if err := types.ValidateLibraryColor(*request.Color); err != nil {
			return nil, apperrors.NewBadRequestError(err.Error())
		}
		library.Color = *request.Color
	}

	library.EnsureDefaults()
	library.ReviewCriteria = library.DefaultReviewCriteria()
	if err := types.ValidateLibrary(library); err != nil {
		return nil, apperrors.NewBadRequestError(err.Error())
	}
	if err := validateProductLibraries(s.db.WithContext(ctx), library); err != nil {
		return nil, err
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
	if patch.PromptOverrides != nil {
		if err := types.ValidateLibraryPromptOverrides(*patch.PromptOverrides); err != nil {
			return nil, apperrors.NewBadRequestError(err.Error())
		}
	}
	if patch.Color != nil {
		if err := types.ValidateLibraryColor(*patch.Color); err != nil {
			return nil, apperrors.NewBadRequestError(err.Error())
		}
	}
	if patch.Template != nil || patch.Directory != nil || patch.SystemRole != nil {
		return nil, apperrors.NewBadRequestError("template, directory and system_role are immutable")
	}
	if patch.ReviewCriteria != nil || patch.SourceEvidenceRule != nil {
		return nil, apperrors.NewBadRequestError("review_criteria and source_evidence_rule are no longer configurable")
	}
	var result *types.Library
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, types.MustSpaceIDFromContext(ctx)); err != nil {
			return err
		}
		var locked types.Library
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", id, types.MustSpaceIDFromContext(ctx)).First(&locked).Error; err != nil {
			return err
		}
		repo := repository.NewLibraryRepository(tx)
		library, err := repo.GetLibraryByID(ctx, id)
		if err != nil {
			return err
		}
		previous := *library
		if patch.ProductLibraryIDs != nil {
			library.ProductLibraryIDs = *patch.ProductLibraryIDs
		}
		if patch.DocumentRelationTypes != nil {
			library.DocumentRelationTypes = *patch.DocumentRelationTypes
		}
		if patch.EntityMode != nil {
			library.EntityMode = *patch.EntityMode
		}
		if patch.EntitySchema != nil {
			library.EntitySchema = *patch.EntitySchema
		}
		if patch.Name != nil {
			library.Name = *patch.Name
		}
		if patch.Description != nil {
			library.Description = *patch.Description
		}
		if patch.PromptOverrides != nil {
			library.PromptOverrides = *patch.PromptOverrides
		}
		if patch.Color != nil {
			library.Color = *patch.Color
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
		if err := validateProductLibraries(tx, library); err != nil {
			return err
		}
		if !previous.ManagesEntities() && library.ManagesEntities() {
			if err := convertLibraryEntities(ctx, tx, library, patch.EntityKindMapping); err != nil {
				return err
			}
		}
		if err := validateManagedCatalogChange(tx, &previous, library); err != nil {
			return err
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
		spaceID := types.MustSpaceIDFromContext(ctx)
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
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
		if library.ManagesEntities() {
			if err := tx.Model(&types.Entity{}).Where("library_id=?", id).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return apperrors.NewBadRequestError("library contains managed entities, including archived entities")
			}
			if err := tx.Model(&types.EntityAttachment{}).Where("library_id=?", id).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return apperrors.NewBadRequestError("library contains product attachments")
			}
			if err := tx.Model(&types.Library{}).Where("id<>? AND product_library_ids @> jsonb_build_array(?::text)", id, id).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return apperrors.NewBadRequestError("other libraries still use this product library")
			}
		}
		if err := graph.Lock(tx, spaceID); err != nil {
			return err
		}
		var ids []string
		if err := tx.Model(&types.Entity{}).Where("space_id=? AND library_id=?", spaceID, id).Pluck("id", &ids).Error; err != nil {
			return err
		}
		for _, model := range []any{&types.Relation{}, &types.RelationSuppression{}, &types.Topic{}} {
			if err := tx.Where("space_id=? AND library_id=?", spaceID, id).Delete(model).Error; err != nil {
				return err
			}
		}
		if len(ids) > 0 {
			if err := tx.Where("space_id=? AND (new_entity_id IN ? OR candidate_entity_id IN ?)", spaceID, ids, ids).Delete(&types.EntityMergeSuggestion{}).Error; err != nil {
				return err
			}
			if err := tx.Where("space_id=? AND (keep_entity_id IN ? OR merge_entity_id IN ?)", spaceID, ids, ids).Delete(&types.EntityMergeRecord{}).Error; err != nil {
				return err
			}
			if err := tx.Where("space_id=? AND library_id=?", spaceID, id).Delete(&types.Entity{}).Error; err != nil {
				return err
			}
			for _, entityID := range ids {
				if err := notifyEntityProjection(tx, spaceID, entityID); err != nil {
					return err
				}
			}
		}
		// The row lock and foreign keys serialize creation against this empty-library deletion.
		return tx.Unscoped().Delete(&library).Error
	})
}
