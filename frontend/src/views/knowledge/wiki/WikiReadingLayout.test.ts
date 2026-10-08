import assert from 'node:assert/strict'
import test from 'node:test'
import { h, nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import zhCN from '../../../i18n/locales/zh-CN'
import enUS from '../../../i18n/locales/en-US'
import { click, descendants, element, hasClass, loadComponent, mockViewport, registerPrimitives, renderer, visibleText } from './wikiComponentTestHarness'

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale} at 390px keeps the reader visible and the directory closed, with operable source actions`, async () => {
    const viewport = mockViewport(390)
    const Layout = loadComponent(new URL('./WikiReadingLayout.vue', import.meta.url), {})
    let ignores = 0
    let regenerations = 0
    const Notice = loadComponent(new URL('./WikiSourceChanges.vue', import.meta.url), {
      'tdesign-vue-next': { MessagePlugin: { success() {}, error() {} } },
      '@/api/wiki': {
        ignoreWikiSourceChanges: async () => { ignores++; return { data: { ...page, source_changes: {} } } },
        regenerateWikiSourceChanges: async () => { regenerations++; return {} },
      },
    })
    const page = { id: 'page-a', knowledge_base_id: 'kb-a', slug: 'concept/a', title: 'Page A', source_changes: { 'chunk-a': '2026-10-08T06:20:15Z' } }
    const state = reactive({ selectionKey: 'page-a', page })
    const app = renderer.createApp({ render: () => h(Layout, { selectionKey: state.selectionKey }, {
      directory: () => h('aside', { class: 'wiki-sidebar' }, [h('button', { 'data-select-page': true, onClick: () => { state.selectionKey = 'page-b' } }, 'Page B')]),
      default: () => h('main', { class: 'wiki-content' }, h(Notice, { page: state.page, kbId: 'kb-a', canEdit: true, onUpdated: (updated: typeof page) => { state.page = updated } })),
    }) })
    app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } }))
    registerPrimitives(app)
    const root = element('root')
    app.mount(root)
    const copy = (locale === 'zh-CN' ? zhCN : enUS).knowledgeEditor.wikiBrowser
    const button = (text: string) => descendants(root).find(node => node.tag === 'button' && visibleText(node).trim() === text)!
    const open = () => click(button(copy.openDirectory))
    assert.ok(descendants(root).some(node => hasClass(node, 'wiki-reading-layout--narrow')))
    assert.equal(descendants(root).some(node => hasClass(node, 'wiki-sidebar')), false, 'a closed mobile directory takes no reading width')
    assert.ok(descendants(root).some(node => node.tag === 'main'))
    assert.ok(visibleText(root).includes(copy.sourceChangesTitle))
    assert.equal(button(copy.sourceChangesRegenerate).props.disabled, false)
    assert.equal(button(copy.sourceChangesIgnore).props.disabled, false)
    await open()
    const drawer = descendants(root).find(node => node.props.role === 'dialog')!
    assert.equal(drawer.props['aria-label'], copy.directoryTitle)
    assert.equal(drawer.props['data-size'], 'min(320px, 100vw)')
    assert.ok(descendants(root).some(node => hasClass(node, 'wiki-sidebar')))
    await click(descendants(root).find(node => node.props['data-close-drawer'])!)
    assert.equal(descendants(root).some(node => hasClass(node, 'wiki-sidebar')), false)
    await open()
    await click(descendants(root).find(node => node.props['data-select-page'])!)
    assert.equal(descendants(root).some(node => node.props.role === 'dialog'), false, 'selecting a page closes the directory')
    const confirmation = button(copy.sourceChangesRegenerate).parent!
    await click(confirmation)
    await click(descendants(root).find(node => node.props['data-confirm'])!)
    assert.equal(regenerations, 1)
    state.page = { ...page }
    await nextTick()
    await click(button(copy.sourceChangesIgnore))
    assert.equal(ignores, 1)
    assert.equal(visibleText(root).includes(copy.sourceChangesTitle), false)
    app.unmount()
    assert.equal(viewport.listenerCount(), 0)
    viewport.restore()
  })
}

test('desktop keeps both columns and resizing to 390px closes the directory', async () => {
  const viewport = mockViewport(1440)
  const Layout = loadComponent(new URL('./WikiReadingLayout.vue', import.meta.url), {})
  const app = renderer.createApp({ render: () => h(Layout, { selectionKey: 'page-a' }, {
    directory: () => h('aside', { class: 'wiki-sidebar' }, 'Directory'),
    default: () => h('main', 'Reader'),
  }) })
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': enUS } }))
  registerPrimitives(app)
  const root = element('root')
  app.mount(root)
  assert.ok(descendants(root).some(node => hasClass(node, 'wiki-sidebar')))
  assert.equal(visibleText(root).includes('Open directory'), false)
  viewport.resize(390)
  await nextTick()
  assert.equal(descendants(root).some(node => hasClass(node, 'wiki-sidebar')), false)
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node).trim() === 'Open directory')!)
  assert.ok(descendants(root).some(node => node.props.role === 'dialog'))
  viewport.resize(1440)
  await nextTick()
  assert.equal(descendants(root).some(node => node.props.role === 'dialog'), false)
  assert.ok(descendants(root).some(node => hasClass(node, 'wiki-sidebar')))
  viewport.resize(390)
  await nextTick()
  assert.equal(descendants(root).some(node => hasClass(node, 'wiki-sidebar')), false)
  assert.ok(visibleText(root).includes('Reader'))
  app.unmount()
  viewport.restore()
})
