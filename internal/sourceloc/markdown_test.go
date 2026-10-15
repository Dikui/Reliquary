package sourceloc

import (
	"github.com/stretchr/testify/require"
	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/types"
	"strings"
	"testing"
)

func TestMarkdownPositionsPartitionEveryChunk(t *testing.T) {
	body := "# 项目 Project 🚀\n\n第一段 English 😀。\n\n## 数据\n\n- 第一项\n  continuation\n- 第二项\n\n| 名称 | 值 |\n| --- | --- |\n| 中文 | 42 |\n\n```md\n# not a heading\n\ncode 😀\n```\n\n![说明](../../attachments/a/image.png)\n\n重复段落。\n\n重复段落。\n"
	for _, strategy := range []string{"legacy", "heading", "heuristic", "auto"} {
		t.Run(strategy, func(t *testing.T) {
			parts := chunker.Split(body, chunker.SplitterConfig{Strategy: strategy, ChunkSize: 83, ChunkOverlap: 12})
			chunks := []*types.Chunk{}
			for _, p := range parts {
				chunks = append(chunks, &types.Chunk{Content: p.Content, StartAt: p.Start, EndAt: p.End})
			}
			Assign(body, chunks)
			runes := []rune(body)
			for _, c := range chunks {
				require.NotEmpty(t, c.SourceLocations)
				var b strings.Builder
				for _, loc := range c.SourceLocations {
					require.Positive(t, loc.Paragraph)
					require.Equal(t, "exact", loc.Mapping)
					require.Len(t, loc.BodyHash, 64)
					b.WriteString(string(runes[loc.Start:loc.End]))
					require.NotContains(t, loc.HeadingPath, "not a heading")
				}
				require.Equal(t, c.Content, b.String())
			}
		})
	}
	c := &types.Chunk{Content: body, EndAt: len([]rune(body))}
	Assign(body, []*types.Chunk{c})
	require.Equal(t, []string{"项目 Project 🚀", "数据"}, c.SourceLocations[len(c.SourceLocations)-1].HeadingPath)
}
func TestInvalidRangeCannotInventLocation(t *testing.T) {
	c := &types.Chunk{StartAt: 0, EndAt: 3, Content: "generated summary"}
	Assign("原文😀", []*types.Chunk{c})
	require.Empty(t, c.SourceLocations)
}
