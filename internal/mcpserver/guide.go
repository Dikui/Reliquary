package mcpserver

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"reliquary/skills"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

type guideWorkflow struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	content     string
}

func loadGuideWorkflows() ([]guideWorkflow, error) {
	files, err := fs.Glob(skills.Files, "reliquary/workflows/*.md")
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no embedded workflows found")
	}
	workflows := make([]guideWorkflow, 0, len(files))
	for _, filename := range files {
		data, err := skills.Files.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filename, err)
		}
		frontmatter, ok := strings.CutPrefix(string(data), "---\n")
		if !ok {
			return nil, fmt.Errorf("missing frontmatter in %s", filename)
		}
		frontmatter, _, ok = strings.Cut(frontmatter, "\n---\n")
		if !ok {
			return nil, fmt.Errorf("unclosed frontmatter in %s", filename)
		}
		var workflow guideWorkflow
		if err := yaml.Unmarshal([]byte(frontmatter), &workflow); err != nil {
			return nil, fmt.Errorf("parse %s: %w", filename, err)
		}
		if workflow.Name != strings.TrimSuffix(path.Base(filename), ".md") || strings.TrimSpace(workflow.Description) == "" {
			return nil, fmt.Errorf("invalid workflow name or description in %s", filename)
		}
		workflow.content = string(data)
		workflows = append(workflows, workflow)
	}
	return workflows, nil
}

func registerGuide(server *mcp.Server) {
	workflows, err := loadGuideWorkflows()
	if err != nil {
		panic(fmt.Errorf("load embedded Reliquary workflows: %w", err))
	}
	var names, summaries []string
	for _, workflow := range workflows {
		names = append(names, workflow.Name)
		summaries = append(summaries, fmt.Sprintf("- %s: %s", workflow.Name, workflow.Description))
	}
	mcp.AddTool(server, &mcp.Tool{Name: "reliquary_guide", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)},
		Description: "多步任务先读取指南。省略 workflow 列出全部流程及使用场景，传名称返回对应流程全文；未知名称返回错误及可用名称。" + externalContentNotice,
		InputSchema: object(map[string]any{"workflow": text("可选流程名称，省略列出全部流程及使用场景")})},
		func(_ context.Context, _ *mcp.CallToolRequest, input struct {
			Workflow string `json:"workflow,omitempty"`
		}) (*mcp.CallToolResult, any, error) {
			content := strings.Join(summaries, "\n")
			if input.Workflow != "" {
				content = ""
				for _, workflow := range workflows {
					if workflow.Name == input.Workflow {
						content = workflow.content
						break
					}
				}
				if content == "" {
					return nil, nil, fmt.Errorf("unknown workflow %q; available workflows: %s", input.Workflow, strings.Join(names, ", "))
				}
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: content}}}, nil, nil
		})
}
