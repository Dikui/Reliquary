import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { markdownCitationRanges, markdownCitationSegments, normalizeMarkdownCitation } from './markdownCitation'

const cases: Array<{ name: string; body: string; quote: string; normalized: string; matches: string[] }> = JSON.parse(
  readFileSync(new URL('../../../internal/application/service/testdata/markdown_citations.json', import.meta.url), 'utf8'),
)

for (const example of cases) {
  test(`Markdown citation normalization and highlighting: ${example.name}`, () => {
    assert.equal(normalizeMarkdownCitation(example.body).text, example.normalized)
    const ranges = markdownCitationRanges(example.body, example.quote)
    assert.deepEqual(ranges.map(range => example.body.slice(range.start, range.end)), example.matches)
    const segments = markdownCitationSegments(example.body, example.quote)
    assert.equal(segments.map(segment => segment.text).join(''), example.body)
    assert.deepEqual(segments.filter(segment => segment.match).map(segment => segment.text), example.matches)
  })
}

test('truncated report excerpts locate the exact normalized prefix', () => {
  const body = 'Acme 发布了 **新模型**。支持快速运行。'
  assert.deepEqual(markdownCitationSegments(body, 'Acme 发布了 新模型…'), [
    { text: 'Acme 发布了 **新模型', match: true },
    { text: '**。支持快速运行。', match: false },
  ])
  assert.equal(markdownCitationSegments(body, 'Acme 新模型…').some(segment => segment.match), false)
})

test('formatting across an identifying object name still locates the source', () => {
  const body = 'Acme **Model** 发布。'
  assert.deepEqual(markdownCitationRanges(body, 'Acme Model').map(range => body.slice(range.start, range.end)), ['Acme **Model'])
})

test('long unmatched marker runs preserve literal text without rescanning each suffix', () => {
  for (const marker of ['*', '_', '~', '`']) {
    const body = marker.repeat(100000) + 'Acme'
    assert.equal(normalizeMarkdownCitation(body).text, body)
  }
  assert.equal(normalizeMarkdownCitation('****Acme***').text, '*Acme')
})
