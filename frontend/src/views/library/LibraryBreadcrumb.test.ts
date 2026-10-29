import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import { click, descendants, element, hasClass, loadComponent, registerPrimitives, renderer, settle, visibleText } from './componentTestHarness'

const require = createRequire(import.meta.url)
const vueRouter = require('vue-router') as typeof import('vue-router')
async function mountCatalog(fail = false) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } })
  const libraries = [
    { id: 'one', display_name: 'Library One', name: 'one', description: 'First library', entry_count: 2 },
    { id: 'two', display_name: 'Library Two', name: 'two', description: 'Second library', entry_count: 0 },
  ]
  const page = loadComponent(new URL('./LibraryList.vue', import.meta.url), {
    'vue-router': vueRouter, 'vue-i18n': { useI18n: () => i18n.global },
    '@/api/library': { listLibraries: async () => { if (fail) throw new Error('Offline'); return { success: true, data: libraries } } },
  })
  const router = vueRouter.createRouter({ history: vueRouter.createMemoryHistory(), routes: [
    { path: '/platform/libraries', component: page },
    { path: '/platform/entries', component: defineComponent({ setup: () => () => h('main', router.currentRoute.value.query.libraries as string) }) },
  ] })
  await router.push('/platform/libraries')
  const app = renderer.createApp(defineComponent({ setup: () => () => h(vueRouter.RouterView) }))
  app.use(router); app.use(i18n); registerPrimitives(app)
  app.component('t-alert', defineComponent({ props: ['message'], setup: (props, { slots }) => () => h('div', { role: 'alert' }, [props.message, slots.operation?.()]) }))
  const errors: unknown[] = []; app.config.errorHandler = e => errors.push(e)
  const root = element('root'); app.mount(root); await settle()
  return { root, router, errors, recover: () => { fail = false }, close: () => app.unmount() }
}

test('library cards open only the selected library and Back restores the catalog', async () => {
  const f = await mountCatalog()
  try {
    const cards = () => descendants(f.root).filter(node => hasClass(node, 'library-card'))
    assert.equal(cards().length, 2)
    assert.match(visibleText(cards()[0]), /Library One/)
    await click(cards()[1]); await new Promise(resolve => setImmediate(resolve)); await settle()
    assert.equal(f.router.currentRoute.value.path, '/platform/entries')
    assert.deepEqual(f.router.currentRoute.value.query, { libraries: 'two' })
    f.router.back(); await new Promise(resolve => setTimeout(resolve, 0)); await settle()
    assert.equal(cards().length, 2)
    await click(cards()[0]); await new Promise(resolve => setImmediate(resolve)); await settle()
    assert.deepEqual(f.router.currentRoute.value.query, { libraries: 'one' })
    const source = readFileSync(new URL('./Library.vue', import.meta.url), 'utf8')
    assert.match(source, /to="\/platform\/libraries"/)
    assert.doesNotMatch(source, /@click="activeLibraryTab = '(?:documents|reviews|entities)'"|<LibraryInfoPopover|library-settings-button/)
    assert.deepEqual(f.errors, [])
  } finally { f.close() }
})

test('a failed catalog read shows retry and can recover without modifying libraries', async () => {
  const f = await mountCatalog(true)
  try {
    assert.match(visibleText(f.root), /无法加载库/)
    assert.equal(descendants(f.root).filter(node => hasClass(node, 'library-card')).length, 0)
    f.recover()
    const retry = descendants(f.root).find(node => node.tag === 'button' && visibleText(node).includes('重试'))!
    await click(retry); await settle()
    assert.equal(descendants(f.root).filter(node => hasClass(node, 'library-card')).length, 2)
    assert.deepEqual(f.errors, [])
  } finally { f.close() }
})
