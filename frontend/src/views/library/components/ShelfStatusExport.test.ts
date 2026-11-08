import assert from 'node:assert/strict'
import test from 'node:test'
import { cloneVNode, defineComponent, h, inject, provide } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'

// Small spec, task 15: the fake Popover replaces browser focus/positioning only;
// the download API and browser file handoff are the observable boundaries.
async function mountExport(t: test.TestContext, download = async (..._args: any[]) => new Blob(['synthetic xlsx'])) {
  const requests: any[] = [], files: string[] = [], released: string[] = []
  const oldDocument = globalThis.document, oldCreate = URL.createObjectURL, oldRevoke = URL.revokeObjectURL
  globalThis.document = { createElement: () => ({ href: '', download: '', click() { files.push(this.download) } }) } as unknown as Document
  URL.createObjectURL = () => 'blob:synthetic'
  URL.revokeObjectURL = url => { released.push(url) }
  const Root = defineComponent({ props: ['open'], emits: ['update:open'], setup: (props, { slots, emit }) => {
    provide('toggleExport', () => emit('update:open', !props.open))
    return () => h('div', slots.default?.())
  } })
  const Trigger = defineComponent({ setup: (_, { slots }) => {
    const toggle = inject<() => void>('toggleExport')!
    return () => cloneVNode(slots.default!()[0]!, { onClick: toggle })
  } })
  const Pass = defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) })
  const Component = loadComponent(new URL('./ShelfStatusExport.vue', import.meta.url), {
    ...entryPresentationTestModules,
    'reka-ui': { PopoverRoot: Root, PopoverTrigger: Trigger, PopoverPortal: Pass, PopoverContent: Pass },
    '@/api/shelf': { exportShelf: (...args: any[]) => { requests.push(args); return download(...args) } },
  })
  const app = renderer.createApp(Component, { libraryId: 'library-a', productLineId: 'line-a', productLineName: '示例产品线', query: { sale_statuses: [{ region: 'north', status: 'GA' }] } })
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } }))
  const root = element('root'); app.mount(root)
  t.after(() => { app.unmount(); globalThis.document = oldDocument; URL.createObjectURL = oldCreate; URL.revokeObjectURL = oldRevoke })
  await settle()
  const button = (label: string) => descendants(root).find(node => node.tag === 'button' && visibleText(node) === label)!
  await click(button('导出 Excel'))
  return { root, requests, files, released, button }
}

test('export downloads the current product line with the applied table filters', async t => {
  const { root, requests, files, released, button } = await mountExport(t)
  assert.match(visibleText(root), /当前页签（示例产品线）/)
  await click(button('导出'))
  assert.equal(requests.length, 1)
  assert.equal(requests[0][0], 'library-a')
  assert.deepEqual(requests[0][1], { product_line_id: 'line-a', query: { sale_statuses: [{ region: 'north', status: 'GA' }] } })
  assert.deepEqual(files, ['product-shelf.xlsx'])
  assert.deepEqual(released, ['blob:synthetic'])
  assert.match(visibleText(root), /下载已开始/)
})

// Small spec, task 15: clearing the filter option exports the complete shelf.
test('all product lines can be exported without the current filters', async t => {
  const { root, requests, button } = await mountExport(t)
  const scope = descendants(root).find(node => node.tag === 'input' && node.props.value === 'all')!
  scope.props.onChange(); await settle()
  const filters = descendants(root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!
  filters.props.onChange({ target: { checked: false } }); await settle()
  await click(button('导出'))
  assert.deepEqual(requests[0][1], { query: {} })
})

// Small spec, task 15: server failures keep the selected export scope for retry.
test('a failed export preserves the options and can be retried', async t => {
  let fail = true
  const { root, requests, files, button } = await mountExport(t, async () => {
    if (fail) throw new Error('Synthetic network failure')
    return new Blob(['synthetic xlsx'])
  })
  descendants(root).find(node => node.tag === 'input' && node.props.value === 'all')!.props.onChange(); await settle()
  await click(button('导出'))
  assert.equal(files.length, 0)
  assert.match(visibleText(root), /Excel 导出失败，请重试/)
  assert.equal(descendants(root).find(node => node.tag === 'input' && node.props.value === 'all')!.props.checked, true)
  fail = false; await click(button('导出'))
  assert.equal(requests.length, 2)
  assert.equal(files.length, 1)
  assert.doesNotMatch(visibleText(root), /Excel 导出失败/)
})

// Small spec, task 15: duplicate activation cannot start parallel downloads;
// cancelling aborts the request and a late result cannot download a file.
test('a pending export prevents duplicates and cancellation suppresses late downloads', async t => {
  let finish: (blob: Blob) => void = () => {}
  const { root, requests, files, button } = await mountExport(t, () => new Promise(resolve => { finish = resolve }))
  await click(button('导出'))
  assert.equal(button('导出').props.disabled, true)
  assert.match(visibleText(root), /正在生成 Excel/)
  await click(button('导出'))
  assert.equal(requests.length, 1)
  await click(button('取消'))
  assert.equal(requests[0][2].aborted, true)
  finish(new Blob(['late result'])); await settle()
  assert.equal(files.length, 0)
  assert.doesNotMatch(visibleText(root), /下载已开始/)
})
