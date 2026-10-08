package types

import (
	"testing"
)

func TestParseProviderScheme(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"local://tenant/file.pdf", "local"},
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

func TestDefaultParserEngine(t *testing.T) {
	for input, want := range map[string]string{
		"pptx": "anydoc", ".PPT": "anydoc", "ppt": "anydoc",
		"pdf": "anydoc", "csv": "simple", "docx": "anydoc", "": "simple",
	} {
		if got := DefaultParserEngine(input); got != want {
			t.Errorf("DefaultParserEngine(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestChunkingConfigResolveParserEngineDefaults(t *testing.T) {
	var empty ChunkingConfig
	if got := empty.ResolveParserEngine("pptx"); got != "anydoc" {
		t.Fatalf("empty rules ResolveParserEngine(pptx) = %q, want anydoc", got)
	}
	if got := empty.ResolveParserEngine("pdf"); got != "anydoc" {
		t.Fatalf("empty rules ResolveParserEngine(pdf) = %q, want anydoc", got)
	}

	configured := ChunkingConfig{ParserEngineRules: []ParserEngineRule{
		{FileTypes: []string{"pptx"}, Engine: "mineru"},
	}}
	if got := configured.ResolveParserEngine(".PPTX"); got != "mineru" {
		t.Fatalf("configured ResolveParserEngine(pptx) = %q, want mineru", got)
	}
}
