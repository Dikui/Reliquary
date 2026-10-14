package types

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// ProcessingSegment owns [Start, End) in Unicode code points. ContextBefore
// repeats only the tail of an oversized block and is not part of that range.
type ProcessingSegment struct {
	Number        int      `json:"number"`
	Start         int      `json:"start"`
	End           int      `json:"end"`
	Hash          string   `json:"hash"`
	HeadingPath   []string `json:"heading_path"`
	ContextBefore string   `json:"context_before,omitempty"`
}

const ProcessingSegmentVersion = "markdown-blocks-v1"

type SegmentLimitError struct{ Count, Limit int }

func (e *SegmentLimitError) Error() string {
	return fmt.Sprintf("正文需要至少 %d 段，超过单篇分段上限 %d，请拆分资料后重新收录", e.Count, e.Limit)
}

// PlanProcessingSegments reuses the saved-body Markdown parser. A normal
// table or fenced code block is indivisible. Only a block larger than the
// budget is cut by code point, with separate preceding context.
func PlanProcessingSegments(body string, budget, overlap, limit int) ([]ProcessingSegment, error) {
	if budget < 1 || limit < 1 {
		return nil, fmt.Errorf("invalid segment budget or limit")
	}
	runes := []rune(body)
	parts := processingBlocks(body)
	if len(parts) == 0 {
		parts = []block{{start: 0, end: len(runes)}}
	}
	result := []ProcessingSegment{}
	appendSegment := func(start, end int, path []string, context string) error {
		if len(result) == limit {
			return &SegmentLimitError{len(result) + 1, limit}
		}
		result = append(result, ProcessingSegment{Number: len(result) + 1, Start: start, End: end, Hash: fmt.Sprintf("%x", sha256.Sum256([]byte(string(runes[start:end])))), HeadingPath: append([]string{}, path...), ContextBefore: context})
		return nil
	}
	for i := 0; i < len(parts); {
		p := parts[i]
		if p.end-p.start > budget {
			for start := p.start; start < p.end; {
				end := min(start+budget, p.end)
				context := ""
				if start > p.start {
					context = string(runes[max(p.start, start-overlap):start])
				}
				if err := appendSegment(start, end, p.path, context); err != nil {
					return nil, err
				}
				start = end
			}
			i++
			continue
		}
		j := i + 1
		for j < len(parts) && parts[j].end-p.start <= budget {
			j++
		}
		// Prefer starting a new segment at a heading when the remainder does
		// not fit. Avoid leaving a heading alone whenever its paragraph fits.
		if j < len(parts) {
			for k := j - 1; k > i; k-- {
				if strings.Join(parts[k].path, "\x00") != strings.Join(parts[k-1].path, "\x00") {
					j = k
					break
				}
			}
		}
		if err := appendSegment(p.start, parts[j-1].end, p.path, ""); err != nil {
			return nil, err
		}
		i = j
	}
	return result, nil
}

// Saved locations already keep fenced code and tables together. Indented
// code may span blank lines, which the location parser treats as paragraphs;
// join those boundaries for model input without changing saved locations.
func processingBlocks(body string) []block {
	parts := blocks(body)
	protected := []block{}
	offset, start := 0, -1
	boundary := true
	for _, line := range strings.SplitAfter(body, "\n") {
		blank := strings.TrimSpace(line) == ""
		indented := strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")
		if start >= 0 && !blank && !indented {
			protected = append(protected, block{start: start, end: offset})
			start = -1
		}
		if start < 0 && boundary && indented && !blank {
			start = offset
		}
		offset += len([]rune(line))
		boundary = blank
	}
	if start >= 0 {
		protected = append(protected, block{start: start, end: offset})
	}
	joined := []block{}
	region := 0
	for _, p := range parts {
		for region < len(protected) && p.start >= protected[region].end {
			region++
		}
		if len(joined) > 0 && region < len(protected) && p.start > protected[region].start && p.start < protected[region].end {
			joined[len(joined)-1].end = p.end
		} else {
			joined = append(joined, p)
		}
	}
	return joined
}
