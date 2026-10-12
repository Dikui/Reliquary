package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var ErrAPIKeyNotFound = errors.New("space api key not found")

type apiKeyRepository struct {
	db *gorm.DB
}

func NewAPIKeyRepository(db *gorm.DB) interfaces.APIKeyRepository {
	return &apiKeyRepository{db: db}
}

func (r *apiKeyRepository) CreateAPIKey(ctx context.Context, key *types.APIKey) error {
	return r.db.WithContext(ctx).Create(key).Error
}

func (r *apiKeyRepository) GetAPIKeyByHash(ctx context.Context, hash string) (*types.APIKey, error) {
	var key types.APIKey
	err := r.db.WithContext(ctx).Session(&gorm.Session{SkipHooks: true}).
		Where("key_hash = ?", hash).
		First(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *apiKeyRepository) ListAPIKeys(ctx context.Context, spaceID uint64) ([]*types.APIKey, error) {
	var keys []*types.APIKey
	err := r.db.WithContext(ctx).
		Where("space_id = ? AND revoked_at IS NULL", spaceID).
		Order("created_at DESC").
		Find(&keys).Error
	return keys, err
}

func (r *apiKeyRepository) UpdateAPIKey(
	ctx context.Context, spaceID uint64, id uint64, update *types.APIKey,
) (*types.APIKey, error) {
	res := r.db.WithContext(ctx).
		Model(&types.APIKey{}).
		Where("id = ? AND space_id = ? AND revoked_at IS NULL",
			id, spaceID).
		Updates(map[string]any{
			"name":       update.Name,
			"expires_at": update.ExpiresAt,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrAPIKeyNotFound
	}

	var updatedKey types.APIKey
	if err := r.db.WithContext(ctx).
		Where("id = ? AND space_id = ? AND revoked_at IS NULL", id, spaceID).
		First(&updatedKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAPIKeyNotFound
		}
		return nil, err
	}
	return &updatedKey, nil
}

func (r *apiKeyRepository) RevokeAPIKey(ctx context.Context, spaceID uint64, id uint64) error {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).
		Model(&types.APIKey{}).
		Where("id = ? AND space_id = ? AND revoked_at IS NULL", id, spaceID).
		Update("revoked_at", &now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (r *apiKeyRepository) UpdateAPIKeyHash(ctx context.Context, id uint64, hash string) error {
	return r.db.WithContext(ctx).
		Model(&types.APIKey{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("key_hash", hash).Error
}

// placeholderKeyHashPrefix identifies legacy keys whose hash is a
// placeholder ('migrated-space-' || id). Rows still carrying it do
// not yet contain a real SHA-256 hash of the API key.
const placeholderKeyHashPrefix = "migrated-space-"

func (r *apiKeyRepository) HasKeysWithPlaceholderHash(ctx context.Context) (bool, error) {
	var id uint64
	err := r.db.WithContext(ctx).Session(&gorm.Session{SkipHooks: true}).
		Model(&types.APIKey{}).
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

func (r *apiKeyRepository) ListKeysWithPlaceholderHash(
	ctx context.Context,
) ([]*types.APIKey, error) {
	var keys []*types.APIKey
	// AfterFind decrypts api_key, so callers get the plaintext token needed
	// to compute the real hash.
	err := r.db.WithContext(ctx).
		Where("key_hash LIKE ? AND revoked_at IS NULL", placeholderKeyHashPrefix+"%").
		Find(&keys).Error
	return keys, err
}

func (r *apiKeyRepository) UpdateAPIKeyLastUsed(ctx context.Context, id uint64, at time.Time) error {
	return r.db.WithContext(ctx).
		Model(&types.APIKey{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("last_used_at", &at).Error
}
