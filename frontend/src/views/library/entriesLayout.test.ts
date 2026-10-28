import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../../utils/sfcStyleTestHelpers'

for (const width of [390, 1440]) {
  test(`${width}px: expanded filters leave a reachable material scroller`, async () => {
    const rules = await compiledSFCStyleRules(new URL('./Library.vue', import.meta.url))
    const page = styleDeclarationsAtWidth(rules, '.entries-page', width)
    const material = styleDeclarationsAtWidth(rules, '.doc-scroll-container', width)
    const sections = styleDeclarationsAtWidth(rules, '.entries-page > :not(.doc-scroll-container)', width)
    assert.equal(page['overflow-y'], 'auto', 'the whole page remains scrollable when its controls exceed the viewport')
    assert.ok(Number.parseFloat(material['min-height']!) >= 224, 'the material area retains enough height for readable rows')
    assert.equal(material.flex?.split(/\s+/)[1], '0', 'filters cannot shrink the material scroller to zero')
    assert.equal(material.overflow, 'auto', 'materials retain their paging scroll owner')
    assert.equal(sections['flex-shrink'], '0', 'expanded filters retain their own content height')
  })
}

test('narrow material tables scroll horizontally instead of compressing titles', async () => {
  const rules = await compiledSFCStyleRules(new URL('./components/DocumentListView.vue', import.meta.url))
  const title = styleDeclarationsAtWidth(rules, '.entries-title-link', 390)
  assert.ok(Number.parseFloat(title['min-width']!) >= 240, 'titles retain a readable column on a 390px screen')
  assert.equal(title.display, 'block', 'the minimum width applies to the title content box')
  assert.equal(title['overflow-wrap'], 'break-word')
  const tableStyle = readFileSync(new URL('../../components/ui/ui.css', import.meta.url), 'utf8')
  assert.match(tableStyle, /\.rq-table-wrap\s*\{[^}]*overflow:\s*auto/, 'the shared table owns horizontal overflow')
  assert.match(tableStyle, /\.rq-table-wrap\s*\{[^}]*max-width:\s*100%/, 'wide columns cannot widen the page')
})
