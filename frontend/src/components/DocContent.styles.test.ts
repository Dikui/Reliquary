import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'
import postcss from 'postcss'

// Inspect the CSS after Less expansion and Vue scoping, in browser source order.
// These fixtures only match descendant selectors made of tags, classes and scope attrs;
// interactive selectors cannot affect the stationary Markdown elements below.
const filename = new URL('./DocContent.vue', import.meta.url).pathname
const descriptor = parse(readFileSync(filename, 'utf8'), { filename }).descriptor
const sheets = await Promise.all(descriptor.styles.map(style => compileStyleAsync({
  filename, source: style.content, id: 'data-v-reading-test', scoped: style.scoped,
  preprocessLang: style.lang as 'less' | undefined,
})))
for (const sheet of sheets) assert.deepEqual(sheet.errors, [])
const css = postcss.parse(sheets.map(sheet => sheet.code).join('\n'))

function computed(path: string[], property: string) {
  let winner = { weight: -1, value: '' }
  css.walkRules(rule => {
    // Screen fixtures exclude print and responsive rules.
    if (rule.parent?.type === 'atrule') return
    for (const selector of rule.selectors) {
      if (/[:>+~*]/.test(selector)) continue
      const parts = selector.trim().split(/\s+/)
      let cursor = path.length - 1
      let matches = true
      for (let index = parts.length - 1; index >= 0; index--) {
        const part = parts[index]!.replace(/\[data-v-reading-test\]/g, '')
        if (part.includes('[')) { matches = false; break }
        const tag = part.match(/^[\w-]+/)?.[0]
        const classes = [...part.matchAll(/\.([\w-]+)/g)].map(match => match[1]!)
        const match = (node: string) => (!tag || node.split('.')[0] === tag) && classes.every(name => node.split('.').includes(name))
        if (index === parts.length - 1 && !match(path[cursor]!)) { matches = false; break }
        while (cursor >= 0 && !match(path[cursor]!)) cursor--
        if (cursor < 0) { matches = false; break }
        cursor--
      }
      if (!matches) continue
      const weight = (selector.match(/[.\[]/g)?.length || 0) * 100 + parts.filter(part => /^[a-z]/.test(part)).length
      rule.walkDecls(property, declaration => {
        const priority = weight + (declaration.important ? 10000 : 0)
        if (priority >= winner.weight) winner = { weight: priority, value: declaration.value }
      })
    }
  })
  return winner.value
}

function reading(...children: string[]) { return ['div.doc-reading', 'div.md-content', ...children] }

test('reading typography uses the shared tokens after SFC compilation', () => {
  assert.equal(computed(['div.doc-reading'], 'font-size'), '17px')
  assert.equal(computed(['div.doc-reading'], 'line-height'), '1.85')
  for (const node of ['p', 'blockquote', 'ul', 'ol', 'dl', 'table', 'summary']) {
    assert.equal(computed(reading(node), 'font-size'), 'inherit', node)
    assert.equal(computed(reading(node), 'line-height'), 'inherit', node)
  }
  assert.equal(computed(reading('h2'), 'font-size'), 'var(--text-lg)')
  assert.equal(computed(reading('h3'), 'font-weight'), '800')
  assert.equal(computed(reading('h2', 'a'), 'color'), 'inherit')
})

test('reading media, quotations, code and tables use square neutral surfaces', () => {
  for (const node of ['div.mermaid', 'figure', 'img', 'blockquote', 'div.code-block-wrapper', 'pre.code-block-pre', 'code', 'table']) {
    assert.equal(computed(reading(node), 'border-radius'), '0', node)
    assert.equal(computed(reading(node), 'box-shadow'), 'none', node)
  }
  assert.equal(computed(reading('blockquote'), 'border-left'), '1px solid var(--rule)')
  assert.equal(computed(reading('blockquote'), 'background'), 'var(--surface-soft)')
  assert.equal(computed(reading('code'), 'background'), 'var(--surface-soft)')
  assert.equal(computed(reading('code'), 'font-family'), 'var(--mono)')
  assert.equal(computed(reading('code'), 'padding'), '2px 4px')
  assert.equal(computed(reading('div.code-block-wrapper', 'pre.code-block-pre'), 'padding'), '16px')
  assert.equal(computed(reading('div.code-block-wrapper', 'pre.code-block-pre', 'code'), 'white-space'), 'pre')
  assert.equal(computed(reading('table', 'tbody', 'tr', 'td'), 'border'), '1px solid var(--border)')
  assert.equal(computed(reading('table', 'tbody', 'tr', 'td'), 'padding'), '8px')
  assert.equal(computed(reading('table', 'thead'), 'background'), 'var(--surface-soft)')
  assert.equal(computed(reading('table', 'tbody', 'tr'), 'border-top'), '1px solid var(--border)')
})
