import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'
import postcss from 'postcss'

const files = ['EntryWorkbench.vue', 'EntityGraph.vue', 'EntityRelations.vue']
const sheets = [] as string[]
for (const file of files) {
  const source = readFileSync(new URL(file, import.meta.url), 'utf8')
  for (const style of parse(source).descriptor.styles) {
    const result = await compileStyleAsync({ source: style.content, filename: file, id: 'data-v-entity', scoped: true })
    assert.deepEqual(result.errors, [])
    sheets.push(result.code)
  }
}
function value(selector: string, property: string) {
  let value = ''
  for (const sheet of sheets) postcss.parse(sheet).walkRules(rule => {
    if (rule.parent?.type === 'atrule') return
    if (rule.selectors.some(item => item.replaceAll('[data-v-entity]', '') === selector)) rule.walkDecls(property, item => { value = item.value })
  })
  return value
}

test('entity page uses the mockup grid and an inverted active entity with readable secondary text', () => {
  assert.equal(value('.entry-workbench', 'grid-template-columns'), '304px minmax(0, 1fr)')
  assert.equal(value('.entity-items button[aria-current="true"]', 'background'), 'var(--text)')
  assert.equal(value('.entity-items button[aria-current="true"] small', 'color'), 'var(--bg)')
  assert.equal(value('.entity-items button:hover:not([aria-current="true"]) small', 'color'), 'var(--on-select)')
  assert.equal(value('.entry-workbench.is-empty', 'grid-template-columns'), 'minmax(0, 1fr)')
})

test('entity pages leave hidden panels intact and replace legacy feedback colors', () => {
  assert.equal(value('.entity-detail .follow-error', 'color'), 'var(--danger)')
  for (const file of files) {
    const source = readFileSync(new URL(file, import.meta.url), 'utf8')
    assert.doesNotMatch(source, /--td-|MessagePlugin|DialogPlugin|<t-/)
    assert.doesNotMatch(source, /\.rq-tabs__panel\s*\{[^}]*display:/)
  }
})

test('all focused relationship row controls use the selection foreground', () => {
  assert.equal(value('tbody tr:hover .rq-button', 'color'), 'var(--on-select)')
  assert.equal(value('tbody tr:focus-within .rq-button', 'color'), 'var(--on-select)')
})

test('related materials keep readable ID, type and date columns in a horizontally scrolling table', () => {
  assert.equal(value('.entity-table-scroll', 'overflow-x'), 'auto')
  assert.equal(value('.entity-articles table', 'min-width'), '680px')
  assert.equal(value('.entity-articles td:first-child', 'min-width'), '104px')
  assert.equal(value('.entity-articles td:first-child', 'white-space'), 'nowrap')
  assert.equal(value('.entity-articles td:nth-child(3)', 'min-width'), '104px')
  assert.equal(value('.entity-articles td:nth-child(3)', 'white-space'), 'nowrap')
  assert.equal(value('.entity-articles td:last-child', 'min-width'), '168px')
})
