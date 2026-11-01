package router

import (
	"strings"
	"testing"
	"unicode/utf8"

	"reliquary/skills"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestMCPSkillInstructions(t *testing.T) {
	rules, err := skills.Files.ReadFile("reliquary/rules.md")
	require.NoError(t, err)
	require.True(t, utf8.Valid(rules))
	require.NotEmpty(t, strings.TrimSpace(string(rules)))
	require.LessOrEqual(t, utf8.RuneCount(rules), 1500)
	require.NotRegexp(t, `文章|本人文档|用户文档|整理结果|摘要加链接|event、opinion`, string(rules))

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

	f := newMCPFixture(t)
	for _, version := range []string{"2025-06-18", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			client := f.connect(t, version)
			result := client.InitializeResult()
			require.NotNil(t, result)
			require.Equal(t, version, result.ProtocolVersion)
			require.Equal(t, string(rules), result.Instructions)
		})
	}
}
