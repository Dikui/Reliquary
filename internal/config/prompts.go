package config

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

var loadedPrompts atomic.Value   // map[string]string; published together at startup.
var promptOverrides atomic.Value // map[string]string; published after persisted settings change.

var promptFiles = []struct {
	name string
	ids  []string
}{
	{
		"processing.yaml",
		[]string{
			"article_processing",
			"summary_merge",
			"fact_grouping",
			"story_consolidation",
			"output_correction",
			"model_connection_test",
			"model_maintenance_probe",
		},
	},
	{"evaluation.yaml", []string{"evaluation_questions", "evaluation_questions_multi_library"}},
}

// LoadPromptTemplates reads and validates the complete set before starting workers.
// Files are read only here; editing a file takes effect after a process restart.
func LoadPromptTemplates(configDir string) error {
	prompts := make(map[string]string)
	directory := filepath.Join(configDir, "prompt_templates")
	for _, file := range promptFiles {
		path := filepath.Join(directory, file.name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("load prompt templates: %w", err)
		}
		var document struct {
			Templates []struct {
				ID      string `yaml:"id"`
				Name    string `yaml:"name"`
				Content string `yaml:"content"`
			} `yaml:"templates"`
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&document); err != nil {
			return fmt.Errorf("load prompt templates %s: %w", path, err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("prompt templates %s must contain one YAML document", path)
		}
		required := make(map[string]bool, len(file.ids))
		for _, id := range file.ids {
			required[id] = true
		}
		for _, template := range document.Templates {
			if !required[template.ID] {
				return fmt.Errorf("prompt templates %s: unknown or duplicate id %q", path, template.ID)
			}
			if strings.TrimSpace(template.Content) == "" {
				return fmt.Errorf("prompt templates %s: empty content for %q", path, template.ID)
			}
			prompts[template.ID] = template.Content
			delete(required, template.ID)
		}
		for _, id := range file.ids {
			if required[id] {
				return fmt.Errorf("prompt templates %s: missing id %q", path, id)
			}
		}
	}
	loadedPrompts.Store(prompts)
	return nil
}

// PromptIDs returns the fixed set of templates that operators may edit.
func PromptIDs() []string {
	var ids []string
	for _, file := range promptFiles {
		ids = append(ids, file.ids...)
	}
	return ids
}

// PromptDefaults returns a copy of the validated file defaults.
func PromptDefaults() map[string]string {
	if prompts, ok := loadedPrompts.Load().(map[string]string); ok {
		return maps.Clone(prompts)
	}
	return map[string]string{}
}

// SetPromptOverrides publishes saved settings without changing file defaults.
func SetPromptOverrides(prompts map[string]string) {
	promptOverrides.Store(maps.Clone(prompts))
}

// Prompt returns a saved override or the required template's file default.
// An absent ID indicates an uninitialized process or a programming error.
func Prompt(id string, libraryIDs ...string) string {
	key := id
	if len(libraryIDs) > 0 && libraryIDs[0] != "" {
		key = "library." + libraryIDs[0] + "." + id
	}
	if prompts, ok := promptOverrides.Load().(map[string]string); ok {
		if content, exists := prompts[key]; exists {
			return content
		}
	}
	if prompts, ok := loadedPrompts.Load().(map[string]string); ok {
		if content, exists := prompts[id]; exists {
			return content
		}
	}
	panic("prompt template is not loaded: " + id)
}

// PromptScopeIDs exposes only templates that can run in the selected scope.
func PromptScopeIDs(library bool) []string {
	if library {
		return []string{"article_processing", "summary_merge", "fact_grouping", "story_consolidation", "output_correction", "evaluation_questions"}
	}
	return []string{"evaluation_questions_multi_library", "model_connection_test", "model_maintenance_probe"}
}
