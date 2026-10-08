package utils

import "testing"

func TestGetMaxFileSizeMB_Default(t *testing.T) {
	t.Setenv("MAX_FILE_SIZE_MB", "")
	if GetMaxFileSizeMB() != defaultMaxFileSizeMB {
		t.Fatalf("knowledge cap must stay %d MB", defaultMaxFileSizeMB)
	}
}
