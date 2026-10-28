import assert from 'node:assert/strict'
import test from 'node:test'
import { compiledSFCStyleRules, styleDeclarationsAtWidth, type CompiledStyleRule } from '../utils/sfcStyleTestHelpers'

const palette = await compiledSFCStyleRules(new URL('./GlobalCommandPalette.vue', import.meta.url))
const items = await compiledSFCStyleRules(new URL('./GlobalCommandPalette/ResultItem.vue', import.meta.url))
const retrieval = await compiledSFCStyleRules(new URL('../views/settings/RetrievalSettings.vue', import.meta.url))
// Child styles intentionally come last, the order that exposed earlier migrations' overrides.
const rules = [...palette, ...items,
  ...await compiledSFCStyleRules(new URL('./EntryUpdateNotice.vue', import.meta.url)),
  ...await compiledSFCStyleRules(new URL('./SearchImage.vue', import.meta.url))]

function computed(path: string[], property: string) {
  let winner = { weight: -1, value: '' }
  for (const rule of rules) {
    if (rule.media.length || rule.containers.length || /[:>+~]/.test(rule.selector)) continue
    const parts = rule.selector.split(/\s+/)
    let cursor = path.length - 1, matches = true
    for (let i = parts.length - 1; i >= 0; i--) {
      const part = parts[i]!, tag = part.match(/^[\w-]+/)?.[0]
      const classes = [...part.matchAll(/\.([\w-]+)/g)].map(match => match[1]!)
      const selected = part.includes('[aria-selected="true"]')
      const match = (node: string) => (!tag || node.split('.')[0] === tag) && classes.every(name => node.split('.').includes(name)) && (!selected || node.includes('.selected'))
      if (i === parts.length - 1 && !match(path[cursor]!)) { matches = false; break }
      while (cursor >= 0 && !match(path[cursor]!)) cursor--
      if (cursor < 0) { matches = false; break }
      cursor--
    }
    if (!matches || !rule.declarations[property]) continue
    const weight = (rule.selector.match(/[.\[]/g)?.length || 0) * 100 + parts.filter(part => /^[a-z]/.test(part)).length
    if (weight >= winner.weight) winner = { weight, value: rule.declarations[property]! }
  }
  return winner.value
}

test('palette sizing follows the 640px mockup and keeps results scrollable at narrow widths', () => {
  for (const width of [390, 1440]) {
    const dialog = styleDeclarationsAtWidth(palette, '.rq-cmdk-dialog', width)
    assert.equal(dialog.width, 'min(640px, calc(100vw - 32px))')
    assert.equal(dialog.top, width === 390 ? '16px' : '12vh')
    assert.equal(styleDeclarationsAtWidth(palette, '.rq-cmdk__results', width).overflow, 'auto')
  }
  for (const rule of [...palette, ...items]) {
    for (const value of Object.values(rule.declarations)) assert.doesNotMatch(value, /\drem/)
    if (!rule.selector.includes('[hidden]')) assert.doesNotMatch(rule.selector, /^\*$/)
  }
  assert.equal(styleDeclarationsAtWidth(items, '.rq-cmdk-item', 1440)['border-radius'], '0')
})

test('the full focused row and all descendant text win over late legacy child styles', () => {
  const row = ['div.rq-cmdk', 'div.rq-cmdk-item.selected']
  assert.equal(computed(row, 'background'), 'var(--select)')
  for (const path of [
    [...row, 'div.rq-cmdk-item__meta', 'span.rq-cmdk__id'],
    [...row, 'div.rq-cmdk-item__subtitle', 'span.entry-update-notice', 'a'],
    [...row, 'div.rq-cmdk-item__subtitle', 'span.search-image', 'span.search-image-caption'],
    [...row, 'div.rq-cmdk-item__subtitle', 'span.search-image', 'span.search-image-state'],
    [...row, 'div.rq-cmdk-item__badges', 'span.rq-tag'],
  ]) assert.equal(computed(path, 'color'), 'var(--on-select)', path.join(' '))
})

for (const width of [390, 1440]) test(`${width}: retrieval range leaves an unwrapped fixed-width numeric output`, () => {
  const range = styleDeclarationsAtWidth(retrieval, '.rq-retrieval-settings .rq-retrieval-control input[type="range"]', width)
  const output = styleDeclarationsAtWidth(retrieval, '.rq-retrieval-settings .rq-retrieval-control output', width)
  assert.equal(range.flex, '1 1 0')
  assert.equal(range.width, '0')
  assert.equal(range['min-width'], '0')
  assert.equal(output.flex, '0 0 56px')
  assert.equal(output['min-width'], '56px')
  assert.equal(output['white-space'], 'nowrap')
  assert.equal(output['overflow-wrap'], 'normal')
  assert.equal(output['word-break'], 'normal')
})
