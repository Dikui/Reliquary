package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// ErrInvalidSpaceID represents an error for invalid space ID
var ErrInvalidSpaceID = errors.New("invalid space ID")

// libraryService implements the library service interface
type libraryService struct {
	db             *gorm.DB
	repo           interfaces.LibraryRepository
	entryRepo      interfaces.EntryRepository
	chunkRepo      interfaces.ChunkRepository
	modelService   interfaces.ModelService
	retrieveEngine interfaces.RetrievalService

	resourceCatalog        interfaces.ResourceCatalog
	nameEmbeddingRebuilder EntityNameEmbeddingRebuildTrigger
}

// NewLibraryService creates a new library service
func NewLibraryService(db *gorm.DB, repo interfaces.LibraryRepository,
	entryRepo interfaces.EntryRepository,
	chunkRepo interfaces.ChunkRepository,
	modelService interfaces.ModelService,
	retrieveEngine interfaces.RetrievalService,

	resourceCatalog interfaces.ResourceCatalog,
) interfaces.LibraryService {
	return &libraryService{
		db:             db,
		repo:           repo,
		entryRepo:      entryRepo,
		chunkRepo:      chunkRepo,
		modelService:   modelService,
		retrieveEngine: retrieveEngine,

		resourceCatalog: resourceCatalog,
	}
}

// GetSystemLibrary selects a stable role. The no-argument legacy settings endpoint selects institutional.
func (s *libraryService) GetSystemLibrary(ctx context.Context, roles ...string) (*types.Library, error) {
	library, err := s.repo.GetSystemLibraryByRole(ctx, types.MustSpaceIDFromContext(ctx), roles...)
	if err != nil {
		return nil, err
	}
	library.EnsureDefaults()
	return library, nil
}

// Lock the existing row while persisting library settings.
func (s *libraryService) persistLibrary(ctx context.Context, library *types.Library) error {
	if s.db == nil {
		return s.repo.UpdateLibrary(ctx, library)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var active types.Library
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("space_id = ? AND id = ?", library.SpaceID, library.ID).First(&active).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.NewNotFoundError("library not found")
			}
			return err
		}
		return repository.NewLibraryRepository(tx).UpdateLibrary(ctx, library)
	})
}

// GetLibraryByID retrieves a library by its ID
func (s *libraryService) GetLibraryByID(ctx context.Context, id string) (*types.Library, error) {
	if id == "" {
		logger.Error(ctx, "Library ID is empty")
		return nil, errors.New("library ID cannot be empty")
	}

	library, err := s.repo.GetLibraryByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"library_id": id,
		})
		return nil, err
	}

	if library == nil || library.SpaceID != types.SingleSpaceID {
		return nil, repository.ErrLibraryNotFound
	}
	library.EnsureDefaults()
	return library, nil
}

func (s *libraryService) GetLibraryByIDOnly(ctx context.Context, id string) (*types.Library, error) {
	if id == "" {
		logger.Error(ctx, "Library ID is empty")
		return nil, errors.New("library ID cannot be empty")
	}

	library, err := s.repo.GetLibraryByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"library_id": id,
		})
		return nil, err
	}

	if library == nil || library.SpaceID != types.SingleSpaceID {
		return nil, repository.ErrLibraryNotFound
	}
	library.EnsureDefaults()
	return library, nil
}

// GetLibrariesByIDsOnly retrieves team libraries by IDs (batch).
func (s *libraryService) GetLibrariesByIDsOnly(ctx context.Context, ids []string) ([]*types.Library, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	libraries, err := s.repo.GetLibraryByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	teamLibraries := make([]*types.Library, 0, len(libraries))
	for _, library := range libraries {
		if library != nil && library.SpaceID == types.SingleSpaceID {
			library.EnsureDefaults()
			teamLibraries = append(teamLibraries, library)
		}
	}
	return teamLibraries, nil
}

