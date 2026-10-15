package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	if err := r.db.WithContext(ctx).Preload("ContentTypes", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).Preload("RelationTypes", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).Where("id = ? AND space_id = ?", id, types.SingleSpaceID).First(&library).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLibraryNotFound
		}
		return nil, err
	}
	if err := r.hydrate(ctx, &library); err != nil {
		return nil, err
	}
	return &library, nil
}

// GetLibraryByIDAndSpace gets a library by id only if it belongs to the given space (enforces space isolation)
func (r *libraryRepository) GetLibraryByIDAndSpace(ctx context.Context, id string, spaceID uint64) (*types.Library, error) {
	var library types.Library
	if err := r.db.WithContext(ctx).Preload("ContentTypes", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).Preload("RelationTypes", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).Where("id = ? AND space_id = ?", id, spaceID).First(&library).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLibraryNotFound
		}
		return nil, err
	}
	if err := r.hydrate(ctx, &library); err != nil {
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
	if err := r.db.WithContext(ctx).Preload("ContentTypes", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).Preload("RelationTypes", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).Where("id IN ? AND space_id = ?", ids, types.SingleSpaceID).Find(&libraries).Error; err != nil {
		return nil, err
	}
	for _, library := range libraries {
		if err := r.hydrate(ctx, library); err != nil {
			return nil, err
		}
	}
	return libraries, nil
}

// GetSystemLibraryByRole returns the active library for one space.
func (r *libraryRepository) GetSystemLibraryByRole(ctx context.Context, spaceID uint64, roles ...string) (*types.Library, error) {
	role := "institutional"
	if len(roles) > 0 {
		role = roles[0]
	}
	var library types.Library
	if err := r.db.WithContext(ctx).Preload("ContentTypes", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).Preload("RelationTypes", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).Where("space_id = ? AND system_role = ?", spaceID, role).First(&library).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLibraryNotFound
		}
		return nil, err
	}
	if err := r.hydrate(ctx, &library); err != nil {
		return nil, err
	}
	return &library, nil
}

