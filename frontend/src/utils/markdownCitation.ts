export interface MarkdownCitationRange { start: number; end: number }
export interface MarkdownCitationSegment { text: string; match: boolean }
interface Unit { char: string; start: number; end: number }
interface NormalizedText { text: string; starts: number[]; ends: number[] }

const isSpace = (char: string) => /[\s\u0085]/u.test(char)
const isWord = (char: string) => /[\p{L}\p{N}]/u.test(char)
const escapable = '!"#$%&\'()*+,-./:;<=>?@[\\]^_`{|}~'

// Keep this normalization in sync with markdown_citation.go. Shared fixtures
// check both implementations, including original source offsets and Unicode.
export function normalizeMarkdownCitation(source: string): NormalizedText {
  const chars = Array.from(source)
  const offsets = [0]
  for (const char of chars) offsets.push(offsets[offsets.length - 1]! + char.length)
  const units: Unit[] = []
  const brackets = new Map<number, number>()
  const labels: number[] = []
  const targets: number[] = []
  for (let index = 0; index < chars.length; index++) {
    if (chars[index] === '\\') { index++; continue }
    const char = chars[index]
    if (char === '\n') { labels.length = 0; targets.length = 0 }
    else if (char === '[') labels.push(index)
    else if (char === '(') targets.push(index)
    else if (char === ']' && labels.length) brackets.set(labels.pop()!, index)
    else if (char === ')' && targets.length) brackets.set(targets.pop()!, index)
  }
  const emit = (index: number) => units.push({ char: chars[index]!, start: offsets[index]!, end: offsets[index + 1]! })
  const runAt = (start: number, end: number) => {
    let index = start + 1
    while (index < end && chars[index] === chars[start]) index++
    return index - start
  }
  const delimiter = (start: number, end: number, marker: string, run: number) => {
    for (let index = start; index < end; index++) {
      if (chars[index] === '\\' && marker !== '`') { index++; continue }
      if (chars[index] === marker) {
        const length = runAt(index, end)
        if (length === run) return index
        index += length - 1
      }
    }
    return -1
  }
  const inline = (start: number, end: number, depth: number, table: boolean) => {
    let runEnd = start
    const inlineRunAt = (index: number) => {
      if (index >= runEnd) runEnd = index + runAt(index, end)
      return runEnd - index
    }
    for (let index = start; index < end;) {
      const char = chars[index]!
      if (char === '\\' && index + 1 < end && escapable.includes(chars[index + 1]!)) {
        emit(index + 1)
        index += 2
        continue
      }
      if (depth < 32 && (char === '[' || (char === '!' && chars[index + 1] === '['))) {
        const label = char === '!' ? index + 1 : index
        const close = brackets.get(label)
        if (close !== undefined && close + 1 < end && chars[close + 1] === '(') {
          const target = brackets.get(close + 1)
          if (target !== undefined && target < end) {
            inline(label + 1, close, depth + 1, false)
            index = target + 1
            continue
          }
        }
      }
      if (char === '`') {
        const run = inlineRunAt(index)
        const close = delimiter(index + run, end, char, run)
        if (close >= 0) {
          for (let cursor = index + run; cursor < close; cursor++) emit(cursor)
          index = close + run
          continue
        }
      }
      if (depth < 32 && '*_~'.includes(char)) {
        const run = inlineRunAt(index)
        if (run <= 3 && (char !== '~' || run === 2) && index + run < end && !isSpace(chars[index + run]!) &&
          (char !== '_' || index === start || !isWord(chars[index - 1]!))) {
          const close = delimiter(index + run, end, char, run)
          if (close > index + run && !isSpace(chars[close - 1]!) &&
            (char !== '_' || close + run === end || !isWord(chars[close + run]!))) {
            inline(index + run, close, depth + 1, table)
            index = close + run
            continue
          }
        }
      }
      if (table && char === '|') units.push({ char: ' ', start: offsets[index]!, end: offsets[index + 1]! })
      else emit(index)
      index++
    }
  }
  const trim = (text: string) => text.replace(/^[\s\u0085]+|[\s\u0085]+$/gu, '')
  for (let start = 0; start < chars.length;) {
    let end = start
    while (end < chars.length && chars[end] !== '\n') end++
    const line = trim(chars.slice(start, end).join(''))
    const divider = line.includes('|') && line.replace(/^\|+|\|+$/gu, '').split('|').every(cell => /^:?-{3,}:?$/u.test(trim(cell)))
    if (!divider) inline(start, end, 0, line.startsWith('|') || line.endsWith('|') || line.includes(' | '))
    if (end < chars.length) emit(end)
    start = end + 1
  }
  const normalized: NormalizedText = { text: '', starts: [], ends: [] }
  const pieces: string[] = []
  let whitespace: Unit | undefined
  for (const unit of units) {
    if (isSpace(unit.char)) {
      if (!whitespace) whitespace = { ...unit }
      whitespace.end = unit.end
      continue
    }
    if (whitespace && pieces.length > 0) {
      pieces.push(' ')
      normalized.starts.push(whitespace.start)
      normalized.ends.push(whitespace.end)
    }
    whitespace = undefined
    pieces.push(unit.char)
    for (let index = 0; index < unit.char.length; index++) {
      normalized.starts.push(unit.start)
      normalized.ends.push(unit.end)
    }
  }
  normalized.text = pieces.join('')
  return normalized
}

export function markdownCitationRanges(body: string, quote: string): MarkdownCitationRange[] {
  const source = normalizeMarkdownCitation(body)
  const needle = normalizeMarkdownCitation(quote).text
  if (!needle) return []
  const matches: MarkdownCitationRange[] = []
  for (let offset = 0; offset < source.text.length && matches.length < 100;) {
    const index = source.text.indexOf(needle, offset)
    if (index < 0) break
    matches.push({ start: source.starts[index]!, end: source.ends[index + needle.length - 1]! })
    offset = index + needle.length
  }
  return matches
}

export function markdownCitationSegments(body: string, quote: string): MarkdownCitationSegment[] {
  let matches = markdownCitationRanges(body, quote)
  // Reports can truncate excerpts with an ellipsis. Only match the exact prefix.
  if (!matches.length && quote.endsWith('…')) matches = markdownCitationRanges(body, quote.slice(0, -1))
  const segments: MarkdownCitationSegment[] = []
  let offset = 0
  for (const match of matches) {
    if (match.start > offset) segments.push({ text: body.slice(offset, match.start), match: false })
    segments.push({ text: body.slice(match.start, match.end), match: true })
    offset = match.end
  }
  if (offset < body.length) segments.push({ text: body.slice(offset), match: false })
  return segments
}
