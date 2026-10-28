import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent, h, reactive } from 'vue'
import * as routerApi from 'vue-router'
import ts from 'typescript'
import { doc, send, settle } from './ui/__tests__/dom'
import { loadUiComponent } from './ui/uiTestLoader'
import { loadComponent } from '../views/library/componentTestHarness'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../utils/sfcStyleTestHelpers'

const require = createRequire(import.meta.url)
const icon = defineComponent({ render: () => h('svg') })
const icons = new Proxy({}, { get: () => icon })
const catalog = [{ id: 'a', name: 'Research', display_name: '', color: 2, entry_count: 1200 }, { id: 'b', name: 'Reports', color: 7, entry_count: 84 }]

async function fixture(path = '/platform/libraries', collapsed = false, mobileOverlay = false) {
  let failed = false, opened = 0, closed = 0
  const ui = reactive({ sidebarCollapsed: collapsed, openSettings() {}, toggleSidebar() { ui.sidebarCollapsed = !ui.sidebarCollapsed } })
  const follows = reactive({ newCount: 12, stateError: false, refreshState() {} })
  const pending = reactive({ total: 5, refresh() {} })
  const menuSource = readFileSync(new URL('../stores/menu.ts', import.meta.url), 'utf8')
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', ts.transpileModule(menuSource, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText)(
    (name: string) => name === 'pinia' ? { defineStore: (_id: string, setup: () => unknown) => setup }
      : name === '@/i18n' ? { __esModule: true, default: { global: { t: (key: string) => key } } } : require(name), module, module.exports)
  const modules = {
    'vue-router': routerApi,
    'pinia': { storeToRefs: (store: unknown) => store },
    'tdesign-icons-vue-next': icons,
    '@/stores/menu': { useMenuStore: module.exports.useMenuStore },
    '@/stores/ui': { useUIStore: () => ui },
    '@/stores/follows': { useFollowStore: () => follows },
    '@/stores/pendingCounts': { usePendingCountsStore: () => pending },
    '@/stores/commandPalette': { useCommandPaletteStore: () => ({ openPalette: () => opened++ }) },
    '@/api/library': { listLibraries: async () => ({ success: !failed, data: catalog }) },
    '@/api/ingest-diagnostics': { listIngestFailures: async () => ({ success: !failed, data: { total: 3 } }) },
    '@/components/ui': { LibMark: loadUiComponent('LibMark'), Tooltip: loadUiComponent('Tooltip') },
    '@/components/PendingCountBadge.vue': { __esModule: true, default: loadComponent(new URL('./PendingCountBadge.vue', import.meta.url), {
      '@/stores/pendingCounts': { usePendingCountsStore: () => pending },
    }) },
  }
  const router = routerApi.createRouter({ history: routerApi.createMemoryHistory(), routes: [{ path: '/platform/entries/:id', name: 'entryDetail', component: { render: () => null } }, { path: '/:pathMatch(.*)*', name: 'entries', component: { render: () => null } }] })
  await router.push(path)
  const component = loadComponent(new URL('./SideMenu.vue', import.meta.url), modules)
  const root = doc.createElement('main')
  doc.body.appendChild(root)
  // Reuse the DOM host and locale from the shared primitive tests.
  const wrapper = defineComponent({ setup: () => () => h(component, { mobileOverlay, onClose: () => closed++ }) })
  const { renderer } = await import('./ui/__tests__/dom')
  const { createI18n } = await import('vue-i18n')
  const en = (await import('../i18n/locales/en-US')).default
  const app = renderer.createApp(wrapper)
  app.use(router)
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en } }))
  app.mount(root)
  await settle()
  return { root, router, ui, follows, pending, opened: () => opened, closed: () => closed,
    fail: () => { failed = true },
    async unmount() { app.unmount(); root.remove(); await settle() },
  }
}

test('shell links select only the scoped library and preserve all main and diagnostic routes', async t => {
  const f = await fixture('/platform/entries?libraries=b')
  t.after(() => f.unmount())
  const links = f.root.querySelectorAll('a')
  assert.equal(links.find((link: any) => link.props.href === '/platform/entries?libraries=b')?.textContent, 'Reports84')
  assert.equal(f.root.querySelectorAll('[aria-current="page"]').length, 1)
  assert.equal(f.root.querySelector('[aria-current="page"]')?.props.href, '/platform/entries?libraries=b')
  const hrefs = links.map((link: any) => link.props.href)
  for (const href of ['/platform/libraries', '/platform/search', '/platform/entries?tab=entities', '/platform/topics', '/platform/follows', '/platform/entries?tab=reviews', '/platform/diagnostics', '/platform/content-checks', '/platform/evaluation', '/platform/model-calls', '/platform/settings']) assert.ok(hrefs.includes(href), href)
  assert.match(f.root.querySelector('[aria-label="Libraries"]')!.textContent, /Research1,200/)
  const mark = f.root.querySelector('a[href="/platform/entries?libraries=b"]')!.querySelector('span')!
  assert.equal(mark.style.backgroundColor, 'var(--lib-7)')
})

