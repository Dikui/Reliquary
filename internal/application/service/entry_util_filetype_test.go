package service

import "testing"

func TestIsValidFileTypeRejectsRemovedHTMLUploads(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{name: "html", filename: "index.html", want: false},
		{name: "uppercase html", filename: "INDEX.HTML", want: false},
		{name: "htm", filename: "legacy.htm", want: false},
		{name: "unsupported", filename: "payload.exe", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidFileType(tt.filename); got != tt.want {
				t.Fatalf("isValidFileType(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

// Direct upload and URL import must agree on the accepted extension set,
// otherwise #2447 (xlsx accepted on upload, rejected on URL import) regresses.
