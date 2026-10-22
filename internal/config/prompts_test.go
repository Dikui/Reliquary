package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptTemplatesUseValidatedStartupSnapshot(t *testing.T) {
	directory := t.TempDir()
	templates := filepath.Join(directory, "prompt_templates")
	require.NoError(t, os.Mkdir(templates, 0755))
	for _, file := range promptFiles {
		data, err := os.ReadFile(filepath.Join("../../config/prompt_templates", file.name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(templates, file.name), data, 0644))
	}
	require.NoError(t, LoadPromptTemplates(directory))
	original := Prompt("article_processing")
	require.Contains(t, original, "个人知识库")

	path := filepath.Join(templates, "processing.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	updated := strings.Replace(string(data), "个人知识库", "测试知识库", 1)
	require.NoError(t, os.WriteFile(path, []byte(updated), 0644))
	require.Equal(t, original, Prompt("article_processing"), "disk edits must not change running requests")
	require.NoError(t, LoadPromptTemplates(directory))
	require.Contains(t, Prompt("article_processing"), "测试知识库")

	// A damaged configuration must fail as a whole, never publish partial templates.
	broken := strings.Replace(updated, "id: article_processing", "id: misspelled_processing", 1)
	require.NoError(t, os.WriteFile(path, []byte(broken), 0644))
	require.ErrorContains(t, LoadPromptTemplates(directory), "misspelled_processing")
	require.Contains(t, Prompt("article_processing"), "测试知识库")
}
