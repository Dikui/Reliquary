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

type fakeTenantAPIKeyRepo struct {
	byHash              map[string]*types.TenantAPIKey
	nextID              uint64
	lastUsedUpdateCount int
}

func TestTenantAPIKeyServiceCreateAPIKeyUsesSKPrefix(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTenantAPIKeyRepo()
	svc := NewTenantAPIKeyService(repo)

	result, err := svc.CreateAPIKey(ctx, interfaces.TenantAPIKeyCreateRequest{
		TenantID: 1,
		Name:     "integration",
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

func newFakeTenantAPIKeyRepo() *fakeTenantAPIKeyRepo {
	return &fakeTenantAPIKeyRepo{byHash: map[string]*types.TenantAPIKey{}, nextID: 1}
}

func (r *fakeTenantAPIKeyRepo) CreateAPIKey(_ context.Context, key *types.TenantAPIKey) error {
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

func (r *fakeTenantAPIKeyRepo) GetAPIKeyByHash(_ context.Context, hash string) (*types.TenantAPIKey, error) {
	key, ok := r.byHash[hash]
	if !ok {
		return nil, apprepo.ErrTenantAPIKeyNotFound
	}
	cp := *key
	return &cp, nil
}

func (r *fakeTenantAPIKeyRepo) ListAPIKeys(_ context.Context, tenantID uint64) ([]*types.TenantAPIKey, error) {
	out := []*types.TenantAPIKey{}
	for _, key := range r.byHash {
		if key.TenantIDValue() == tenantID && key.RevokedAt == nil {
			cp := *key
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeTenantAPIKeyRepo) RevokeAPIKey(_ context.Context, tenantID uint64, id uint64) error {
	now := time.Now()
	for _, key := range r.byHash {
		if key.ID == id && key.TenantIDValue() == tenantID && key.RevokedAt == nil {
			key.RevokedAt = &now
			return nil
		}
	}
	return apprepo.ErrTenantAPIKeyNotFound
}

// UpdateAPIKey 模拟仓储的租户边界并覆盖 API Key 的可配置属性。
// 传入租户 ID、Key ID 和新配置，返回更新后的 Key；跨租户或已撤销目标返回未找到。
func (r *fakeTenantAPIKeyRepo) UpdateAPIKey(
	_ context.Context, tenantID uint64, id uint64, update *types.TenantAPIKey,
) (*types.TenantAPIKey, error) {
	for _, key := range r.byHash {
		if key.ID == id && key.TenantIDValue() == tenantID && key.RevokedAt == nil {
			key.Name = update.Name
			key.ExpiresAt = update.ExpiresAt
			cp := *key
			return &cp, nil
		}
	}
	return nil, apprepo.ErrTenantAPIKeyNotFound
}

// TestTenantAPIKeyServiceUpdateNormalizesConfiguration 验证通用更新的输入规范化。
// 输入包含重复 ID/能力和 UTC+8 到期时间，输出应去重、清理名称并统一为 UTC。
func TestTenantAPIKeyServiceUpdateNormalizesConfiguration(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTenantAPIKeyRepo()
	svc := NewTenantAPIKeyService(repo)
	created, err := svc.CreateAPIKey(ctx, interfaces.TenantAPIKeyCreateRequest{
		TenantID: 1, Name: "scoped",
	})
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	expiresAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	updated, err := svc.UpdateAPIKey(ctx, interfaces.TenantAPIKeyUpdateRequest{
		TenantID: 1, APIKeyID: created.APIKey.ID,
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

func (r *fakeTenantAPIKeyRepo) UpdateAPIKeyHash(_ context.Context, id uint64, hash string) error {
	for oldHash, key := range r.byHash {
		if key.ID == id && key.RevokedAt == nil {
			delete(r.byHash, oldHash)
			key.KeyHash = hash
			r.byHash[hash] = key
			return nil
		}
	}
	return apprepo.ErrTenantAPIKeyNotFound
}

func (r *fakeTenantAPIKeyRepo) HasKeysWithPlaceholderHash(_ context.Context) (bool, error) {
	for _, key := range r.byHash {
		if key.RevokedAt == nil && strings.HasPrefix(key.KeyHash, "migrated-tenant-") {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeTenantAPIKeyRepo) ListKeysWithPlaceholderHash(_ context.Context) ([]*types.TenantAPIKey, error) {
	out := []*types.TenantAPIKey{}
	for _, key := range r.byHash {
		if key.RevokedAt == nil && strings.HasPrefix(key.KeyHash, "migrated-tenant-") {
			cp := *key
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeTenantAPIKeyRepo) UpdateAPIKeyLastUsed(_ context.Context, id uint64, at time.Time) error {
	r.lastUsedUpdateCount++
	for _, key := range r.byHash {
		if key.ID == id && key.RevokedAt == nil {
			key.LastUsedAt = &at
		}
	}
	return nil
}

func TestTenantAPIKeyServiceBackfillMissingKeyHashes(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTenantAPIKeyRepo()
	svc := NewTenantAPIKeyService(repo)

	token := "sk-legacy-token-value"
	legacy := &types.TenantAPIKey{
		TenantID: uint64Pointer(types.SingleTenantID),
		Name:     "legacy",
		KeyHash:  "migrated-tenant-1",
		APIKey:   token,
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

func TestTenantAPIKeyServiceRevokeAPIKey(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTenantAPIKeyRepo()
	svc := NewTenantAPIKeyService(repo)

	created, err := svc.CreateAPIKey(ctx, interfaces.TenantAPIKeyCreateRequest{
		TenantID: 1,
		Name:     "integration",
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

func TestTenantAPIKeyServiceAuthenticateThrottlesLastUsedUpdates(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTenantAPIKeyRepo()
	svc := NewTenantAPIKeyService(repo)

	created, err := svc.CreateAPIKey(ctx, interfaces.TenantAPIKeyCreateRequest{
		TenantID: 1,
		Name:     "integration",
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

func TestTenantAPIKeyServiceAuthenticateRejectsExpiredKey(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTenantAPIKeyRepo()
	svc := NewTenantAPIKeyService(repo)

	expired := time.Now().UTC().Add(-time.Minute)
	created, err := svc.CreateAPIKey(ctx, interfaces.TenantAPIKeyCreateRequest{
		TenantID:  1,
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
