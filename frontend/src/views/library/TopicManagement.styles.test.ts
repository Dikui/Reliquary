import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'
import postcss from 'postcss'

const source = readFileSync(new URL('./TopicManagement.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const result = await compileStyleAsync({ source: descriptor.styles[0]!.content, filename: 'TopicManagement.vue', id: 'data-v-topics', scoped: true })
assert.deepEqual(result.errors, [])
const sheet = postcss.parse(result.code)
function value(selector: string, property: string) {
  let value = ''
  sheet.walkRules(rule => {
    if (rule.parent?.type === 'atrule') return
    if (rule.selectors.some(item => item.replaceAll('[data-v-topics]', '') === selector)) rule.walkDecls(property, declaration => { value = declaration.value })
  })
  return value
}

test('topics match the mockup columns and remove the empty detail allocation', () => {
  assert.equal(value('.topic-page', 'grid-template-columns'), '304px minmax(0, 1fr)')
  assert.equal(value('.topic-page.is-empty', 'grid-template-columns'), 'minmax(0, 1fr)')
  assert.equal(value('.topic-list button[aria-current="true"]', 'background'), 'var(--text)')
  assert.equal(value('.topic-list button[aria-current="true"] small', 'color'), 'var(--bg)')
  assert.match(result.code, /@media\s*\(max-width:\s*900px\)/)
  assert.equal(value('.topic-progress-bar i', 'background'), 'var(--text)')
  assert.equal(value('.topic-progress-bar i.failed', 'background'), 'var(--danger)')
})

test('focused rows and legacy follow errors keep the Swiss palette, without overriding hidden', () => {
  for (const state of ['hover', 'focus-visible']) {
    assert.equal(value(`.topic-list button:${state}:not([aria-current="true"]) small`, 'color'), 'var(--on-select)')
  }
  assert.equal(value('tbody tr:focus-within .topic-link', 'color'), 'var(--on-select)')
  assert.equal(value('tbody tr:hover .topic-link', 'color'), 'var(--on-select)')
  assert.equal(value('.topic-detail .follow-error', 'color'), 'var(--danger)')
  assert.doesNotMatch(source, /--td-|MessagePlugin|DialogPlugin|<t-|\[hidden\]/)
})

test('material columns have readable minimum sizes and local horizontal scrolling', () => {
  assert.equal(value('.topic-table-scroll', 'overflow-x'), 'auto')
  assert.equal(value('table', 'min-width'), '760px')
  assert.equal(value('td:first-child', 'min-width'), '96px')
  assert.equal(value('td:first-child', 'white-space'), 'nowrap')
  assert.equal(value('td:nth-child(2)', 'min-width'), '220px')
  assert.equal(value('td:nth-child(4)', 'white-space'), 'nowrap')
  assert.equal(value('td:last-child', 'min-width'), '168px')
  assert.equal(value('td:last-child', 'white-space'), 'nowrap')
})


test('topic geometry uses the 16px design baseline independently of the app root font', () => {
  assert.equal(value('.topic-detail', 'max-width'), '1024px')
  assert.equal(value('.topic-progress-bar', 'height'), 'var(--space-3)')
  assert.equal(value('h3', 'font-size'), 'var(--text-md)')
  assert.doesNotMatch(descriptor.styles[0]!.content, /\b[\d.]+rem\b/)
})

test('material titles are bold and tags inherit readable focused-row colors', () => {
  assert.equal(value('tbody .topic-link', 'font-weight'), '700')
  for (const state of ['hover', 'focus-within']) {
    assert.equal(value(`tbody tr:${state} .rq-tag`, 'color'), 'var(--on-select)')
    assert.equal(value(`tbody tr:${state} .rq-tag`, 'background'), 'transparent')
  }
})
