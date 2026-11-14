import assert from 'node:assert/strict'
import { shelfConditionText } from '../../../utils/productShelf'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import type { LibraryCatalogItem } from '../../../api/library'
import type { ShelfNode, ShelfQueryItem } from '../../../api/shelf'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'

const entity = (id: string, kind: string, name = id) => ({ id, kind, canonical_name: name, library_id: 'library-a', revision: 3, entity_type: 'product' as const, space_id: 1, created_at: '', updated_at: '' })
const node = (id: string, kind: string, parent?: string): ShelfNode => ({ entity: entity(id, kind), facts: parent ? [{ entity_id: id, field: 'belongs_to', value: { object_id: parent } }] : [], as_of: '2026-11-07T00:00:00Z', recorded: true })
const model = (id: string, parent: string, region: string): ShelfQueryItem => ({ ...node(id, 'model', parent), partial_options: false, matching_conditions: [], shelf: { sale_statuses: [{ region, status: 'GA', planned_eom: true, notes: 'Synthetic note', raw: 'Customer spelling', conditions: [] }], composition: [] } })
const library = { id: 'library-a', name: 'Test shelf', shelf_enabled: true, shelf_config: { regions: [{ key: 'north', name: 'North' }, { key: 'south', name: 'South' }], standards: [{ key: 'standard', name: 'Synthetic standard', aliases: [] }] }, entity_schema: { kinds: [], fields: [{ key: 'energy', name: 'Energy', type: 'number', product_line_ids: ['line-a'], unit: 'kWh', kinds: ['model'] }] } } as unknown as LibraryCatalogItem
const success = (data: unknown) => Promise.resolve({ success: true, data })
const tabs = defineComponent({ props: ['modelValue', 'items', 'label'], emits: ['update:modelValue'], setup: (props, { slots, emit }) => () => h('div', [h('nav', { 'aria-label': props.label }, props.items.map((item: any) => h('button', { role: 'tab', 'aria-selected': props.modelValue === item.value, onClick: () => emit('update:modelValue', item.value) }, item.label))), slots[props.modelValue]?.()]) })
async function mountTable(t: test.TestContext, api: Record<string, unknown> = {}) {
  const requests: any[] = [], navigations: any[] = []
  const data: Record<string, ShelfNode> = { 'line-a': node('line-a', 'product_line'), 'line-b': node('line-b', 'product_line'), 'product-a': node('product-a', 'product', 'platform-a'), 'platform-a': node('platform-a', 'platform', 'line-a'), 'product-b': node('product-b', 'product', 'platform-b'), 'platform-b': node('platform-b', 'platform', 'line-b') }
  const modules = {
    ...entryPresentationTestModules, '@/utils/productShelf': { shelfConditionText },
    '@/components/ui': { ...entryPresentationTestModules['@/components/ui'] as object, Tabs: tabs },
    '@/api/library': {}, '@/api/entities': { listEntities: () => success([entity('line-a', 'product_line'), entity('line-b', 'product_line')]) },
    '@/api/shelf': { SALE_STATUSES: ['GA', 'DEV', 'Planning', 'EOM', '待定'],
      queryShelf: (_: string, input: any) => { requests.push(JSON.parse(JSON.stringify(input))); return success({ items: input.ancestor_id === 'line-b' ? [model('model-b', 'product-b', 'south')] : [model('model-a', 'product-a', 'north')], total: 1, truncated: false, as_of: '2026-11-07T00:00:00Z' }) },
      getShelfNode: (_: string, id: string) => success(data[id]), findShelf: () => success({ items: [], total: 0, truncated: false }), ...api },
    'vue-router': { useRouter: () => ({ push(target: unknown) { navigations.push(target) } }) },
    './ShelfStatusEditor.vue': { __esModule: true, default: defineComponent({ props: ['models', 'region', 'bulk'], setup: props => () => h('section', { role: 'dialog' }, `Editor ${props.models?.[0]?.entity.id} ${props.region}`) }) },
    './ShelfStatusExport.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
  }
  const component = loadComponent(new URL('./ShelfStatusTable.vue', import.meta.url), modules)
  const app = renderer.createApp(component, { library }); app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } }))
  const root = element('root'); app.mount(root); t.after(() => app.unmount()); await settle(); await new Promise<void>(resolve => setImmediate(resolve)); await settle()
  return { root, requests, navigations }
}
// Small spec, task 14: page tests use synthetic HTTP responses as the network boundary.
test('product line tabs show their regions and expandable platform and product groups', async t => {
  const { root } = await mountTable(t)
  assert.match(visibleText(root), /model-a/)
  const headers = () => descendants(root).filter(node => node.tag === 'th').map(visibleText)
  assert.ok(headers().includes('North')); assert.ok(!headers().includes('South'))
  const toggle = descendants(root).find(node => node.tag === 'button' && node.props['aria-expanded'] === true && visibleText(node).includes('platform-a'))!
  await click(toggle); assert.doesNotMatch(visibleText(root), /model-a/)
  await click(toggle); assert.match(visibleText(root), /model-a/)
  const productToggle = descendants(root).find(node => node.tag === 'button' && node.props['aria-expanded'] === true && visibleText(node).includes('product-a'))!
  await click(productToggle); assert.doesNotMatch(visibleText(root), /model-a/)
  await click(productToggle); assert.match(visibleText(root), /model-a/)
  await click(descendants(root).find(node => node.props.role === 'tab' && visibleText(node).includes('line-b'))!)
  assert.match(visibleText(root), /model-b/); assert.ok(headers().includes('South')); assert.ok(!headers().includes('North'))
})
test('platform rows and the current product line open their node pages', async t => {
  const { root, navigations } = await mountTable(t)
  await click(descendants(root).find(node => node.tag === 'button' && node.props['aria-label'] === '查看 platform-a')!)
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === '查看产品线')!)
  assert.deepEqual(navigations.map(item => item.query.entity_id), ['platform-a', 'line-a'])
})

