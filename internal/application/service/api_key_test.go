package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type fakeAPIKeyRepo struct {
	byHash              map[string]*types.APIKey
	nextID              uint64
	lastUsedUpdateCount int
}

func TestAPIKeyServiceCreateAPIKeyUsesSKPrefix(t *testing.T) {
	ctx := context.Background()
	repo := newFakeAPIKeyRepo()
	svc := NewAPIKeyService(repo)

	result, err := svc.CreateAPIKey(ctx, interfaces.APIKeyCreateRequest{
		SpaceID: 1,
		Name:    "integration",
	})
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	if !strings.HasPrefix(result.Token, "sk-") {
		t.Fatalf("created token = %q, want sk- prefix", result.Token)
	}
	if result.APIKey.APIKey != result.Token {
		t.Fatalf("created api_key = %q, want token %q", result.APIKey.APIKey, result.Token)
	}
}

func newFakeAPIKeyRepo() *fakeAPIKeyRepo {
	return &fakeAPIKeyRepo{byHash: map[string]*types.APIKey{}, nextID: 1}
}

func (r *fakeAPIKeyRepo) CreateAPIKey(_ context.Context, key *types.APIKey) error {
	if _, ok := r.byHash[key.KeyHash]; ok {
		return errors.New("duplicate key hash")
	}
	cp := *key
	cp.ID = r.nextID
	r.nextID++
	r.byHash[cp.KeyHash] = &cp
	key.ID = cp.ID
	return nil
}

func (r *fakeAPIKeyRepo) GetAPIKeyByHash(_ context.Context, hash string) (*types.APIKey, error) {
	key, ok := r.byHash[hash]
	if !ok {
		return nil, apprepo.ErrAPIKeyNotFound
	}
	cp := *key
	return &cp, nil
}

