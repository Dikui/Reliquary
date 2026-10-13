package types

import (
	"strings"
	"testing"
)

func TestAppendCustomPromptInstructions(t *testing.T) {
	t.Run("empty preserves prompt", func(t *testing.T) {
		if got := AppendCustomPromptInstructions("base", "  ", "table_metadata"); got != "base" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("appends bounded business guidance after base", func(t *testing.T) {
		got := AppendCustomPromptInstructions("base", " Focus on contracts. ", "table_metadata")
		if !strings.HasPrefix(got, "base\n\n<table_metadata_business_instructions>") {
			t.Fatalf("unexpected prefix: %q", got)
		}
		if !strings.Contains(got, "Focus on contracts.") || !strings.Contains(got, "do not conflict") {
			t.Fatalf("missing guidance or precedence rule: %q", got)
		}
	})
}

func TestValidateLibraryPromptInstructions(t *testing.T) {
	library := &Library{
		ChunkingConfig: ChunkingConfig{TableMetadataInstructions: strings.Repeat("a", MaxCustomPromptInstructionsLength+1)},
	}
	if err := ValidateLibraryPromptInstructions(library); err == nil {
		t.Fatal("expected length validation error")
	}
}

func TestNormalizeLibraryPromptInstructions(t *testing.T) {
	library := &Library{
		ChunkingConfig: ChunkingConfig{TableMetadataInstructions: "  focus table columns  "},
	}
	NormalizeLibraryPromptInstructions(library)
	if library.ChunkingConfig.TableMetadataInstructions != "focus table columns" {
		t.Fatalf("got %q", library.ChunkingConfig.TableMetadataInstructions)
	}
}
