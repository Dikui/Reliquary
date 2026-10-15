package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var (
	ErrSpaceNotFound   = errors.New("space not found")
	ErrSpaceHasLibrary = errors.New("space has associated libraries")
)

// spaceRepository implements space repository interface
type spaceRepository struct {
	db *gorm.DB
}

// NewSpaceRepository creates a new space repository
func NewSpaceRepository(db *gorm.DB) interfaces.SpaceRepository {
	return &spaceRepository{db: db}
}

// CreateSpace creates space
func (r *spaceRepository) CreateSpace(ctx context.Context, space *types.Space) error {
	return r.db.WithContext(ctx).Create(space).Error
}

// GetSpaceByID gets space by ID
func (r *spaceRepository) GetSpaceByID(ctx context.Context, id uint64) (*types.Space, error) {
	var space types.Space
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&space).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSpaceNotFound
		}
		return nil, err
	}
	return &space, nil
}

// GetSpacesByIDs batches GetSpaceByID with a single IN-list query.
// Returns a map keyed by space ID; missing rows are simply absent from
// the map (no error). An empty input slice short-circuits to an empty map
// without hitting the database.
func (r *spaceRepository) GetSpacesByIDs(ctx context.Context, ids []uint64) (map[uint64]*types.Space, error) {
	if len(ids) == 0 {
		return map[uint64]*types.Space{}, nil
	}
	var spaces []*types.Space
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&spaces).Error; err != nil {
		return nil, err
	}
	out := make(map[uint64]*types.Space, len(spaces))
	for _, t := range spaces {
		if t != nil {
			out[t.ID] = t
		}
	}
	return out, nil
}

// ListSpaces lists all spaces
func (r *spaceRepository) ListSpaces(ctx context.Context) ([]*types.Space, error) {
	var spaces []*types.Space
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&spaces).Error; err != nil {
		return nil, err
	}
	return spaces, nil
}

// UpdateSpace updates space.
func (r *spaceRepository) UpdateSpace(ctx context.Context, space *types.Space) error {
	return r.db.WithContext(ctx).Model(&types.Space{}).Where("id = ?", space.ID).Omit("EmbeddingModelID").Updates(space).Error
}

// DeleteSpace soft-deletes the workspace.
func (r *spaceRepository) DeleteSpace(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&types.Space{}).Error
}

func (r *spaceRepository) AdjustStorageUsed(ctx context.Context, spaceID uint64, delta int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var space types.Space
		// 使用悲观锁确保并发安全
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&space, spaceID).Error; err != nil {
			return err
		}

		space.StorageUsed += delta
		// 保存更新并验证业务规则
		if space.StorageUsed < 0 {
			logger.Errorf(ctx, "space storage used is negative %d: %d", space.ID, space.StorageUsed)
			space.StorageUsed = 0
		}

		return tx.Save(&space).Error
	})
}
