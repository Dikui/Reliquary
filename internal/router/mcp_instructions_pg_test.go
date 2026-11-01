package router

import (
	"context"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"reliquary/skills"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const mcpDeprecatedTerms = `文章|本人文档|用户文档|整理结果|摘要加链接|event、opinion`

func TestMCPSkillInstructions(t *testing.T) {
	rules, err := skills.Files.ReadFile("reliquary/rules.md")
	require.NoError(t, err)
	require.True(t, utf8.Valid(rules))
	require.NotEmpty(t, strings.TrimSpace(string(rules)))
	require.LessOrEqual(t, utf8.RuneCount(rules), 1500)
	require.NotRegexp(t, mcpDeprecatedTerms, string(rules))

	entrypoint, err := skills.Files.ReadFile("reliquary/SKILL.md")
	require.NoError(t, err)
	require.True(t, utf8.Valid(entrypoint))
	frontmatter, ok := strings.CutPrefix(string(entrypoint), "---\n")
	require.True(t, ok, "SKILL.md must start with YAML frontmatter")
	frontmatter, body, ok := strings.Cut(frontmatter, "\n---\n")
	require.True(t, ok, "SKILL.md frontmatter must have a closing delimiter")
	var metadata struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Metadata    struct {
			TestedAgainst string `yaml:"tested_against"`
		} `yaml:"metadata"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &metadata))
	require.Equal(t, "reliquary", metadata.Name)
	require.Regexp(t, `^[a-z0-9]+(-[a-z0-9]+)*$`, metadata.Name)
	require.LessOrEqual(t, len(metadata.Name), 64)
	require.NotEmpty(t, strings.TrimSpace(metadata.Description))
	require.LessOrEqual(t, utf8.RuneCountInString(metadata.Description), 1024)
	require.Regexp(t, `^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`, metadata.Metadata.TestedAgainst)
	require.Less(t, strings.Count(strings.TrimSpace(body), "\n")+1, 200)
	for _, match := range regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`).FindAllStringSubmatch(body, -1) {
		target, _, _ := strings.Cut(match[1], "#")
		if strings.Contains(target, "://") || target == "" {
			continue
		}
		_, err := fs.Stat(skills.Files, path.Join("reliquary", target))
		require.NoError(t, err, "SKILL.md reference %s must exist", match[1])
	}

	f := newMCPFixture(t)
	for _, version := range []string{"2025-06-18", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			client := f.connect(t, version)
			result := client.InitializeResult()
			require.NotNil(t, result)
			require.Equal(t, version, result.ProtocolVersion)
			require.Equal(t, string(rules), result.Instructions)
			require.NotRegexp(t, mcpDeprecatedTerms, result.Instructions)
		})
	}
}

func TestMCPSkillToolReferences(t *testing.T) {
	f := newMCPFixture(t)
	client := f.connect(t, "2025-11-25")
	listed, err := client.ListTools(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, listed.NextCursor, "check all tools if tools/list becomes paginated")
	tools := make(map[string]bool, len(listed.Tools))
	for _, tool := range listed.Tools {
		tools[tool.Name] = true
	}
	// Only parameters, response fields and literal values may bypass tools/list.
	allowed := strings.Fields(`
		acquisition asset_id author candidates cannot_be_fact confirmed content current_result cursor
		data description edges end entity entity_ids entry_id evidence findings global has_update hops
		id identifier images involved items kind latest_entry_id length libraries library library_id
		limit locations mode next_cursor next_offset offset page pending_sync published_at published_from
		published_to publisher query quote reason rerank_status results since source_ref source_unverified
		source_url sources start status target target_ids topic_ids total truncated urls url
		vector_unavailable workflow
		applied confirmation_required conflict discarded duplicate extracted in_library model_written
		needs_selection not_found pending_review result_error
	`)
	for _, identifier := range allowed {
		require.False(t, tools[identifier], "whitelist must not hide a registered tool: %s", identifier)
	}
	whitelist := make(map[string]bool, len(allowed))
	for _, identifier := range allowed {
		whitelist[identifier] = true
	}
	workflows, err := fs.Glob(skills.Files, "reliquary/workflows/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, workflows)
	files := append([]string{"reliquary/SKILL.md", "reliquary/rules.md"}, workflows...)
	identifier := regexp.MustCompile("`([a-z][a-z0-9_]*)(?:=[^`\\r\\n]+)?`")
	for _, filename := range files {
		t.Run(filename, func(t *testing.T) {
			data, err := skills.Files.ReadFile(filename)
			require.NoError(t, err)
			for _, match := range identifier.FindAllStringSubmatch(string(data), -1) {
				name := match[1]
				require.True(t, tools[name] || whitelist[name], "%s references unknown tool %q; fix the tool name or explicitly whitelist a field/value", filename, name)
			}
		})
	}
}
