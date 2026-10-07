package service

import (
	"strings"
	"unicode"
)

// markdownCitationText retains source byte offsets while removing presentation
// syntax. Matching remains an exact, continuous substring after normalization.
type markdownCitationText struct {
	text   string
	starts []int
	ends   []int
}

type markdownCitationUnit struct {
	char       rune
	start, end int
}

type markdownCitationRange struct{ start, end int }

func normalizeMarkdownCitation(source string) markdownCitationText {
	return normalizeMarkdownCitationSource(source, true)
}

func normalizeMarkdownCitationSource(source string, mapping bool) markdownCitationText {
	chars := []rune(source)
	brackets := markdownCitationBrackets(chars)
	var offsets []int
	if mapping {
		offsets = make([]int, len(chars)+1)
		for i, offset := 0, 0; i < len(chars); i++ {
			offsets[i] = offset
			offset += len(string(chars[i]))
			offsets[i+1] = offset
		}
	}
	var normalized markdownCitationText
	var text strings.Builder
	text.Grow(len(source))
	var whitespace *markdownCitationUnit
	emitChar := func(char rune, i int) {
		unit := markdownCitationUnit{char: char}
		if mapping {
			unit.start, unit.end = offsets[i], offsets[i+1]
		}
		if markdownCitationSpace(char) {
			if whitespace == nil {
				pending := unit
				whitespace = &pending
			}
			whitespace.end = unit.end
			return
		}
		if whitespace != nil && text.Len() > 0 {
			text.WriteByte(' ')
			if mapping {
				normalized.starts = append(normalized.starts, whitespace.start)
				normalized.ends = append(normalized.ends, whitespace.end)
			}
		}
		whitespace = nil
		value := string(char)
		text.WriteString(value)
		if mapping {
			for range len(value) {
				normalized.starts = append(normalized.starts, unit.start)
				normalized.ends = append(normalized.ends, unit.end)
			}
		}
	}
	var inline func(int, int, int, bool)
	emit := func(i int) { emitChar(chars[i], i) }
	inline = func(start, end, depth int, table bool) {
		runEnd := start
		runAt := func(i int) int {
			if i >= runEnd {
				runEnd = i + markdownCitationRun(chars, i, end)
			}
			return runEnd - i
		}
		for i := start; i < end; {
			if chars[i] == '\\' && i+1 < end && strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", chars[i+1]) {
				emit(i + 1)
				i += 2
				continue
			}
			if depth < 32 && (chars[i] == '[' || (chars[i] == '!' && i+1 < end && chars[i+1] == '[')) {
				label := i
				if chars[label] == '!' {
					label++
				}
				close, found := brackets[label]
				if found && close+1 < end && chars[close+1] == '(' {
					target, found := brackets[close+1]
					if found && target < end {
						inline(label+1, close, depth+1, false)
						i = target + 1
						continue
					}
				}
			}
			if chars[i] == '`' {
				run := runAt(i)
				close := markdownCitationDelimiter(chars, i+run, end, '`', run)
				if close >= 0 {
					for j := i + run; j < close; j++ {
						emit(j)
					}
					i = close + run
					continue
				}
			}
			if depth < 32 && strings.ContainsRune("*_~", chars[i]) {
				run := runAt(i)
				if run <= 3 && (chars[i] != '~' || run == 2) && i+run < end && !markdownCitationSpace(chars[i+run]) &&
					(chars[i] != '_' || i == start || !markdownCitationWord(chars[i-1])) {
					close := markdownCitationDelimiter(chars, i+run, end, chars[i], run)
					if close > i+run && !markdownCitationSpace(chars[close-1]) &&
						(chars[i] != '_' || close+run == end || !markdownCitationWord(chars[close+run])) {
						inline(i+run, close, depth+1, table)
						i = close + run
						continue
					}
				}
			}
			if table && chars[i] == '|' {
				emitChar(' ', i)
			} else {
				emit(i)
			}
			i++
		}
	}
	for start := 0; start < len(chars); {
		end := start
		for end < len(chars) && chars[end] != '\n' {
			end++
		}
		line := strings.TrimSpace(string(chars[start:end]))
		if !markdownCitationTableDivider(line) {
			table := strings.HasPrefix(line, "|") || strings.HasSuffix(line, "|") || strings.Contains(line, " | ")
			inline(start, end, 0, table)
		}
		if end < len(chars) {
			emit(end)
		}
		start = end + 1
	}
	normalized.text = text.String()
	return normalized
}

func markdownCitationRanges(body, quote string) []markdownCitationRange {
	source := normalizeMarkdownCitation(body)
	needle := normalizeMarkdownCitationSource(quote, false).text
	if needle == "" {
		return nil
	}
	var matches []markdownCitationRange
	for offset := 0; offset < len(source.text) && len(matches) < 100; {
		index := strings.Index(source.text[offset:], needle)
		if index < 0 {
			break
		}
		index += offset
		matches = append(matches, markdownCitationRange{source.starts[index], source.ends[index+len(needle)-1]})
		offset = index + len(needle)
	}
	return matches
}

func markdownCitationBrackets(chars []rune) map[int]int {
	pairs := make(map[int]int)
	var labels, targets []int
	for i := 0; i < len(chars); i++ {
		if chars[i] == '\\' {
			i++
			continue
		}
		switch chars[i] {
		case '\n':
			labels, targets = labels[:0], targets[:0]
		case '[':
			labels = append(labels, i)
		case '(':
			targets = append(targets, i)
		case ']':
			if len(labels) > 0 {
				pairs[labels[len(labels)-1]] = i
				labels = labels[:len(labels)-1]
			}
		case ')':
			if len(targets) > 0 {
				pairs[targets[len(targets)-1]] = i
				targets = targets[:len(targets)-1]
			}
		}
	}
	return pairs
}

func markdownCitationRun(chars []rune, start, end int) int {
	i := start + 1
	for i < end && chars[i] == chars[start] {
		i++
	}
	return i - start
}

func markdownCitationDelimiter(chars []rune, start, end int, marker rune, run int) int {
	for i := start; i < end; i++ {
		if chars[i] == '\\' && marker != '`' {
			i++
			continue
		}
		if chars[i] == marker {
			length := markdownCitationRun(chars, i, end)
			if length == run {
				return i
			}
			i += length - 1
		}
	}
	return -1
}

func markdownCitationTableDivider(line string) bool {
	if !strings.ContainsRune(line, '|') {
		return false
	}
	line = strings.Trim(strings.TrimSpace(line), "|")
	for _, cell := range strings.Split(line, "|") {
		cell = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(cell), ":"), ":")
		if len(cell) < 3 || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func markdownCitationWord(char rune) bool { return unicode.IsLetter(char) || unicode.IsNumber(char) }

// This set is shared with the frontend, including non-breaking spaces and BOM.
func markdownCitationSpace(char rune) bool { return unicode.IsSpace(char) || char == '\ufeff' }
