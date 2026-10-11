package service

import (
	"context"
	"testing"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type retiredSettingRepository struct {
	interfaces.SystemSettingRepository
	rows       []*types.SystemSetting
	getCalls   int
	writeCalls int
}

func (r *retiredSettingRepository) List(context.Context) ([]*types.SystemSetting, error) {
	return r.rows, nil
}

func (r *retiredSettingRepository) Get(context.Context, string) (*types.SystemSetting, error) {
	r.getCalls++
	return nil, nil
}

func (r *retiredSettingRepository) Upsert(context.Context, *types.SystemSetting) error {
	r.writeCalls++
	return nil
}

func (r *retiredSettingRepository) Delete(context.Context, string) (bool, error) {
	r.writeCalls++
	return false, nil
}

func TestSystemSettingsRetireTenantCreationOptions(t *testing.T) {
	retiredKeys := []string{"tenant.auto_create_api_key", "tenant.default_storage_quota_gb"}
	for _, persisted := range []bool{false, true} {
		name := "without_stored_values"
		if persisted {
			name = "with_stored_values"
		}
		t.Run(name, func(t *testing.T) {
			repo := &retiredSettingRepository{}
			if persisted {
				repo.rows = []*types.SystemSetting{
					{Key: retiredKeys[0], Value: types.JSON(`true`), ValueType: "bool", Category: "tenant"},
					{Key: retiredKeys[1], Value: types.JSON(`100`), ValueType: "int", Category: "tenant"},
					{Key: "queue.core_concurrency", Value: types.JSON(`4`), ValueType: "int", Category: "worker"},
				}
			}
			svc := &systemSettingService{repo: repo}
			rows, err := svc.List(context.Background())
			if err != nil {
				t.Fatalf("list settings: %v", err)
			}
			activeSettingPresent := false
			for _, row := range rows {
				for _, key := range retiredKeys {
					if row.Key == key {
						t.Fatalf("retired key %q remains visible in settings", key)
					}
				}
				activeSettingPresent = activeSettingPresent || row.Key == "queue.core_concurrency"
			}
			if !activeSettingPresent {
				t.Fatal("active worker configuration missing")
			}
			for _, key := range retiredKeys {
				if _, err := svc.Get(context.Background(), key); err == nil {
					t.Fatalf("get accepted retired key %q", key)
				}
				if _, err := svc.Update(context.Background(), key, true); err == nil {
					t.Fatalf("update accepted retired key %q", key)
				}
				if err := svc.Reset(context.Background(), key); err == nil {
					t.Fatalf("reset accepted retired key %q", key)
				}
			}
			if repo.getCalls != 0 || repo.writeCalls != 0 {
				t.Fatalf("retired setting operations accessed storage: reads=%d writes=%d", repo.getCalls, repo.writeCalls)
			}
		})
	}
}