// Small spec, task 14: filters are submitted to the shared query service as one conjunction.
test('status region parameter certification and document gap filters use the shared query', async t => {
  const { root, requests } = await mountTable(t)
  async function change(label: string, value: string | boolean) {
    const control = descendants(root).find(node => node.props['aria-label'] === label)!
    assert.ok(control, label)
    const event = { target: { value, checked: value } }
    ;(control.props.onChange || control.props.onInput)(event); await settle()
  }
  await change('状态', 'DEV'); await change('区域', 'north')
  await change('参数', 'energy'); await change('下限', '9'); await change('上限', '54')
  await change('认证标准', 'standard'); await change('资料有缺口', true)
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === '应用筛选')!)
  // Cells render sale statuses, which only full details carry.
  assert.equal(requests.at(-1).detail, 'full')
  assert.deepEqual(requests.at(-1).sale_statuses, [{ region: 'north', status: 'DEV' }])
  assert.deepEqual(requests.at(-1).parameters, [{ field: 'energy', min: 9, max: 54 }])
  assert.deepEqual(requests.at(-1).certification, { standard: 'standard' })
  assert.equal(requests.at(-1).document_gaps, true)
  assert.equal(requests.at(-1).ancestor_id, 'line-a')
  assert.ok(descendants(root).some(node => node.tag === 'th' && visibleText(node) === 'North'), 'filtering retains the product line columns')
})

// Small spec, task 14: same names retain category labels; aliases are resolved by find.
test('name and alias search shows every category and keeps the chosen node identity', async t => {
  const searches: string[] = []
  const { root, navigations } = await mountTable(t, { findShelf: (_: string, q: string) => { searches.push(q); return success({ items: [{ ...entity('platform-a', 'platform', 'Shared name'), kind_label: '产品平台' }, { ...entity('product-a', 'product', 'Shared name'), kind_label: '产品名称' }], total: 2, truncated: false }) } })
  const input = descendants(root).find(node => node.props['aria-label'] === '搜索名称、别名或代码')!
  assert.ok(input)
  input.props.onInput({ target: { value: 'Alias' } }); await settle()
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === '搜索')!)
  assert.deepEqual(searches, ['Alias'])
  assert.match(visibleText(root), /产品平台Shared name/)
  assert.match(visibleText(root), /产品名称Shared name/)
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node).includes('产品名称Shared name'))!)
  assert.deepEqual(navigations, [{ path: '/platform/entities', query: { libraries: 'library-a', entity_id: 'product-a' } }])
})
// Small spec, task 14: a status cell opens the edit flow for exactly its model and region.
test('a cell opens its model and region editor', async t => {
  const { root } = await mountTable(t)
  await click(descendants(root).find(node => node.tag === 'button' && node.props['aria-label'] === '编辑 model-a · North')!)
  assert.match(visibleText(root), /Editor model-a north/)
})

// Small spec, task 14: a table must not mistake one API page for the whole product line.
test('all query pages are displayed with the first page business time', async t => {
  const inputs: any[] = []
  const { root } = await mountTable(t, { queryShelf: (_: string, input: any) => {
    inputs.push(JSON.parse(JSON.stringify(input)))
    return success({ items: [model(input.offset ? 'model-next' : 'model-a', 'product-a', 'north')], total: 2, truncated: true, as_of: '2026-11-07T00:00:00Z' })
  } })
  assert.match(visibleText(root), /model-next/)
  assert.match(visibleText(root), /2 个型号/)
  assert.equal(inputs.length, 2)
  assert.equal(inputs[1].offset, 1)
  assert.equal(inputs[1].as_of, '2026-11-07T00:00:00Z')
})
// Small regression, task 14: navigating to a new product line invalidates outstanding results.
test('an older product line request cannot replace the currently selected line', async t => {
  let finishOld: (value: unknown) => void = () => {}
  const { root } = await mountTable(t, { queryShelf: (_: string, input: any) => input.ancestor_id === 'line-a' ? new Promise(resolve => { finishOld = resolve }) : success({ items: [model('model-b', 'product-b', 'south')], total: 1, truncated: false }) })
  await click(descendants(root).find(node => node.props.role === 'tab' && visibleText(node).includes('line-b'))!)
  finishOld({ success: true, data: { items: [model('model-a', 'product-a', 'north')], total: 1, truncated: false } }); await settle()
  assert.match(visibleText(root), /model-b/)
  assert.doesNotMatch(visibleText(root), /model-a/)
})
// Small spec, task 14: deleting a filter restores the unfiltered rows and keeps region columns.
test('filtered empty results keep columns and a removable condition', async t => {
  const { root } = await mountTable(t, { queryShelf: (_: string, input: any) => success({ items: input.sale_statuses ? [] : [model('model-a','product-a','north')], total: input.sale_statuses ? 0 : 1, truncated: false }) })
  descendants(root).find(node => node.props['aria-label'] === '状态')!.props.onChange({ target: { value: 'EOM' } }); await settle()
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === '应用筛选')!)
  assert.doesNotMatch(visibleText(root), /model-a/)
  assert.ok(descendants(root).some(node => node.tag === 'th' && visibleText(node) === 'North'))
  await click(descendants(root).find(node => node.tag === 'button' && node.props['aria-label'] === '移除筛选：任一区域 · EOM')!)
  assert.match(visibleText(root), /model-a/)
})
