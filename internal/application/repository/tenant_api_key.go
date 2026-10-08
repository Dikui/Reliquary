package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var ErrTenantAPIKeyNotFound = errors.New("tenant api key not found")

type tenantAPIKeyRepository struct {
	db *gorm.DB
}

func NewTenantAPIKeyRepository(db *gorm.DB) interfaces.TenantAPIKeyRepository {
	return &tenantAPIKeyRepository{db: db}
}

func (r *tenantAPIKeyRepository) CreateAPIKey(ctx context.Context, key *types.TenantAPIKey) error {
	return r.db.WithContext(ctx).Create(key).Error
}

func (r *tenantAPIKeyRepository) GetAPIKeyByHash(ctx context.Context, hash string) (*types.TenantAPIKey, error) {
	var key types.TenantAPIKey
	err := r.db.WithContext(ctx).Session(&gorm.Session{SkipHooks: true}).
		Where("key_hash = ?", hash).
		First(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTenantAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *tenantAPIKeyRepository) ListAPIKeys(ctx context.Context, tenantID uint64) ([]*types.TenantAPIKey, error) {
	var keys []*types.TenantAPIKey
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND revoked_at IS NULL", tenantID).
		Order("created_at DESC").
		Find(&keys).Error
	return keys, err
}

func (r *tenantAPIKeyRepository) UpdateAPIKey(
	ctx context.Context, tenantID uint64, id uint64, update *types.TenantAPIKey,
) (*types.TenantAPIKey, error) {
	res := r.db.WithContext(ctx).
		Model(&types.TenantAPIKey{}).
		Where("id = ? AND tenant_id = ? AND revoked_at IS NULL",
			id, tenantID).
		Updates(map[string]any{
			"name":       update.Name,
			"expires_at": update.ExpiresAt,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrTenantAPIKeyNotFound
	}

	var updatedKey types.TenantAPIKey
	if err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ? AND revoked_at IS NULL", id, tenantID).
		First(&updatedKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTenantAPIKeyNotFound
		}
		return nil, err
	}
	return &updatedKey, nil
}

func (r *tenantAPIKeyRepository) RevokeAPIKey(ctx context.Context, tenantID uint64, id uint64) error {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).
		Model(&types.TenantAPIKey{}).
		Where("id = ? AND tenant_id = ? AND revoked_at IS NULL", id, tenantID).
		Update("revoked_at", &now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTenantAPIKeyNotFound
	}
	return nil
}

func (r *tenantAPIKeyRepository) UpdateAPIKeyHash(ctx context.Context, id uint64, hash string) error {
	return r.db.WithContext(ctx).
		Model(&types.TenantAPIKey{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("key_hash", hash).Error
}

// placeholderKeyHashPrefix identifies legacy keys whose hash is a
// placeholder ('migrated-tenant-' || id). Rows still carrying it do
// not yet contain a real SHA-256 hash of the API key.
const placeholderKeyHashPrefix = "migrated-tenant-"

func (r *tenantAPIKeyRepository) HasKeysWithPlaceholderHash(ctx context.Context) (bool, error) {
	var id uint64
	err := r.db.WithContext(ctx).Session(&gorm.Session{SkipHooks: true}).
		Model(&types.TenantAPIKey{}).
		Select("id").
		Where("key_hash LIKE ? AND revoked_at IS NULL", placeholderKeyHashPrefix+"%").
		Limit(1).
		Scan(&id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return id != 0, nil
}

func (r *tenantAPIKeyRepository) ListKeysWithPlaceholderHash(
	ctx context.Context,
) ([]*types.TenantAPIKey, error) {
	var keys []*types.TenantAPIKey
	// AfterFind decrypts api_key, so callers get the plaintext token needed
	// to compute the real hash.
	err := r.db.WithContext(ctx).
		Where("key_hash LIKE ? AND revoked_at IS NULL", placeholderKeyHashPrefix+"%").
		Find(&keys).Error
	return keys, err
}

func (r *tenantAPIKeyRepository) UpdateAPIKeyLastUsed(ctx context.Context, id uint64, at time.Time) error {
	return r.db.WithContext(ctx).
		Model(&types.TenantAPIKey{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("last_used_at", &at).Error
}
