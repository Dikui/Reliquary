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

func TestValidateKnowledgeBasePromptInstructions(t *testing.T) {
	kb := &KnowledgeBase{
		ChunkingConfig: ChunkingConfig{TableMetadataInstructions: strings.Repeat("a", MaxCustomPromptInstructionsLength+1)},
	}
	if err := ValidateKnowledgeBasePromptInstructions(kb); err == nil {
		t.Fatal("expected length validation error")
	}
}

func TestNormalizeKnowledgeBasePromptInstructions(t *testing.T) {
	kb := &KnowledgeBase{
		ChunkingConfig: ChunkingConfig{TableMetadataInstructions: "  focus table columns  "},
	}
	NormalizeKnowledgeBasePromptInstructions(kb)
	if kb.ChunkingConfig.TableMetadataInstructions != "focus table columns" {
		t.Fatalf("got %q", kb.ChunkingConfig.TableMetadataInstructions)
	}
}
