package types

import "testing"

func TestTenantRolePermissionMatrix(t *testing.T) {
	requirements := []TenantRole{TenantRoleViewer, TenantRoleContributor, TenantRoleAdmin, TenantRoleOwner}
	cases := []struct {
		role    TenantRole
		allowed [4]bool
	}{
		{TenantRoleViewer, [4]bool{true, false, false, false}},
		{TenantRoleContributor, [4]bool{true, true, false, false}},
		{TenantRoleAdmin, [4]bool{true, true, true, false}},
		{TenantRoleOwner, [4]bool{true, true, true, true}},
	}
	for _, tc := range cases {
		for i, required := range requirements {
			t.Run(string(tc.role)+"/"+string(required), func(t *testing.T) {
				if got := tc.role.HasPermission(required); got != tc.allowed[i] {
					t.Fatalf("role=%s required=%s allowed=%v want=%v", tc.role, required, got, tc.allowed[i])
				}
			})
		}
	}
}

func TestTenantRolePermissionRejectsUnknownRoles(t *testing.T) {
	roles := []TenantRole{TenantRoleViewer, TenantRoleContributor, TenantRoleAdmin, TenantRoleOwner, "", "unknown"}
	for _, invalid := range []TenantRole{"", "unknown", "system_admin"} {
		for _, role := range roles {
			if invalid.HasPermission(role) {
				t.Errorf("invalid caller role %q received permission for %q", invalid, role)
			}
			if role.HasPermission(invalid) {
				t.Errorf("role %q received permission for invalid requirement %q", role, invalid)
			}
		}
	}
}
