import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import postcss from 'postcss'

const sheet = readFileSync(new URL('./diagnostics.css', import.meta.url), 'utf8')
const ui = readFileSync(new URL('../../components/ui/ui.css', import.meta.url), 'utf8')
const tokens = readFileSync(new URL('../../styles/tokens.css', import.meta.url), 'utf8')
const css = postcss.parse(ui + '\n' + sheet)
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
        const part = parts[index]!.replace(/\[data-v-diagnostics-test\]/g, '')
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


test('diagnostics use only existing tokens and override old typography at the page boundary', () => {
  const known = new Set([...tokens.matchAll(/(--[\w-]+)\s*:/g)].map(match => match[1]))
  assert.deepEqual([...new Set([...sheet.matchAll(/var\((--[\w-]+)/g)].map(match => match[1]))].filter(name => !known.has(name)), [])
  assert.equal(computed(['header.diagnostics-heading', 'h1'], 'margin'), '0')
  assert.equal(computed(['main.diagnostics-page', 'header.diagnostics-heading', 'h1'], 'font'), 'var(--type-page-weight) var(--type-page-size)/1 var(--font)')
  assert.equal(computed(['main.diagnostics-page', 'h2'], 'margin'), '0')
  assert.equal(computed(['main.diagnostics-page', 'h3'], 'font'), 'var(--type-subsection-weight) var(--type-subsection-size)/var(--leading) var(--font)')
  assert.equal(computed(['div.check-evidence', 'blockquote', 'p'], 'font-size'), 'inherit')
  assert.equal(computed(['div.failure-detail', 'pre'], 'border-radius'), '0')
  assert.equal(computed(['main.diagnostics-page', 'section.rq-empty'], 'padding'), 'var(--space-5) 0')
})

test('narrow diagnostics preserve local table scrolling and mobile header clearance', () => {
  assert.equal(computed(['div.failure-table', 'table.rq-table'], 'min-width'), '1180px')
  assert.equal(computed(['div.failed-entry-table', 'table.rq-table'], 'min-width'), '900px')
  assert.equal(computed(['div.rq-table-wrap'], 'overflow'), 'auto')
  assert.match(sheet, /@media \(max-width: 640px\)[\s\S]*padding-top: var\(--space-5\)/)
  assert.match(sheet, /@media \(max-width: 900px\)[\s\S]*\.checks-layout \{ grid-template-columns: minmax\(0, 1fr\)/)
  assert.doesNotMatch(sheet, /\[hidden\]|\b[\d.]+rem\b|--td-|100vh|overflow: hidden/)
  for (const name of ['IngestFailuresPage', 'ContentChecksPage', 'FailedProcessingEntries']) {
    const source = readFileSync(new URL(`./${name}.vue`, import.meta.url), 'utf8')
    assert.doesNotMatch(source, /<t-|MessagePlugin|DialogPlugin|v-html/)
  }
})

test('focused evidence and table text share the selection foreground in both themes', () => {
  assert.equal(computed(['div.check-evidence', 'blockquote.rq-focus-row'], 'color'), 'var(--on-select)')
  assert.equal(computed(['div.check-evidence', 'blockquote.rq-focus-row'], 'background'), 'var(--select)')
  assert.match(sheet, /\.check-run:not\(:disabled\):hover \*[^}]*color: var\(--on-select\)/)
  assert.match(ui, /:is\(\.rq-focus-row, \.rq-table__row--focused\) \*[^}]*color: var\(--on-select\) !important/)
})

test('failure table reserves width for the action column while keeping its own horizontal scroller', () => {
  for (const selector of ['.failure-table .rq-table th:last-child', '.failure-table .rq-table td:last-child']) {
    const declarations: Record<string, string> = {}
    css.walkRules(rule => {
      if (rule.selectors.includes(selector)) rule.walkDecls(decl => { declarations[decl.prop] = decl.value })
    })
    assert.equal(declarations['min-width'], '160px', selector)
    assert.equal(declarations['white-space'], 'nowrap', selector)
  }
  assert.equal(computed(['main.diagnostics-page'], 'overflow-wrap'), 'anywhere', 'long diagnostic text still wraps')
  assert.equal(computed(['div.rq-table-wrap'], 'overflow'), 'auto')
  assert.equal(computed(['div.failure-table', 'table.rq-table'], 'min-width'), '1180px')
  assert.equal(computed(['main.diagnostics-page', 'div.rq-table-wrap', 'button.rq-button'], 'white-space'), 'nowrap')
  assert.equal(computed(['main.diagnostics-page', 'div.rq-table-wrap', 'button.rq-button', 'span.rq-button__label'], 'overflow-wrap'), 'normal')
})