// FillLibraryCounts fills EntryCount, ChunkCount, IsProcessing, ProcessingCount for the given Library using library.SpaceID.
func (s *libraryService) FillLibraryCounts(ctx context.Context, library *types.Library) error {
	if library == nil {
		return nil
	}
	spaceID := library.SpaceID
	library.EnsureDefaults()
	if cnt, err := s.entryRepo.CountEntryByLibraryID(ctx, spaceID, library.ID); err == nil {
		library.EntryCount = cnt
	}
	if processingCount, err := s.entryRepo.CountEntryByStatus(ctx, spaceID, library.ID, []string{"pending", "processing"}); err == nil {
		library.IsProcessing = processingCount > 0
		library.ProcessingCount = processingCount
	}
	return nil
}

// UpdateLibrary updates a library's mutable properties.
func (s *libraryService) UpdateLibrary(ctx context.Context,
	id string,
	name string,
	description string,
	config *types.LibraryConfig,
) (*types.Library, error) {
	if s.nameEmbeddingRebuilder != nil && s.db != nil && !entityDatabaseInTransaction(s.db) {
		var result *types.Library
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			copy := *s
			copy.db = tx
			copy.repo = repository.NewLibraryRepository(tx)
			bound, err := s.nameEmbeddingRebuilder.BindTransaction(ctx, tx)
			if err != nil {
				return err
			}
			copy.nameEmbeddingRebuilder = bound
			result, err = copy.UpdateLibrary(ctx, id, name, description, config)
			return err
		})
		return result, err
	}

	if id == "" {
		logger.Error(ctx, "Library ID is empty")
		return nil, errors.New("library ID cannot be empty")
	}

	logger.Infof(ctx, "Updating library, ID: %s, name: %s", id, name)

	// Get existing library
	library, err := s.repo.GetLibraryByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"library_id": id,
		})
		return nil, err
	}

	original := *library
	current := *library
	library = &current
	previousEmbeddingModelID, _ := ResolveModelTier(ctx, s.db, library, types.ModelTierEmbedding)
	// Update the library properties
	library.Name = name
	library.Description = description
	if config != nil {
		if config.ChunkingConfig != nil {
			if config.ChunkingConfig.ChunkSize <= 0 || config.ChunkingConfig.ChunkOverlap < 0 || config.ChunkingConfig.ChunkOverlap >= config.ChunkingConfig.ChunkSize {
				return nil, apperrors.NewBadRequestError("chunk size must be positive and chunk overlap must be smaller than chunk size")
			}
			library.ChunkingConfig = *config.ChunkingConfig
		}
		// Update the enabled indexing strategy.
		if config.IndexingStrategy != nil {
			if !config.IndexingStrategy.HasAnyIndexing() {
				return nil, apperrors.NewBadRequestError("at least one indexing strategy must be enabled")
			}
			library.IndexingStrategy = *config.IndexingStrategy

		}
	}
	if config != nil && config.ModelTiers != nil {
		if err := ValidateModelTierConfig(ctx, s.db, *config.ModelTiers); err != nil {
			return nil, err
		}
		library.ModelTiers = *config.ModelTiers
		// Scalar bindings mirror the same settings for shared indexing consumers.
		library.SummaryModelID = library.ModelTiers.ModelID(types.ModelTierGeneration)
		library.EmbeddingModelID = library.ModelTiers.ModelID(types.ModelTierEmbedding)
	}
	library.UpdatedAt = time.Now()
	library.EnsureDefaults()

	logger.Info(ctx, "Saving library update")
	if err := s.persistLibrarySettings(ctx, library, &original, config); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"library_id": id,
		})
		return nil, err
	}
	currentEmbeddingModelID, _ := ResolveModelTier(ctx, s.db, library, types.ModelTierEmbedding)
	if s.nameEmbeddingRebuilder != nil && previousEmbeddingModelID != currentEmbeddingModelID {
		if err := s.nameEmbeddingRebuilder.MarkAndEnqueue(ctx, library.SpaceID, "library_embedding_model_changed"); err != nil {
			return library, fmt.Errorf("library update could not enqueue name embedding rebuild: %w", err)
		}
	}

	logger.Infof(ctx, "Library updated successfully, ID: %s, name: %s", library.ID, library.Name)
	return library, nil
}

func (s *libraryService) ModelRoutingDB() *gorm.DB { return s.db }
