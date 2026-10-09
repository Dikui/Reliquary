package service

import (
	"context"
	"testing"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

type fakeIPSettingRepository struct {
	interfaces.SystemSettingRepository
	row *types.SystemSetting
}

func (r *fakeIPSettingRepository) Get(context.Context, string) (*types.SystemSetting, error) {
	return r.row, nil
}

func (r *fakeIPSettingRepository) Upsert(_ context.Context, row *types.SystemSetting) error {
	r.row = row
	return nil
}

func TestSystemSettingFakeIPExtraSurvivesWhitelistEdits(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "203.0.113.99")
	t.Setenv("SSRF_WHITELIST_EXTRA", "198.18.0.0/15")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	repo := &fakeIPSettingRepository{}
	svc := &systemSettingService{repo: repo, cache: make(map[string]*types.SystemSetting)}
	svc.loaded.Store(true)
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "personal-user")
	svc.applySSRFWhitelist(ctx)
	if !utils.IsSSRFWhitelisted("203.0.113.99") || !utils.IsSSRFWhitelisted("198.18.12.34") {
		t.Fatal("initial environment primary and extra whitelists were not merged")
	}
	steps := []struct {
		name string
		list []string
	}{
		{name: "empty settings page whitelist", list: []string{}},
		{name: "add settings page whitelist", list: []string{"203.0.113.10"}},
		{name: "replace settings page whitelist", list: []string{"203.0.113.20"}},
		{name: "clear settings page whitelist", list: []string{}},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			if _, err := svc.Update(ctx, "ssrf.whitelist", step.list); err != nil {
				t.Fatalf("save whitelist: %v", err)
			}
			for _, address := range []string{"198.18.12.34", "198.19.12.34"} {
				if !utils.IsSSRFWhitelisted(address) {
					t.Fatalf("settings page edit removed deployment fake IP whitelist for %s", address)
				}
			}
			for _, address := range []string{"203.0.113.99", "203.0.113.10", "203.0.113.20"} {
				want := len(step.list) > 0 && step.list[0] == address
				if got := utils.IsSSRFWhitelisted(address); got != want {
					t.Fatalf("primary whitelist for %s = %v, want %v", address, got, want)
				}
			}
			for _, address := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.2", "169.254.169.254"} {
				if err := utils.ValidateURLForSSRF("http://" + address + "/"); err == nil {
					t.Fatalf("settings page edit permitted restricted address %s", address)
				}
			}
		})
	}
	// Reload uses the same merge bridge as a settings-page save.
	repo.row = &types.SystemSetting{
		Key: "ssrf.whitelist", Value: types.JSON(`["203.0.113.30"]`),
		ValueType: "string_list", LastModifiedBy: "personal-user",
	}
	svc.reload(ctx, "ssrf.whitelist")
	if !utils.IsSSRFWhitelisted("203.0.113.30") || !utils.IsSSRFWhitelisted("198.18.12.34") {
		t.Fatal("settings reload did not merge primary and deployment fake IP whitelist")
	}
	t.Setenv("SSRF_WHITELIST_EXTRA", "")
	svc.applySSRFWhitelist(ctx)
	if utils.IsSSRFWhitelisted("198.18.12.34") {
		t.Fatal("empty deployment extra whitelist did not disable fake IP allowance")
	}
	if !utils.IsSSRFWhitelisted("203.0.113.30") {
		t.Fatal("disabling deployment extra whitelist removed settings page whitelist")
	}
}
