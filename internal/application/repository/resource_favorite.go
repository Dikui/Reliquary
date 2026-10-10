package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type resourceFavoriteRepository struct {
	db *gorm.DB
}

// NewResourceFavoriteRepository constructs the GORM-backed implementation.
func NewResourceFavoriteRepository(db *gorm.DB) interfaces.ResourceFavoriteRepository {
	return &resourceFavoriteRepository{db: db}
}

func (r *resourceFavoriteRepository) List(
	ctx context.Context, tenantID uint64, resourceType string,
) ([]*types.ResourceFavorite, error) {
	var list []*types.ResourceFavorite
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND resource_type = ?", tenantID, resourceType).
		Order("created_at DESC").
		Find(&list).Error
	return list, err
}

// Add upserts the favorite. We rely on the composite primary key + GORM's
// FirstOrCreate to make the insertion idempotent — concurrent double-clicks
// for the same resource collapse into one row with no error path needed.
// `created` reflects whether the row was newly inserted vs already present.
func (r *resourceFavoriteRepository) Add(
	ctx context.Context, tenantID uint64, resourceType, resourceID string,
) (bool, error) {
	rec := &types.ResourceFavorite{
		TenantID:     tenantID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
	}
	res := r.db.WithContext(ctx).Where(rec).FirstOrCreate(rec)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *resourceFavoriteRepository) Remove(
	ctx context.Context, tenantID uint64, resourceType, resourceID string,
) (bool, error) {
	res := r.db.WithContext(ctx).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ?",
			tenantID, resourceType, resourceID).
		Delete(&types.ResourceFavorite{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *resourceFavoriteRepository) IsFavorite(
	ctx context.Context, tenantID uint64, resourceType, resourceID string,
) (bool, error) {
	var rec types.ResourceFavorite
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ?",
			tenantID, resourceType, resourceID).
		First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *resourceFavoriteRepository) IsConcreteTag(ctx context.Context, tenantID uint64, tagID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&types.KnowledgeTag{}).
		Where("tenant_id = ? AND id = ? AND kind IN ?", tenantID, tagID, []string{types.TagKindTopic, types.TagKindObject, types.TagKindEvent}).
		Count(&count).Error
	return count == 1, err
}
