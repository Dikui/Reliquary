import assert from 'node:assert/strict'
import test from 'node:test'
import { marked } from 'marked'
import { markdownToPlainText } from './markdownPlainText'

const cases = [
  ['ATX and setext headings', '# Heading\n\nSubtitle\n===', 'Heading\n\nSubtitle'],
  ['ordered and nested task lists', '- **First**\n  - [x] _Nested_\n- Second\n\n1. Third\n2. Fourth', 'First\nNested\nSecond\n\nThird\nFourth'],
  ['emphasis, strike and quotes', '> **Strong** *emphasis* ~~deleted~~\n> continued', 'Strong emphasis deleted\ncontinued'],
  ['inline, fenced and indented code', '`a < b`\n\n```js\nconst answer = 42\n```\n\n    indented code', 'a < b\n\nconst answer = 42\n\nindented code'],
  ['links with nested formatting and destinations', '[**Useful** link](https://example.com/a(b) "Title") and <https://example.com>', 'Useful link and https://example.com'],
  ['reference links and images', '[Read][page] ![Chart][picture]\n\n[page]: https://example.com\n[picture]: ../../attachments/a/chart.png', 'Read [Image]'],
  ['Markdown images with remote and attachment paths', '![remote](https://example.com/chart(1).png) ![local](../../attachments/a/chart.png)', '[Image] [Image]'],
  ['HTML images and tags with quoted brackets', '<span>before</span> <IMG src="remote.png" alt="a > b"> <img src=local.png/> <b>after</b>', 'before [Image] [Image] after'],
  ['HTML block tags and entities', '<div>Hello &amp; welcome<br>next <strong>line</strong> &#x4e2d;&#25991;</div>', 'Hello & welcome\nnext line 中文'],
  ['tables and horizontal rules', '| Name | Value |\n| --- | --- |\n| **A** | `42` |\n\n---', 'Name\tValue\nA\t42'],
  ['escaped punctuation and comments', '\\*literal\\* <!-- private --> &lt;safe&gt;', '*literal*  <safe>'],
] as const

for (const [name, source, expected] of cases) {
  test(`Markdown excerpt converts ${name} to plain text`, () => {
    assert.equal(markdownToPlainText(source), expected)
  })
}

test('Markdown excerpt uses the supplied localized image placeholder and handles empty text', () => {
  assert.equal(markdownToPlainText('![chart](chart.png) <img src="photo.png">', '[图片]'), '[图片] [图片]')
  for (const input of ['', undefined, null]) assert.equal(markdownToPlainText(input), '')
})

test('Markdown excerpt conversion leaves the shared detail renderer and its Markdown intact', () => {
  const source = '# Heading\n\n**bold** and [link](https://example.com)\n\n![chart](chart.png)'
  const before = marked.parse(source)
  assert.equal(markdownToPlainText(source), 'Heading\n\nbold and link\n\n[Image]')
  assert.equal(marked.parse(source), before)
  assert.match(before as string, /<h1>Heading<\/h1>/)
  assert.match(before as string, /<strong>bold<\/strong>/)
  assert.match(before as string, /<img src="chart.png" alt="chart">/)
})
