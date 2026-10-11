package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	apprepo "reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// apiKeyLastUsedMinInterval bounds how often we persist last_used_at per key.
// The UI only needs minute-level freshness; throttling avoids a DB write on
// every authenticated request under high QPS.
const apiKeyLastUsedMinInterval = time.Minute

type apiKeyService struct {
	repo          interfaces.APIKeyRepository
	lastUsedTouch sync.Map // key ID (uint64) -> time.Time of last persisted touch
}

func NewAPIKeyService(repo interfaces.APIKeyRepository) interfaces.APIKeyService {
	return &apiKeyService{repo: repo}
}

func (s *apiKeyService) CreateAPIKey(
	ctx context.Context, req interfaces.APIKeyCreateRequest,
) (*interfaces.APIKeyCreateResult, error) {
	if req.SpaceID != types.SingleSpaceID {
		return nil, errors.New("only the personal space is available")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	token, err := generateAPIKeyToken()
	if err != nil {
		return nil, err
	}
	expiresAt := req.ExpiresAt
	if expiresAt != nil {
		utc := expiresAt.UTC()
		expiresAt = &utc
	}
	spaceID := &req.SpaceID
	key := &types.APIKey{
		SpaceID:   spaceID,
		Name:      name,
		KeyHash:   hashAPIKey(token),
		APIKey:    token,
		ExpiresAt: expiresAt,
	}
	if err := s.repo.CreateAPIKey(ctx, key); err != nil {
		return nil, err
	}
	return &interfaces.APIKeyCreateResult{APIKey: key, Token: token}, nil
}

func (s *apiKeyService) AuthenticateAPIKey(ctx context.Context, token string) (*types.APIKey, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, apprepo.ErrAPIKeyNotFound
	}
	key, err := s.repo.GetAPIKeyByHash(ctx, hashAPIKey(token))
	if err != nil && !errors.Is(err, apprepo.ErrAPIKeyNotFound) {
		return nil, apperrors.NewServiceUnavailableError("Authentication is temporarily unavailable")
	}
	if err != nil {
		return nil, err
	}
	if key.SpaceIDValue() != types.SingleSpaceID || key.RevokedAt != nil {
		return nil, apprepo.ErrAPIKeyNotFound
	}
	if key.ExpiresAt != nil && time.Now().UTC().After(key.ExpiresAt.UTC()) {
		return nil, apprepo.ErrAPIKeyNotFound
	}
	s.touchAPIKeyLastUsedAsync(key.ID)
	return key, nil
}

// touchAPIKeyLastUsedAsync persists last_used_at at most once per key per
// apiKeyLastUsedMinInterval. The write runs in a detached goroutine so auth
// latency is not tied to an UPDATE on the hot path.
func (s *apiKeyService) touchAPIKeyLastUsedAsync(keyID uint64) {
	now := time.Now().UTC()
	if v, ok := s.lastUsedTouch.Load(keyID); ok {
		if now.Sub(v.(time.Time)) < apiKeyLastUsedMinInterval {
			return
		}
	}
	s.lastUsedTouch.Store(keyID, now)
	go func(id uint64, at time.Time) {
		if err := s.repo.UpdateAPIKeyLastUsed(context.Background(), id, at); err != nil {
			logger.Warnf(context.Background(),
				"failed to update space api key last_used_at (id=%d): %v", id, err)
			s.lastUsedTouch.Delete(id)
		}
	}(keyID, now)
}

func (s *apiKeyService) ListAPIKeys(ctx context.Context, spaceID uint64) ([]*types.APIKey, error) {
	return s.repo.ListAPIKeys(ctx, spaceID)
}

// UpdateAPIKey 按创建接口的相同语义更新租户 API Key 配置。
// API Key 与登录用户使用相同权限，只更新名称和有效期。
func (s *apiKeyService) UpdateAPIKey(
	ctx context.Context, req interfaces.APIKeyUpdateRequest,
) (*types.APIKey, error) {
	if req.SpaceID == 0 {
		return nil, errors.New("space_id is required")
	}
	if req.APIKeyID == 0 {
		return nil, errors.New("api_key_id is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	expiresAt := req.ExpiresAt
	if expiresAt != nil {
		utc := expiresAt.UTC()
		expiresAt = &utc
	}
	key := &types.APIKey{
		Name:      name,
		ExpiresAt: expiresAt,
	}
	return s.repo.UpdateAPIKey(ctx, req.SpaceID, req.APIKeyID, key)
}

func (s *apiKeyService) RevokeAPIKey(ctx context.Context, spaceID uint64, id uint64) error {
	return s.repo.RevokeAPIKey(ctx, spaceID, id)
}

func (s *apiKeyService) BackfillMissingKeyHashes(ctx context.Context) (int, error) {
	has, err := s.repo.HasKeysWithPlaceholderHash(ctx)
	if err != nil {
		return 0, err
	}
	if !has {
		return 0, nil
	}
	keys, err := s.repo.ListKeysWithPlaceholderHash(ctx)
	if err != nil {
		return 0, err
	}
	backfilled := 0
	for _, key := range keys {
		if key == nil || strings.TrimSpace(key.APIKey) == "" {
			continue
		}
		hash := hashAPIKey(key.APIKey)
		if key.KeyHash == hash {
			continue
		}
		if err := s.repo.UpdateAPIKeyHash(ctx, key.ID, hash); err != nil {
			return backfilled, err
		}
		backfilled++
	}
	return backfilled, nil
}

func generateAPIKeyToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "sk-" + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func hashAPIKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
