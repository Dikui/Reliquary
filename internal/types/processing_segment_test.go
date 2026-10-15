package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProcessingSegmentsCoverMarkdown(t *testing.T) {
	for name, body := range map[string]string{
		"headings":   "# 中文🙂\r\n\r\n第一段。\r\n\r\n## Child\n\nSecond paragraph.\n\nThird paragraph.",
		"table":      "# Table\n\n| A | B |\n|---|---|\n| 一 | 二 |\n| 三 | 四 |\n\nTail.",
		"fence":      "# Code\n\n````go\n# not heading\n\n```\nfmt.Println(1)\n````\n\nTail.",
		"indented":   "# Code\n\n    first line\n\n    second line\n\nTail.",
		"long_block": "# Oversize\n\n" + strings.Repeat("正文🙂", 90),
		"blank":      "  \n\n",
	} {
		t.Run(name, func(t *testing.T) {
			parts, err := PlanProcessingSegments(body, 65, 7, 100)
			require.NoError(t, err)
			runes := []rune(body)
			joined := ""
			end := 0
			for i, p := range parts {
				require.Equal(t, i+1, p.Number)
				require.Equal(t, end, p.Start)
				require.LessOrEqual(t, p.End-p.Start, 65)
				require.NotEmpty(t, p.Hash)
				joined += string(runes[p.Start:p.End])
				end = p.End
				if p.ContextBefore != "" {
					require.LessOrEqual(t, len([]rune(p.ContextBefore)), 7)
					require.True(t, strings.HasSuffix(string(runes[:p.Start]), p.ContextBefore))
				}
			}
			require.Equal(t, body, joined)
			require.Equal(t, len(runes), end)
			if name == "table" || name == "fence" || name == "indented" {
				needle := "| A | B |\n|---|---|\n| 一 | 二 |\n| 三 | 四 |"
				if name == "fence" {
					needle = "````go\n# not heading\n\n```\nfmt.Println(1)\n````"
				}
				if name == "indented" {
					needle = "    first line\n\n    second line"
				}
				found := false
				for _, p := range parts {
					if strings.Contains(string(runes[p.Start:p.End]), needle) {
						found = true
					}
				}
				require.True(t, found, "ordinary atomic block must stay intact")
			}
		})
	}
}

func TestProcessingSegmentsShortAndLimit(t *testing.T) {
	parts, err := PlanProcessingSegments("# Small\n\nComplete body", 100, 7, 1)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	_, err = PlanProcessingSegments(strings.Repeat("字", 100), 20, 4, 4)
	var limit *SegmentLimitError
	require.ErrorAs(t, err, &limit)
	require.Equal(t, 5, limit.Count)
}
