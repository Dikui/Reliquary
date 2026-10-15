package interfaces

import (
	"context"
	"time"

	"reliquary/internal/types"
)

// SpaceService defines the space service interface
type SpaceService interface {
	// CreateSpace creates a space
	CreateSpace(ctx context.Context, space *types.Space) (*types.Space, error)
	// GetSpaceByID gets a space by ID
	GetSpaceByID(ctx context.Context, id uint64) (*types.Space, error)
	// GetSpacesByIDs batches GetSpaceByID for multiple IDs in a single
	// query. Returns a map keyed by space ID for O(1) lookup at the
	// call site; missing spaces are simply absent from the map.
	GetSpacesByIDs(ctx context.Context, ids []uint64) (map[uint64]*types.Space, error)
	// ListSpaces lists all spaces
	ListSpaces(ctx context.Context) ([]*types.Space, error)
	// UpdateSpace updates a space
	UpdateSpace(ctx context.Context, space *types.Space) (*types.Space, error)
	// DeleteSpace deletes a space
	DeleteSpace(ctx context.Context, id uint64) error
}

// SpaceRepository defines the space repository interface
type SpaceRepository interface {
	// CreateSpace creates a space
	CreateSpace(ctx context.Context, space *types.Space) error
	// GetSpaceByID gets a space by ID
	GetSpaceByID(ctx context.Context, id uint64) (*types.Space, error)
	// GetSpacesByIDs batches GetSpaceByID; see SpaceService.GetSpacesByIDs.
	GetSpacesByIDs(ctx context.Context, ids []uint64) (map[uint64]*types.Space, error)
	// ListSpaces lists all spaces
	ListSpaces(ctx context.Context) ([]*types.Space, error)
	// SearchSpaces searches spaces with pagination and filters
	// UpdateSpace updates a space
	UpdateSpace(ctx context.Context, space *types.Space) error
	// DeleteSpace deletes a space
	DeleteSpace(ctx context.Context, id uint64) error
	// AdjustStorageUsed adjusts the storage used for a space
	AdjustStorageUsed(ctx context.Context, spaceID uint64, delta int64) error
}

type APIKeyCreateRequest struct {
	SpaceID   uint64
	Name      string
	ExpiresAt *time.Time
}

type APIKeyCreateResult struct {
	APIKey *types.APIKey
	Token  string
}

// APIKeyUpdateRequest 修改已创建租户 API Key 的可配置属性。
// 配置语义与创建接口一致：FullAccess 为 true 时忽略细粒度能力和库范围。
type APIKeyUpdateRequest struct {
	SpaceID   uint64
	APIKeyID  uint64
	Name      string
	ExpiresAt *time.Time
}

type APIKeyRepository interface {
	CreateAPIKey(ctx context.Context, key *types.APIKey) error
	GetAPIKeyByHash(ctx context.Context, hash string) (*types.APIKey, error)
	ListAPIKeys(ctx context.Context, spaceID uint64) ([]*types.APIKey, error)
	UpdateAPIKey(ctx context.Context, spaceID uint64, id uint64, key *types.APIKey) (*types.APIKey, error)
	RevokeAPIKey(ctx context.Context, spaceID uint64, id uint64) error
	UpdateAPIKeyHash(ctx context.Context, id uint64, hash string) error
	UpdateAPIKeyLastUsed(ctx context.Context, id uint64, at time.Time) error
	// ListKeysWithPlaceholderHash returns keys whose key_hash is still the
	// legacy migration placeholder (not yet authenticated),
	// so the real SHA-256 hash can be computed and backfilled.
	ListKeysWithPlaceholderHash(ctx context.Context) ([]*types.APIKey, error)
	// HasKeysWithPlaceholderHash is a cheap existence probe used at startup
	// to skip loading/decrypting api_key rows once backfill is complete.
	HasKeysWithPlaceholderHash(ctx context.Context) (bool, error)
}

type APIKeyService interface {
	CreateAPIKey(ctx context.Context, req APIKeyCreateRequest) (*APIKeyCreateResult, error)
	AuthenticateAPIKey(ctx context.Context, token string) (*types.APIKey, error)
	ListAPIKeys(ctx context.Context, spaceID uint64) ([]*types.APIKey, error)
	UpdateAPIKey(ctx context.Context, req APIKeyUpdateRequest) (*types.APIKey, error)
	RevokeAPIKey(ctx context.Context, spaceID uint64, id uint64) error
	// BackfillMissingKeyHashes computes and persists the SHA-256 key_hash
	// for legacy keys still carrying the migration placeholder.
	// Returns the number of keys backfilled.
	BackfillMissingKeyHashes(ctx context.Context) (int, error)
}
