import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { parse, NodeTypes } from '@vue/compiler-dom'
import { parse as parseSFC } from '@vue/compiler-sfc'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from './utils/sfcStyleTestHelpers'

for (const width of [1440, 1280, 1024, 390]) test(`${width}: the materials library selector retains usable width inside its filter row`, async () => {
  const rules = await compiledSFCStyleRules(new URL('./views/library/Library.vue', import.meta.url))
  const selector = styleDeclarationsAtWidth(rules, '.doc-filter-bar__filters > .library-scope', width, { 'doc-card-area': width - 200 })
  const filters = styleDeclarationsAtWidth(rules, '.doc-filter-bar__filters', width, { 'doc-card-area': width - 200 })
  assert.equal(filters.display, 'grid')
  assert.equal(filters['grid-template-columns'], width - 200 <= 420 ? 'repeat(2, minmax(0, 1fr))' : 'repeat(4, minmax(0, 1fr))')
  assert.equal(selector.width, '100%')
  assert.equal(selector['min-width'], '0')
  assert.equal(selector['max-width'], 'none')
  if (width - 200 <= 420) assert.equal(selector['grid-column'], 'span 2')
})
function layer(path: string, tag: string) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const { descriptor } = parseSFC(source)
  const nodes: any[] = []
  function visit(node: any) { if (node.type === NodeTypes.ELEMENT && node.tag === tag) nodes.push(node); node.children?.forEach(visit) }
  visit(parse(descriptor.template!.content))
  assert.ok(nodes.length)
  return nodes.map(node => {
    const prop = node.props.find((item: any) => item.type === NodeTypes.DIRECTIVE && item.name === 'bind' && ['z-index', 'zIndex'].includes(item.arg?.content))
    assert.ok(prop, `${path}: ${tag} has an explicit teleport layer`)
    const value = Number(prop.exp?.content); assert.ok(Number.isFinite(value)); return value
  })
}
test('global search and its image viewer remain above both material drawers and overlays', () => {
  const drawers = layer('./components/DocContent.vue', 't-drawer')
  const palette = layer('./components/GlobalCommandPalette.vue', 't-dialog')[0]
  const retrieval = layer('./components/GlobalCommandPalette.vue', 't-drawer')[0]
  const preview = layer('./components/ImagePreview.vue', 'ImageViewer')[0]
  assert.ok(palette > Math.max(...drawers) + 1, 'The search panel clears drawers and their resize handles')
  assert.ok(retrieval > palette)
  assert.ok(preview > retrieval, 'Thumbnail viewing stays above the search panel')
  const source = readFileSync(new URL('./components/GlobalCommandPalette/ResultItem.vue', import.meta.url), 'utf8')
  assert.match(source, /@click="\$emit\('primary'\)"/)
  assert.match(source, /@keydown\.enter\.self\.prevent=/)
})
