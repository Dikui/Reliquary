import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileTemplate } from '@vue/compiler-sfc'
import { computed, createSSRApp, reactive } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import { compiledSFCStyleRules } from '../utils/sfcStyleTestHelpers'
import zhCN from '../i18n/locales/zh-CN'
import enUS from '../i18n/locales/en-US'
import { click, descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../views/library/componentTestHarness'

test('the sole bottom settings button opens directly and closes mobile navigation', async () => {
  const paths: string[] = []
  let opens = 0, closes = 0
  const ui = reactive({ sidebarCollapsed: false, openSettings: () => opens++, toggleSidebar() {} })
  const modules: Record<string, unknown> = {
    'vue-router': { useRoute: () => ({ name: 'libraries', path: '/platform/libraries', query: {} }), useRouter: () => ({ push: (path: string) => paths.push(path) }) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'pinia': { storeToRefs: (store: unknown) => store },
    '@/stores/ui': { useUIStore: () => ui },
    '@/stores/follows': { useFollowStore: () => ({ refreshState() {}, newCount: 0 }) },
    '@/stores/menu': { useMenuStore: () => ({ menuGroups: computed(() => [{ key: 'pending', title: 'Pending', items: [{ key: 'pending', title: 'Pending', path: 'entries', tab: 'reviews' }] }]) }) },
    '@/components/PendingCountBadge.vue': { __esModule: true, default: { render: () => null } },
    '@/assets/img/reliquary.svg': { __esModule: true, default: '/reliquary.svg' },
  }
  modules['@/components/UserMenu.vue'] = { __esModule: true, default: loadComponent(new URL('./UserMenu.vue', import.meta.url), modules) }
  const app = renderer.createApp(loadComponent(new URL('./SideMenu.vue', import.meta.url), modules), { mobileOverlay: true, onClose: () => closes++ })
  registerPrimitives(app)
  const root = element('root')
  try {
    app.mount(root); await settle()
    const buttons = descendants(root).filter(node => node.tag === 'button' && node.props['aria-label'] === 'menu.settings')
    assert.equal(buttons.length, 1)
    assert.equal(descendants(root).filter(node => node.tag === 'h2').length, 0)
    assert.equal(visibleText(buttons[0]!), 'menu.settings')
    await click(buttons[0]!)
    assert.equal(opens, 1)
    assert.equal(closes, 1)
    assert.deepEqual(paths, ['/platform/settings'])
  } finally { app.unmount() }
})

test('dark sidebar CSS filters only the wordmark after scoped compilation', async () => {
  const rules = await compiledSFCStyleRules(new URL('./SideMenu.vue', import.meta.url))
  const filters = rules.filter(rule => 'filter' in rule.declarations)
  assert.equal(filters.length, 1)
  assert.deepEqual(filters.map(rule => [rule.selector, rule.declarations.filter]), [
    [':root[theme-mode="dark"] .logo', 'invert(1)'],
  ])
  assert.ok(!rules.some(rule => /^:root(?:\[[^\]]+\])*$/.test(rule.selector) && 'filter' in rule.declarations))
  assert.match(readFileSync(new URL('./SideMenu.vue', import.meta.url), 'utf8'), /<img class="logo"/)
})

const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const source = readFileSync(new URL('../views/library/settings/LibraryChunkingDebug.vue', import.meta.url), 'utf8')
const statsTemplate = source.match(/<div class="chunk-stats">[\s\S]*?\n          <\/div>/)![0]
const compiled = compileTemplate({ source: statsTemplate, filename: 'chunk-stats.vue', id: 'ui09-stats', ssr: true, ssrCssVars: [] })
assert.deepEqual(compiled.errors, [])
const code = ts.transpileModule(compiled.code, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
const module = { exports: {} as { ssrRender: any } }
new Function('require', 'module', 'exports', code)(createRequire(import.meta.url), module, module.exports)

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: actual chunk statistics template renders localized minimum and maximum`, async () => {
    const app = createSSRApp({
      props: ['result'], ssrRender: module.exports.ssrRender,
    }, { result: { stats: { count: 2, avg_chars: 193, stddev_chars: 124, min_chars: 70, max_chars: 317, truncated_to: 10 } } })
    app.use(createI18n({ legacy: false, locale, messages }))
    const html = await renderToString(app)
    assert.ok(html.includes(locale === 'zh-CN' ? '最小 70' : 'Min 70'))
    assert.ok(html.includes(locale === 'zh-CN' ? '最大 317' : 'Max 317'))
    assert.ok(html.includes(locale === 'zh-CN' ? '已截断；总数 10' : 'truncated; total 10'))
    assert.ok(html.includes(locale === 'zh-CN' ? '块' : 'chunks'))
    assert.doesNotMatch(html, /entryEditor\./)
    if (locale === 'zh-CN') assert.doesNotMatch(html, /\bmin\b|\bmax\b/i)
  })
}
