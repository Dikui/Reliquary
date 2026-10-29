import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { parse, NodeTypes } from '@vue/compiler-dom'
import { parse as parseSFC } from '@vue/compiler-sfc'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from './utils/sfcStyleTestHelpers'

for (const width of [1440, 1280, 1024, 390]) test(`${width}: the materials filters keep their own responsive grid`, async () => {
  const rules = await compiledSFCStyleRules(new URL('./views/library/Library.vue', import.meta.url))
  const filters = styleDeclarationsAtWidth(rules, '.entries-filters__grid', width)
  assert.equal(filters.display, 'grid')
  assert.equal(filters['grid-template-columns'], 'repeat(auto-fit, minmax(200px, 1fr))')
  const scope = await compiledSFCStyleRules(new URL('./components/LibraryScopeSelect.vue', import.meta.url))
  assert.equal(styleDeclarationsAtWidth(scope, '.library-scope', width)['min-width'], '0')
  assert.equal(styleDeclarationsAtWidth(scope, '.library-scope', width).width, '100%')
})
function layer(path: string, tag: string) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const { descriptor } = parseSFC(source)
  const nodes: any[] = []
  function visit(node: any) { if (node.type === NodeTypes.ELEMENT && node.tag === tag) nodes.push(node); node.children?.forEach(visit) }
  visit(parse(descriptor.template!.content))
  assert.ok(nodes.length)
  return nodes.map(node => {
    const prop = node.props.find((item: any) => item.type === NodeTypes.DIRECTIVE && item.name === 'bind' && ['z-index', 'zIndex', 'layer', 'preview-layer'].includes(item.arg?.content))
    assert.ok(prop, `${path}: ${tag} has an explicit teleport layer`)
    const value = Number(prop.exp?.content); assert.ok(Number.isFinite(value)); return value
  })
}
test('global search and its image viewer remain above both material drawers and overlays', () => {
  const drawers = layer('./views/library/components/ReparseConfirmDialog.vue', 'Dialog')
  const palette = layer('./components/GlobalCommandPalette.vue', 'Dialog')[0]
  const retrieval = layer('./components/GlobalCommandPalette.vue', 'Drawer')[0]
  const preview = layer('./components/GlobalCommandPalette.vue', 'SearchImage')[0]
  assert.ok(palette > 201, 'The search panel clears default dialogs')
  assert.ok(drawers.every(Number.isFinite))
  assert.ok(retrieval > palette)
  assert.ok(preview > retrieval, 'Thumbnail viewing stays above the search panel')
  const source = readFileSync(new URL('./components/GlobalCommandPalette/ResultItem.vue', import.meta.url), 'utf8')
  assert.match(source, /@click="\$emit\('primary'\)"/)
  assert.match(source, /@keydown\.enter\.self\.prevent(?:\.stop)?=/)
})
