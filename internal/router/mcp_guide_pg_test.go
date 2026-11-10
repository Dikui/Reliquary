package router

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reliquary/skills"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestMCPGuideWorkflows(t *testing.T) {
	f := newMCPFixture(t)
	client := f.connect(t, "2025-11-25")
	names := []string{"compare", "entity-profile", "ingest", "review-check", "shelf-expert", "shelf-import", "topic-brief", "write-report"}
	call := func(t *testing.T, arguments any) *mcp.CallToolResult {
		t.Helper()
		result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "reliquary_guide", Arguments: arguments})
		require.NoError(t, err)
		require.Len(t, result.Content, 1)
		data, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotRegexp(t, mcpDeprecatedTerms, string(data))
		return result
	}

	var summaries []string
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			data, err := skills.Files.ReadFile("reliquary/workflows/" + name + ".md")
			require.NoError(t, err)
			frontmatter, ok := strings.CutPrefix(string(data), "---\n")
			require.True(t, ok)
			frontmatter, body, ok := strings.Cut(frontmatter, "\n---\n")
			require.True(t, ok)
			var metadata struct {
				Name        string `yaml:"name"`
				Description string `yaml:"description"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &metadata))
			require.Equal(t, name, metadata.Name)
			require.NotEmpty(t, strings.TrimSpace(metadata.Description))
			summaries = append(summaries, fmt.Sprintf("- %s: %s", name, metadata.Description))
			for _, section := range []string{"步骤", "停止条件", "输出格式", "禁止事项"} {
				require.Contains(t, body, "## "+section+"\n")
			}
			result := call(t, map[string]any{"workflow": name})
			require.False(t, result.IsError)
			text := result.Content[0].(*mcp.TextContent).Text
			require.Equal(t, string(data), text)
		})
	}
	for _, arguments := range []any{nil, map[string]any{}} {
		result := call(t, arguments)
		require.False(t, result.IsError)
		text := result.Content[0].(*mcp.TextContent).Text
		require.Equal(t, summaries, strings.Split(strings.TrimSpace(text), "\n"))
		require.Len(t, strings.Split(strings.TrimSpace(text), "\n"), len(names))
	}
	for _, name := range []string{"unknown", "../rules"} {
		result := call(t, map[string]any{"workflow": name})
		require.True(t, result.IsError)
		text := result.Content[0].(*mcp.TextContent).Text
		require.Contains(t, text, name)
		for _, available := range names {
			require.Contains(t, text, available)
		}
	}
}
