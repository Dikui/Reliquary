package types

import (
	"testing"
)

func TestParseProviderScheme(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"local://space/file.pdf", "local"},
		{"https://example.com/img.png", ""},
		{"http://localhost:9000/bucket/key", ""},
		{"/data/files/images/abc.png", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseProviderScheme(tt.input)
			if got != tt.want {
				t.Errorf("ParseProviderScheme(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// strPtr returns a pointer to the given string, used to express *string literals in tests.
func strPtr(s string) *string { return &s }
