package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// vectorStoreRepository implements the VectorStoreRepository interface
type vectorStoreRepository struct {
	db *gorm.DB
}

// NewVectorStoreRepository creates a new vector store repository
func NewVectorStoreRepository(db *gorm.DB) interfaces.VectorStoreRepository {
	return &vectorStoreRepository{db: db}
}

// GetByID retrieves a vector store by ID within a tenant scope.
// Returns (nil, nil) when the record is not found (not an error).
func (r *vectorStoreRepository) GetByID(ctx context.Context, tenantID uint64, id string) (*types.VectorStore, error) {
	var store types.VectorStore
	if err := r.db.WithContext(ctx).Where(
		"id = ? AND tenant_id = ?", id, tenantID,
	).First(&store).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &store, nil
}

// List lists all vector stores for a tenant (newest first)
func (r *vectorStoreRepository) List(ctx context.Context, tenantID uint64) ([]*types.VectorStore, error) {
	var stores []*types.VectorStore
	if err := r.db.WithContext(ctx).Where(
		"tenant_id = ?", tenantID,
	).Order("created_at DESC").Find(&stores).Error; err != nil {
		return nil, err
	}
	return stores, nil
}