test('search entry and collapsed navigation remain keyboard accessible', async t => {
  const f = await fixture('/platform/search', true)
  t.after(() => f.unmount())
  const command = f.root.querySelectorAll('button').find((button: any) => button.props['aria-label'] === 'Search materials, entities or commands')!
  assert.ok(command)
  await send(command, 'click')
  assert.equal(f.opened(), 1)
  const expand = f.root.querySelector('button[aria-label="Expand sidebar"]')!
  await send(expand, 'click')
  assert.equal(f.ui.sidebarCollapsed, false)
  assert.equal(f.root.querySelector('[aria-current="page"]')?.props.href, '/platform/search')
})

test('detail tabs keep the material library selected in the sidebar', async t => {
  const f = await fixture('/platform/entries/material-a?libraries=b&tab=entities')
  t.after(() => f.unmount())
  const current = f.root.querySelectorAll('[aria-current="page"]')
  assert.equal(current.length, 1)
  assert.equal(current[0]!.props.href, '/platform/entries?libraries=b')
})

test('entity, pending and multi-library routes select only their matching navigation entry', async t => {
  const f = await fixture()
  t.after(() => f.unmount())
  for (const [path, expected] of [
    ['/platform/entries?libraries=b&tab=entities', '/platform/entries?tab=entities'],
    ['/platform/entries?libraries=b&tab=reviews', '/platform/entries?tab=reviews'],
    ['/platform/entries?libraries=a,b', '/platform/libraries'],
  ]) {
    await f.router.push(path)
    await settle()
    const current = f.root.querySelectorAll('[aria-current="page"]')
    assert.equal(current.length, 1)
    assert.equal(current[0]!.props.href, expected)
  }
})

test('failed refresh hides catalogue, follow and diagnostic counts instead of showing stale numbers', async t => {
  const f = await fixture()
  t.after(() => f.unmount())
  f.fail(); f.follows.stateError = true
  await f.router.push('/platform/topics')
  await settle()
  assert.doesNotMatch(f.root.textContent, /1,200|84|12/)
  assert.ok(f.root.querySelector('[role="alert"]'))
  assert.ok(f.root.querySelector('button[aria-label="Retry"]'))
  assert.doesNotMatch(f.root.querySelector('a[href="/platform/diagnostics"]')!.textContent, /3/)
})

test('the bottom settings link navigates directly and closes mobile navigation', async t => {
  const f = await fixture('/platform/libraries', true, true)
  t.after(() => f.unmount())
  assert.equal(f.root.querySelectorAll('[aria-current="page"]').length, 1)
  const settings = f.root.querySelectorAll('a').filter((link: any) => link.props.href === '/platform/settings')
  assert.equal(settings.length, 1)
  assert.equal(settings[0]!.textContent, 'Settings')
  await send(settings[0]!, 'click')
  assert.equal(f.router.currentRoute.value.path, '/platform/settings')
  assert.equal(f.closed(), 1)
})

test('sidebar active states use theme tokens and preserve the design dimensions under the legacy root', async () => {
  const rules = await compiledSFCStyleRules(new URL('./SideMenu.vue', import.meta.url))
  const active = styleDeclarationsAtWidth(rules, '.rq-sidenav__link[aria-current="page"]', 1280)
  assert.equal(active.background, 'var(--text)')
  assert.equal(active.color, 'var(--bg)')
  const sidebar = styleDeclarationsAtWidth(rules, '.rq-sidebar', 1280)
  assert.equal(sidebar.width, 'var(--sidebar)')
  assert.equal(sidebar.background, 'var(--surface)')
  assert.equal(styleDeclarationsAtWidth(rules, '.rq-sidebar__scroll', 1280).overflow, 'auto')
  assert.ok(!rules.some(rule => 'filter' in rule.declarations))
  assert.doesNotMatch(readFileSync(new URL('./SideMenu.vue', import.meta.url), 'utf8'), /\b\d*\.?\d+rem\b/)
})
