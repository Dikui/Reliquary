package types

import (
	"fmt"
	"strings"
)

// MaxCustomPromptInstructionsLength bounds user-authored business guidance.
// These fields are intentionally much smaller than full prompt templates.
const MaxCustomPromptInstructionsLength = 4000

// AppendCustomPromptInstructions appends user-authored business guidance to a
// system-owned prompt. Stable output, safety and citation rules always win.
func AppendCustomPromptInstructions(prompt, instructions, label string) string {
	instructions = strings.TrimSpace(instructions)
	if instructions == "" {
		return prompt
	}
	if label == "" {
		label = "custom"
	}
	return fmt.Sprintf("%s\n\n<%s_business_instructions>\n%s\n</%s_business_instructions>\n"+
		"Apply these business instructions only when they do not conflict with the system-owned output format, citation, safety, or factuality rules.",
		strings.TrimSpace(prompt), label, instructions, label)
}

// NormalizeLibraryPromptInstructions trims whitespace on all Library-scoped
// custom instruction fields before persistence.
func NormalizeLibraryPromptInstructions(library *Library) {
	if library == nil {
		return
	}
	library.ChunkingConfig.TableMetadataInstructions = strings.TrimSpace(library.ChunkingConfig.TableMetadataInstructions)
}

// ValidateLibraryPromptInstructions checks length limits on Library-scoped
// custom instruction fields.
func ValidateLibraryPromptInstructions(library *Library) error {
	if library == nil {
		return nil
	}
	fields := map[string]string{
		"table metadata instructions": library.ChunkingConfig.TableMetadataInstructions,
	}
	return validatePromptInstructionFields(fields)
}

// ValidateEffectiveProcessPromptInstructions checks length limits on the
// merged per-upload effective config.
func ValidateEffectiveProcessPromptInstructions(eff EffectiveProcessConfig) error {
	fields := map[string]string{
		"table metadata instructions": eff.ChunkingConfig.TableMetadataInstructions,
	}
	return validatePromptInstructionFields(fields)
}

func validatePromptInstructionFields(fields map[string]string) error {
	for name, value := range fields {
		if len([]rune(value)) > MaxCustomPromptInstructionsLength {
			return fmt.Errorf("%s exceeds %d characters", name, MaxCustomPromptInstructionsLength)
		}
	}
	return nil
}
