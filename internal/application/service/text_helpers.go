package service

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

func previewText(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	r := []rune(s)
	if maxRunes <= 0 || len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "...(truncated)"
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

var minTextContentRunes = 10

var (
	// Markdown image references like ![alt](path) — pure visual placeholders
	// with no extractable text, so the whole reference is removed.
	mdImageRefRE = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)

	// <image_original>...</image_original> blocks wrap the verbatim Markdown
	// image reference inside an enriched <image> block (see
	// searchutil.EnrichContentWithImageInfo). The content is just a redundant
	// copy of an already-stripped image link, so the whole block (tags +
	// content) is removed.
	imageOriginalBlockRE = regexp.MustCompile(`(?is)<image_original\b[^>]*>.*?</image_original>`)

	// Self-closing or attribute-only HTML <img> tags.
	htmlImgTagRE = regexp.MustCompile(`(?i)<img\b[^>]*/?>`)

	// Image wrapper tags are removed while parser-provided inner text is preserved.
	imageWrapperTagRE = regexp.MustCompile(`(?i)</?image[a-z_]*\b[^>]*/?>`)
)

// stripImageMarkup removes image placeholders and unwraps parser image text.
func stripImageMarkup(s string) string {
	s = imageOriginalBlockRE.ReplaceAllString(s, "")
	s = mdImageRefRE.ReplaceAllString(s, "")
	s = htmlImgTagRE.ReplaceAllString(s, "")
	s = imageWrapperTagRE.ReplaceAllString(s, "")
	return s
}

// extractRealText returns the trimmed content with image markup stripped.
// Cached at the call site for use both in the threshold check and in any
// subsequent log message, avoiding redundant regex passes over large docs.
func extractRealText(content string) string {
	return strings.TrimSpace(stripImageMarkup(content))
}

// realTextRuneCount returns the rune length of the content after image
// markup is stripped. Uses utf8.RuneCountInString to avoid allocating a
// rune slice for the count.
func realTextRuneCount(content string) int {
	return utf8.RuneCountInString(extractRealText(content))
}
