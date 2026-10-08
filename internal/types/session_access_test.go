package types

import "testing"

func TestSanitizeClientSessionDescription(t *testing.T) {
	planted := SkillMaintenanceSessionMarker + "install"
	if got := SanitizeClientSessionDescription(planted, ""); got != "" {
		t.Fatalf("planted marker on create = %q, want empty", got)
	}
	if got := SanitizeClientSessionDescription(planted, "ordinary"); got != "" {
		t.Fatalf("planted marker on update = %q, want empty", got)
	}
	if got := SanitizeClientSessionDescription("hello", "ordinary"); got != "hello" {
		t.Fatalf("ordinary update = %q, want hello", got)
	}
	if got := SanitizeClientSessionDescription("unhide", planted); got != planted {
		t.Fatalf("maintenance row must keep its marker, got %q", got)
	}
}
