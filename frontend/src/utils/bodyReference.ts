import type { SourceLocation } from '../types/search'
export function codePointRange(body: string, start: number, end: number) {
  const points = Array.from(body)
  if (!Number.isInteger(start) || !Number.isInteger(end) || start < 0 || end < start || end > points.length) return null
  return { start: points.slice(0, start).join('').length, end: points.slice(0, end).join('').length }
}
// Mirrors the saved-body block rules in internal/types/source_location_compute.go.
export function bodyParagraphs(body: string) {
  const lines = body.match(/[^\n]*\n|[^\n]+$/g) || []
  const blocks: Array<{ start: number; end: number; text: string }> = []
  let offset = 0, fence = '', boundary = true
  const setext = /^ {0,3}(=+|-+)\s*$/
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i], text = line.replace(/\r?\n$/, ''), trimmed = text.trim()
    let nextBoundary = false
    if (!fence) {
      const heading = /^ {0,3}(#{1,6})[ \t]+/.test(text)
      const setextTitle = !heading && !!trimmed && i + 1 < lines.length && setext.test(lines[i + 1].trim())
      if (heading || setextTitle) { boundary = true; nextBoundary = !setextTitle }
      if (/^ {0,3}(?:[-+*]|[0-9]+[.)])[ \t]+/.test(text)) boundary = true
      const opening = trimmed.match(/^(`{3,}|~{3,})/)
      if (opening) { fence = opening[1]; boundary = true }
      if (i > 0 && setext.test(text) && lines[i - 1].trim()) nextBoundary = true
    } else if (trimmed.startsWith(fence) && new RegExp(`^[${fence[0]} \\t]+$`).test(trimmed)) { fence = ''; nextBoundary = true }
    if (trimmed && (boundary || !blocks.length)) {
      if (blocks.length) blocks[blocks.length - 1].end = offset
      blocks.push({ start: offset, end: 0, text: '' }); boundary = false
    }
    offset += line.length
    if (nextBoundary || (!trimmed && !fence)) boundary = true
  }
  if (blocks.length) { blocks[0].start = 0; blocks[blocks.length - 1].end = offset }
  return blocks.map(block => ({ ...block, text: body.slice(block.start, block.end) }))
}
export async function bodyHash(body: string) {
  const bytes = new TextEncoder().encode(body)
  const hash = await crypto.subtle.digest('SHA-256', bytes)
  return Array.from(new Uint8Array(hash), value => value.toString(16).padStart(2, '0')).join('')
}
export async function resolveBodyReference(body: string, location: SourceLocation) {
  const matches = !!location.body_hash && await bodyHash(body) === location.body_hash
  return { matches, range: matches ? codePointRange(body, location.start, location.end) : null, paragraph: Math.max(1, location.paragraph || 1) }
}
export function referenceQuery(entryId: string, location?: SourceLocation) {
  return { entry_id: entryId, ...(location ? { reference: JSON.stringify(location) } : {}) }
}
export function parseReference(value: unknown): SourceLocation | undefined {
  try {
    if (typeof value !== 'string') return
    const location = JSON.parse(value)
    if (location && Number.isInteger(location.paragraph) && Number.isInteger(location.start) && Number.isInteger(location.end) && typeof location.body_hash === 'string') return location
  } catch { /* Malformed deep links fall back to full material details. */ }
}
