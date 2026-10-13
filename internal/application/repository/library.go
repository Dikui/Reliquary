package repository

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var ErrLibraryNotFound = errors.New("library not found")

// libraryRepository implements the LibraryRepository interface
type libraryRepository struct {
	db *gorm.DB
}

// NewLibraryRepository creates a new library repository
func NewLibraryRepository(db *gorm.DB) interfaces.LibraryRepository {
	return &libraryRepository{db: db}
}

// CreateLibrary creates a new library
func (r *libraryRepository) CreateLibrary(ctx context.Context, library *types.Library) error {
	return r.db.WithContext(ctx).Create(library).Error
}

// GetLibraryByID gets a library by id within the personal space
func (r *libraryRepository) GetLibraryByID(ctx context.Context, id string) (*types.Library, error) {
	var library types.Library
	if err := r.db.WithContext(ctx).Where("id = ? AND space_id = ?", id, types.SingleSpaceID).First(&library).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLibraryNotFound
		}
		return nil, err
	}
	return &library, nil
}

// GetLibraryByIDAndSpace gets a library by id only if it belongs to the given space (enforces space isolation)
func (r *libraryRepository) GetLibraryByIDAndSpace(ctx context.Context, id string, spaceID uint64) (*types.Library, error) {
	var library types.Library
	if err := r.db.WithContext(ctx).Where("id = ? AND space_id = ?", id, spaceID).First(&library).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLibraryNotFound
		}
		return nil, err
	}
	return &library, nil
}

// GetLibraryByIDs gets libraries by multiple ids
func (r *libraryRepository) GetLibraryByIDs(ctx context.Context, ids []string) ([]*types.Library, error) {
	if len(ids) == 0 {
		return []*types.Library{}, nil
	}
	var libraries []*types.Library
	if err := r.db.WithContext(ctx).Where("id IN ? AND space_id = ?", ids, types.SingleSpaceID).Find(&libraries).Error; err != nil {
		return nil, err
	}
	return libraries, nil
}

// GetLibraryBySpaceID returns the active library for one space.
func (r *libraryRepository) GetLibraryBySpaceID(ctx context.Context, spaceID uint64) (*types.Library, error) {
	var library types.Library
	if err := r.db.WithContext(ctx).Where("space_id = ?", spaceID).First(&library).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLibraryNotFound
		}
		return nil, err
	}
	return &library, nil
}

// UpdateLibrary updates a library
func (r *libraryRepository) UpdateLibrary(ctx context.Context, library *types.Library) error {
	return r.db.WithContext(ctx).Save(library).Error
}

// CountByModelID counts active libraries that reference modelID in any
// model-binding column (scalar fields or JSON config blobs).
func (r *libraryRepository) CountByModelID(
	ctx context.Context, spaceID uint64, modelID string,
) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).
		Model(&types.Library{}).
		Where("space_id = ?", spaceID)
	query = scopeLibrariesByModelID(query, modelID)
	err := query.Count(&count).Error
	return count, err
}

// ListModelUsages returns active libraries that reference modelID. It
// selects only the columns needed to identify the resource and its bindings;
// GORM's default scope excludes soft-deleted rows.
func (r *libraryRepository) ListModelUsages(
	ctx context.Context, spaceID uint64, modelID string,
) ([]types.ModelUsageResource, error) {
	rows := make([]*types.Library, 0)
	query := r.db.WithContext(ctx).
		Model(&types.Library{}).
		Select(
			"id", "name", "embedding_model_id", "summary_model_id",
			"model_tiers",
		).
		Where("space_id = ?", spaceID)
	query = scopeLibrariesByModelID(query, modelID)
	if err := query.Order("name ASC, id ASC").Limit(types.ModelUsageListLimit).Find(&rows).Error; err != nil {
		return nil, err
	}

	usages := make([]types.ModelUsageResource, 0, len(rows))
	for _, row := range rows {
		bindings := libraryModelUsageBindings(row, modelID)
		if len(bindings) == 0 {
			continue
		}
		usages = append(usages, types.ModelUsageResource{
			ID:       row.ID,
			Name:     row.Name,
			Bindings: bindings,
		})
	}
	return usages, nil
}

// EnsureSpaceLibrary creates the settings resource once. Callers may pass
// an existing registration transaction so the user and library commit together.
func EnsureSpaceLibrary(ctx context.Context, db *gorm.DB, spaceID uint64, creatorID string) (*types.Library, error) {
	if spaceID != types.SingleSpaceID {
		return nil, errors.New("invalid personal space")
	}
	var library *types.Library
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(72651002)).Error; err != nil {
			return err
		}
		repo := NewLibraryRepository(tx)
		existing, err := repo.GetLibraryBySpaceID(ctx, spaceID)
		if err == nil {
			library = existing
			return nil
		}
		if !errors.Is(err, ErrLibraryNotFound) {
			return err
		}
		name := "Library"
		if strings.HasPrefix(strings.ToLower(types.LanguageFromContextOrDefault(ctx)), "zh") {
			name = "库"
		}
		now := time.Now().UTC()
		library = &types.Library{ID: uuid.NewString(), SpaceID: spaceID, CreatorID: creatorID, Name: name, Type: types.LibraryTypeDocument, CreatedAt: now, UpdatedAt: now}
		library.EnsureDefaults()
		return repo.CreateLibrary(ctx, library)
	})
	return library, err
}
