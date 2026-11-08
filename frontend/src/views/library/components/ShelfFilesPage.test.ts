import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import { defineComponent, h } from 'vue'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'

// Small spec, task 20 / ADR 0024. HTTP is replaced; real page controls render.
const entity = { id: 'file', kind: 'document_file', canonical_name: ' 原件.pdf ', revision: 4 }
const ui = { ...entryPresentationTestModules['@/components/ui'] as object, Tabs: defineComponent({ props: ['modelValue', 'items'], emits: ['update:modelValue'], setup: (p, { slots, emit }) => () => h('div', [h('nav', p.items.map((item: any) => h('button', { onClick: () => emit('update:modelValue', item.value) }, item.label))), slots[p.modelValue]?.()]) }) }
async function mount(t: test.TestContext, fail = false) {
  const requests: any[] = [], navigation: any[] = []
  const component = loadComponent(new URL('./ShelfFilesPage.vue', import.meta.url), {
    ...entryPresentationTestModules, '@/components/ui': ui,
    'vue-router': { useRoute: () => ({ query: {} }), useRouter: () => ({ replace: (value: unknown) => navigation.push(value) }) },
    '@/api/shelf-files': { listShelfFiles: async (_: string, input: any) => { requests.push(input); if (fail) throw new Error('offline'); return { success: true, data: { items: [{ entity, document: { id: 'version', version_label: ' 原版 A ', stage: 'draft', parse_status: 'failed', occurred_at: '2026-11-07', is_current: true } }], total: 1 } } } },
    './ShelfDocumentEditor.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
    './ShelfFilePage.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
  })
  const app = renderer.createApp(component, { library: { id: 'shelf', shelf_config: {} } }); app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } }))
  const root = element('root'); app.mount(root); t.after(() => app.unmount()); await settle()
  return { root, requests, navigation }
}
test('资料文件列表展示原名、版本和失败状态，并将历史筛选交给服务端', async t => {
  const { root, requests } = await mount(t)
  assert.match(visibleText(root), /原件.pdf/); assert.match(visibleText(root), /原版 A/); assert.match(visibleText(root), /解析失败/)
  const check = descendants(root).find(n => n.tag === 'input' && n.props.type === 'checkbox')!
  check.props.onChange({ target: { checked: true } }); await settle()
  assert.equal(requests.at(-1).include_history, true)
  await click(descendants(root).find(n => n.tag === 'button' && visibleText(n).includes('证书报告'))!)
  assert.equal(requests.at(-1).group, 'certificates')
})
test('资料文件读取失败保留重试入口', async t => {
  const { root } = await mount(t, true)
  assert.match(visibleText(root), /读取失败/)
  assert.ok(descendants(root).some(n => n.tag === 'button' && visibleText(n) === '重试'))
})