// UpdateLibrary updates a library
func (r *libraryRepository) UpdateLibrary(ctx context.Context, library *types.Library) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous types.Library
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&previous, "id=? AND space_id=?", library.ID, library.SpaceID).Error; err != nil {
			return err
		}
		if previous.Template != library.Template || previous.Directory != library.Directory || previous.SystemRole != library.SystemRole {
			return fmt.Errorf("library identity is immutable")
		}
		if err := tx.Omit("ContentTypes", "RelationTypes", "EmbeddingModelID").Save(library).Error; err != nil {
			return err
		}
		if library.ContentTypes != nil {
			if err := tx.Where("library_id=?", library.ID).Delete(&types.LibraryContentType{}).Error; err != nil {
				return err
			}
			for i := range library.ContentTypes {
				library.ContentTypes[i].LibraryID = library.ID
			}
			if len(library.ContentTypes) > 0 {
				if err := tx.Create(&library.ContentTypes).Error; err != nil {
					return err
				}
			}
		}
		if library.RelationTypes != nil {
			// Preserve referenced type identities and durable merge aliases.
			names := []string{}
			for i := range library.RelationTypes {
				row := &library.RelationTypes[i]
				row.LibraryID = library.ID
				names = append(names, row.Name)
				var merged int64
				if err := tx.Model(&types.RelationTypeMerge{}).Where("library_id=? AND from_type=?", library.ID, row.Name).Count(&merged).Error; err != nil {
					return err
				}
				if merged > 0 {
					row.Enabled = false
				}
				if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "library_id"}, {Name: "name"}}, DoUpdates: clause.AssignmentColumns([]string{"position", "enabled"})}).Create(row).Error; err != nil {
					return err
				}
			}
			var old []types.LibraryRelationType
			q := tx.Where("library_id=?", library.ID)
			if len(names) > 0 {
				q = q.Where("name NOT IN ?", names)
			}
			if err := q.Find(&old).Error; err != nil {
				return err
			}
			for _, row := range old {
				var used bool
				if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM relations WHERE library_id=? AND relation_type=? UNION ALL SELECT 1 FROM relation_suppressions WHERE library_id=? AND relation_type=? UNION ALL SELECT 1 FROM relation_type_merges WHERE library_id=? AND (from_type=? OR to_type=?))`, library.ID, row.Name, library.ID, row.Name, library.ID, row.Name, row.Name).Scan(&used).Error; err != nil {
					return err
				}
				if used {
					if err := tx.Model(&row).Update("enabled", false).Error; err != nil {
						return err
					}
				} else {
					if err := tx.Delete(&row).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
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
			"id", "name", "space_id", "embedding_model_id", "summary_model_id",
			"model_tiers",
		).
		Where("space_id = ?", spaceID)
	query = scopeLibrariesByModelID(query, modelID)
	if err := query.Order("name ASC, id ASC").Limit(types.ModelUsageListLimit).Find(&rows).Error; err != nil {
		return nil, err
	}

	usages := make([]types.ModelUsageResource, 0, len(rows))
	for _, row := range rows {
		if err := r.hydrate(ctx, row); err != nil {
			return nil, err
		}
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

// EnsureSystemLibraries creates each missing role without overwriting user settings.
func EnsureSystemLibraries(ctx context.Context, db *gorm.DB, spaceID uint64, creatorID string) (*types.Library, error) {
	if spaceID != types.SingleSpaceID {
		return nil, errors.New("invalid personal space")
	}
	var first *types.Library
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(72651002)).Error; err != nil {
			return err
		}
		repo := NewLibraryRepository(tx)
		for _, library := range types.SystemLibraryDefaults() {
			existing, err := repo.GetSystemLibraryByRole(ctx, spaceID, library.SystemRole)
			if err == nil {
				if existing.CreatorID == "" && creatorID != "" {
					if err := tx.Model(existing).Update("creator_id", creatorID).Error; err != nil {
						return err
					}
					existing.CreatorID = creatorID
				}
				if library.SystemRole == "institutional" {
					first = existing
				}
				continue
			}
			if !errors.Is(err, ErrLibraryNotFound) {
				return err
			}
			library.ID = uuid.NewString()
			library.SpaceID = spaceID
			library.CreatorID = creatorID
			library.CreatedAt = time.Now().UTC()
			library.UpdatedAt = library.CreatedAt
			if err := repo.CreateLibrary(ctx, library); err != nil {
				return err
			}
			if library.SystemRole == "institutional" {
				first, err = repo.GetLibraryByID(ctx, library.ID)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	return first, err
}

func (r *libraryRepository) ListLibraries(ctx context.Context, spaceID uint64) ([]*types.Library, error) {
	libraries := []*types.Library{}
	err := r.db.WithContext(ctx).Preload("ContentTypes", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).Preload("RelationTypes", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).Where("space_id=?", spaceID).Order("created_at,id").Find(&libraries).Error
	for _, library := range libraries {
		if err := r.hydrate(ctx, library); err != nil {
			return nil, err
		}
	}
	return libraries, err
}

// EmbeddingModelID in library payloads is a compatibility projection. Space is authoritative.
func (r *libraryRepository) hydrate(ctx context.Context, library *types.Library) error {
	var space types.Space
	if err := r.db.WithContext(ctx).Select("id", "embedding_model_id").Where("id=?", library.SpaceID).Find(&space).Error; err != nil {
		return err
	}
	if space.ID != 0 {
		library.EmbeddingModelID = space.EmbeddingModelID
		library.ModelTiers.Embedding = space.EmbeddingModelID
	}
	if library.ContentTypes == nil {
		library.ContentTypes = []types.LibraryContentType{}
	}
	if library.RelationTypes == nil {
		library.RelationTypes = []types.LibraryRelationType{}
	}
	library.EnsureDefaults()
	return nil
}
