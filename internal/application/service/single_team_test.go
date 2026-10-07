package service

import (
	"context"
	"testing"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestEnsureSingleTeamSpaceIsIdempotent(t *testing.T) {
	db := pgtest.Open(t)
	for i := 0; i < 2; i++ {
		if err := EnsureSingleTeamSpace(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	var tenants []types.Tenant
	if err := db.Find(&tenants).Error; err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 1 || tenants[0].ID != types.SingleTenantID || tenants[0].DefaultStorageBackendID == nil {
		t.Fatalf("team bootstrap=%+v", tenants)
	}
	var count int64
	if err := db.Model(&types.StorageBackend{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("default storage count=%d err=%v", count, err)
	}
}
