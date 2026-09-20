package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/sandbox"
)

func TestSkillRelativeFilePathNormalizesCopiedImagePaths(t *testing.T) {
	rel, err := skillRelativeFilePath("ppt-generator", "scripts/generate_ppt.py")
	require.NoError(t, err)
	assert.Equal(t, "scripts/generate_ppt.py", rel)

	rel, err = skillRelativeFilePath("ppt-generator", "ppt-generator/scripts/generate_ppt.py")
	require.NoError(t, err)
	assert.Equal(t, "scripts/generate_ppt.py", rel)

	rel, err = skillRelativeFilePath(
		"ppt-generator",
		sandbox.SkillsImageRoot+"/ppt-generator/scripts/generate_ppt.py",
	)
	require.NoError(t, err)
	assert.Equal(t, "scripts/generate_ppt.py", rel)

	rel, err = skillRelativeFilePath("ppt-generator", sandbox.SkillsImageRoot+"/ppt-generator")
	require.NoError(t, err)
	assert.Empty(t, rel)

	_, err = skillRelativeFilePath("ppt-generator", sandbox.SkillsImageRoot+"/other/scripts/x.py")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `skill "other"`)

	_, err = skillRelativeFilePath("ppt-generator", "/workspace/output/x.py")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "relative")
}