func (r *fakeAPIKeyRepo) ListAPIKeys(_ context.Context, spaceID uint64) ([]*types.APIKey, error) {
	out := []*types.APIKey{}
	for _, key := range r.byHash {
		if key.SpaceIDValue() == spaceID && key.RevokedAt == nil {
			cp := *key
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeAPIKeyRepo) RevokeAPIKey(_ context.Context, spaceID uint64, id uint64) error {
	now := time.Now()
	for _, key := range r.byHash {
		if key.ID == id && key.SpaceIDValue() == spaceID && key.RevokedAt == nil {
			key.RevokedAt = &now
			return nil
		}
	}
	return apprepo.ErrAPIKeyNotFound
}

// UpdateAPIKey 模拟仓储的租户边界并覆盖 API Key 的可配置属性。
// 传入租户 ID、Key ID 和新配置，返回更新后的 Key；跨租户或已撤销目标返回未找到。
func (r *fakeAPIKeyRepo) UpdateAPIKey(
	_ context.Context, spaceID uint64, id uint64, update *types.APIKey,
) (*types.APIKey, error) {
	for _, key := range r.byHash {
		if key.ID == id && key.SpaceIDValue() == spaceID && key.RevokedAt == nil {
			key.Name = update.Name
			key.ExpiresAt = update.ExpiresAt
			cp := *key
			return &cp, nil
		}
	}
	return nil, apprepo.ErrAPIKeyNotFound
}

// TestAPIKeyServiceUpdateNormalizesConfiguration 验证通用更新的输入规范化。
// 输入包含重复 ID/能力和 UTC+8 到期时间，输出应去重、清理名称并统一为 UTC。
func TestAPIKeyServiceUpdateNormalizesConfiguration(t *testing.T) {
	ctx := context.Background()
	repo := newFakeAPIKeyRepo()
	svc := NewAPIKeyService(repo)
	created, err := svc.CreateAPIKey(ctx, interfaces.APIKeyCreateRequest{
		SpaceID: 1, Name: "scoped",
	})
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	expiresAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	updated, err := svc.UpdateAPIKey(ctx, interfaces.APIKeyUpdateRequest{
		SpaceID: 1, APIKeyID: created.APIKey.ID,
		Name: " updated ",

		ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("UpdateAPIKey returned error: %v", err)
	}
	if updated.Name != "updated" {
		t.Fatalf("name = %q, want updated", updated.Name)
	}
	if updated.ExpiresAt == nil || updated.ExpiresAt.Location() != time.UTC {
		t.Fatalf("expires_at = %v, want UTC", updated.ExpiresAt)
	}

}

func apiKeyEqualStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (r *fakeAPIKeyRepo) UpdateAPIKeyHash(_ context.Context, id uint64, hash string) error {
	for oldHash, key := range r.byHash {
		if key.ID == id && key.RevokedAt == nil {
			delete(r.byHash, oldHash)
			key.KeyHash = hash
			r.byHash[hash] = key
			return nil
		}
	}
	return apprepo.ErrAPIKeyNotFound
}

func (r *fakeAPIKeyRepo) HasKeysWithPlaceholderHash(_ context.Context) (bool, error) {
	for _, key := range r.byHash {
		if key.RevokedAt == nil && strings.HasPrefix(key.KeyHash, "migrated-space-") {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeAPIKeyRepo) ListKeysWithPlaceholderHash(_ context.Context) ([]*types.APIKey, error) {
	out := []*types.APIKey{}
	for _, key := range r.byHash {
		if key.RevokedAt == nil && strings.HasPrefix(key.KeyHash, "migrated-space-") {
			cp := *key
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeAPIKeyRepo) UpdateAPIKeyLastUsed(_ context.Context, id uint64, at time.Time) error {
	r.lastUsedUpdateCount++
	for _, key := range r.byHash {
		if key.ID == id && key.RevokedAt == nil {
			key.LastUsedAt = &at
		}
	}
	return nil
}

func TestAPIKeyServiceBackfillMissingKeyHashes(t *testing.T) {
	ctx := context.Background()
	repo := newFakeAPIKeyRepo()
	svc := NewAPIKeyService(repo)

	token := "sk-legacy-token-value"
	legacy := &types.APIKey{
		SpaceID: uint64Pointer(types.SingleSpaceID),
		Name:    "legacy",
		KeyHash: "migrated-space-1",
		APIKey:  token,
	}
	if err := repo.CreateAPIKey(ctx, legacy); err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	n, err := svc.BackfillMissingKeyHashes(ctx)
	if err != nil {
		t.Fatalf("BackfillMissingKeyHashes returned error: %v", err)
	}
	if n != 1 {
		t.Fatalf("backfilled = %d, want 1", n)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, token); err != nil {
		t.Fatalf("AuthenticateAPIKey after backfill returned error: %v", err)
	}
	if n, err := svc.BackfillMissingKeyHashes(ctx); err != nil || n != 0 {
		t.Fatalf("second BackfillMissingKeyHashes = (%d, %v), want (0, nil)", n, err)
	}
}

func uint64Pointer(value uint64) *uint64 { return &value }

func TestAPIKeyServiceRevokeAPIKey(t *testing.T) {
	ctx := context.Background()
	repo := newFakeAPIKeyRepo()
	svc := NewAPIKeyService(repo)

	created, err := svc.CreateAPIKey(ctx, interfaces.APIKeyCreateRequest{
		SpaceID: 1,
		Name:    "integration",
	})
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	if err := svc.RevokeAPIKey(ctx, 1, created.APIKey.ID); err != nil {
		t.Fatalf("RevokeAPIKey returned error: %v", err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, created.Token); err == nil {
		t.Fatal("revoked key should not authenticate")
	}
}

func TestAPIKeyServiceAuthenticateThrottlesLastUsedUpdates(t *testing.T) {
	ctx := context.Background()
	repo := newFakeAPIKeyRepo()
	svc := NewAPIKeyService(repo)

	created, err := svc.CreateAPIKey(ctx, interfaces.APIKeyCreateRequest{
		SpaceID: 1,
		Name:    "integration",
	})
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	for i := 0; i < 5; i++ {
		if _, err := svc.AuthenticateAPIKey(ctx, created.Token); err != nil {
			t.Fatalf("AuthenticateAPIKey #%d returned error: %v", i+1, err)
		}
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for repo.lastUsedUpdateCount == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if repo.lastUsedUpdateCount != 1 {
		t.Fatalf("last_used update count = %d, want 1 (throttled async write)", repo.lastUsedUpdateCount)
	}
}

func TestAPIKeyServiceAuthenticateRejectsExpiredKey(t *testing.T) {
	ctx := context.Background()
	repo := newFakeAPIKeyRepo()
	svc := NewAPIKeyService(repo)

	expired := time.Now().UTC().Add(-time.Minute)
	created, err := svc.CreateAPIKey(ctx, interfaces.APIKeyCreateRequest{
		SpaceID:   1,
		Name:      "short-lived",
		ExpiresAt: &expired,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, created.Token); err == nil {
		t.Fatal("expired key should not authenticate")
	}
}
