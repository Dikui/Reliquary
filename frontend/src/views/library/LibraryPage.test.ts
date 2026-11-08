import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import * as vueRouter from 'vue-router'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from './componentTestHarness'

// Small spec, task 14 / ADR 0023: only enabled shelves replace the library landing page.
// Child page stubs replace the document and shelf page boundaries; the real router is used.
async function mountPage(t: test.TestContext, shelf = true, query = '') {
  const router = vueRouter.createRouter({ history: vueRouter.createMemoryHistory(), routes: [{ path: '/platform/entries', component: { render: () => null } }] })
  await router.push(`/platform/entries?libraries=library-a${query}`)
  const ui = {
    Tabs: defineComponent({ props: ['modelValue', 'items'], emits: ['update:modelValue'], setup: (props, { slots, emit }) => () => h('div', [
      h('nav', props.items.map((item: any) => h('button', { role: 'tab', 'aria-selected': props.modelValue === item.value, onClick: () => emit('update:modelValue', item.value) }, item.label))), slots[props.modelValue]?.(),
    ]) }),
    ErrorState: defineComponent({ props: ['title'], setup: props => () => h('p', props.title) }),
    Button: defineComponent({ setup: (_, { slots }) => () => h('button', slots.default?.()) }),
  }
  const component = loadComponent(new URL('./LibraryPage.vue', import.meta.url), {
    'vue-router': vueRouter, 'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) }, '@/components/ui': ui,
    '@/api/library': { listLibraries: async () => ({ success: true, data: [{ id: 'library-a', name: 'Synthetic library', shelf_enabled: shelf }] }) },
    './Library.vue': { __esModule: true, default: defineComponent({ setup: () => () => h('article', 'Original documents') }) },
    './components/ShelfStatusTable.vue': { __esModule: true, default: defineComponent({ props: ['library'], setup: props => () => h('article', `Shelf table ${props.library.id}`) }) },
  })
  const previous = globalThis.window
  globalThis.window = { addEventListener() {}, removeEventListener() {} } as any
  const app = renderer.createApp(component); app.use(router)
  const root = element('root'); app.mount(root); t.after(() => { app.unmount(); globalThis.window = previous }); await settle()
  return { root, router }
}
test('enabled shelf opens its status table and keeps documents in a tab', async t => {
  const { root, router } = await mountPage(t)
  assert.match(visibleText(root), /Shelf table library-a/)
  assert.doesNotMatch(visibleText(root), /Original documents/)
  const docs = descendants(root).find(node => node.tag === 'button' && visibleText(node) === 'shelfStatus.documents')!
  await click(docs); await new Promise<void>(resolve => setImmediate(resolve)); await settle()
  assert.match(visibleText(root), /Original documents/)
  assert.equal(router.currentRoute.value.query.tab, 'documents')
  await router.push('/platform/entries?libraries=library-a'); await settle()
  assert.match(visibleText(root), /Shelf table library-a/)
})
test('ordinary libraries retain the original document page without shelf tabs', async t => {
  const { root } = await mountPage(t, false)
  assert.match(visibleText(root), /Original documents/)
  assert.equal(descendants(root).filter(node => node.props.role === 'tab').length, 0)
})
test('document bookmarks in a shelf reopen documents', async t => {
  const { root } = await mountPage(t, true, '&tab=documents')
  assert.match(visibleText(root), /Original documents/)
})
