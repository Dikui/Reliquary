package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

var loadedPrompts atomic.Value // map[string]string; published together at startup.

var promptFiles = []struct {
	name string
	ids  []string
}{
	{"processing.yaml", []string{"article_processing", "article_entities", "library_media", "library_reference", "library_own", "library_institutional", "article_relations", "content_type_hint", "summary_merge", "topic_backfill"}},
	{"verification.yaml", []string{"graph_verification"}},
	{"entity_resolution.yaml", []string{"entity_resolution"}},
	{"content_check.yaml", []string{"content_check"}},
	{"evaluation.yaml", []string{"evaluation_questions"}},
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

// Prompt returns a required template from the startup snapshot.
// An absent ID indicates an uninitialized process or a programming error.
func Prompt(id string) string {
	if prompts, ok := loadedPrompts.Load().(map[string]string); ok {
		if content, exists := prompts[id]; exists {
			return content
		}
	}
	panic("prompt template is not loaded: " + id)
}
