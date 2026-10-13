// Saved Markdown locations use exact Unicode positions without changing chunking.
// The text block/rune intersection approach follows WeKnora internal/sourceloc;
// Markdown headings and paragraph identities are specific to the saved body.
package types

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var heading = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.*?)\s*$`)
var closingHeading = regexp.MustCompile(`[ \t]+#+[ \t]*$`)
var listItem = regexp.MustCompile(`^ {0,3}(?:[-+*]|[0-9]+[.)])[ \t]+`)
var setext = regexp.MustCompile(`^ {0,3}(=+|-+)\s*$`)

type block struct {
	start, end int
	path       []string
}

func blocks(body string) []block {
	lines := strings.SplitAfter(body, "\n")
	out := []block{}
	path := make([]string, 6)
	offset := 0
	fence := ""
	boundary := true
	for i, line := range lines {
		if line == "" {
			continue
		}
		text := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		trimmed := strings.TrimSpace(text)
		nextBoundary := false
		level, title := 0, ""
		setextTitle := false
		if fence == "" {
			if m := heading.FindStringSubmatch(text); m != nil {
				level, title = len(m[1]), strings.TrimSpace(closingHeading.ReplaceAllString(m[2], ""))
			}
			if level == 0 && trimmed != "" && i+1 < len(lines) {
				if m := setext.FindStringSubmatch(strings.TrimSpace(lines[i+1])); m != nil {
					level = 2
					setextTitle = true
					if m[1][0] == '=' {
						level = 1
					}
					title = trimmed
				}
			}
			if level > 0 {
				path[level-1] = title
				for n := level; n < 6; n++ {
					path[n] = ""
				}
				boundary = true
				nextBoundary = !setextTitle
			}
			if listItem.MatchString(text) {
				boundary = true
			}
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				fence = string(trimmed[0])
				for len(fence) < len(trimmed) && trimmed[len(fence)] == trimmed[0] {
					fence += string(trimmed[0])
				}
				boundary = true
			}
			if i > 0 && setext.MatchString(text) && strings.TrimSpace(lines[i-1]) != "" {
				nextBoundary = true
			}
		} else if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, string(fence[0])+" \t") == "" {
			fence = ""
			nextBoundary = true
		}
		if trimmed != "" && (boundary || len(out) == 0) {
			p := []string{}
			for _, h := range path {
				if h != "" {
					p = append(p, h)
				}
			}
			if len(out) > 0 {
				out[len(out)-1].end = offset
			}
			out = append(out, block{start: offset, path: p})
			boundary = false
		}
		offset += len([]rune(line))
		if nextBoundary || (trimmed == "" && fence == "") {
			boundary = true
		}
	}
	if len(out) > 0 {
		out[0].start = 0
		out[len(out)-1].end = offset
	}
	return out
}

// AssignSourceLocations verifies source ranges before assigning them. Generated text does not
// receive an invented exact location. Paragraph ordinals are one based.
func AssignSourceLocations(body string, chunks []*Chunk) {
	runes := []rune(body)
	parts := blocks(body)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	for _, c := range chunks {
		c.SourceLocations = []SourceLocation{}
		if c.StartAt < 0 || c.EndAt > len(runes) || c.EndAt <= c.StartAt || string(runes[c.StartAt:c.EndAt]) != c.Content {
			continue
		}
		first := sort.Search(len(parts), func(i int) bool { return parts[i].end > c.StartAt })
		for i := first; i < len(parts) && parts[i].start < c.EndAt; i++ {
			p := parts[i]
			a, b := max(c.StartAt, p.start), min(c.EndAt, p.end)
			if a < b {
				c.SourceLocations = append(c.SourceLocations, SourceLocation{HeadingPath: p.path, Paragraph: i + 1, Start: a, End: b, BodyHash: hash, Mapping: "exact"})
			}
		}
	}
}

// AssignStoredBodyLocations maps the existing LF chunker offsets back to a
// saved CRLF body. The chunk text and the saved body are both retained.
func AssignStoredBodyLocations(body string, c *Chunk) {
	if strings.Contains(body, "\r\n") {
		raw := []rune(body)
		normalized := make([]rune, 0, len(raw))
		offsets := []int{0}
		for i := 0; i < len(raw); i++ {
			if raw[i] == '\r' && i+1 < len(raw) && raw[i+1] == '\n' {
				i++
				normalized = append(normalized, '\n')
			} else {
				normalized = append(normalized, raw[i])
			}
			offsets = append(offsets, i+1)
		}
		if c.StartAt >= 0 && c.EndAt > c.StartAt && c.EndAt <= len(normalized) && string(normalized[c.StartAt:c.EndAt]) == c.Content {
			mapped := *c
			mapped.StartAt, mapped.EndAt = offsets[c.StartAt], offsets[c.EndAt]
			mapped.Content = string(raw[mapped.StartAt:mapped.EndAt])
			AssignSourceLocations(body, []*Chunk{&mapped})
			c.SourceLocations = mapped.SourceLocations
			for i := range c.SourceLocations {
				c.SourceLocations[i].Mapping = "normalized_line_endings"
			}
			return
		}
	}
	AssignSourceLocations(body, []*Chunk{c})
}
