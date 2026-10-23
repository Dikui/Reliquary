import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import enUS from '../../i18n/locales/en-US.ts'
import zhCN from '../../i18n/locales/zh-CN.ts'

const require = createRequire(import.meta.url)
const filename = 'Library.vue'
const source = readFileSync(new URL('./Library.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename })
const breadcrumbs = descriptor.template!.content.match(/<h2 class="document-breadcrumb">[\s\S]*?<\/h2>/g)
assert.equal(breadcrumbs?.length, 1, 'the actual library breadcrumb must remain covered')
const template = compileTemplate({ id: 'breadcrumb-test', filename, source: breadcrumbs![0]! })
assert.deepEqual(template.errors, [])
const compiled = ts.transpileModule(template.code, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const exports = {} as { render(context: unknown, cache: unknown[]): any }
new Function('require', 'exports', compiled)(require, exports)

type Locale = 'zh-CN' | 'en-US'
type KbTab = 'documents' | 'reviews' | 'entities'
type LibraryInfo = { name: string } | null
const tabs: KbTab[] = ['documents', 'reviews', 'entities']
const expectedLabels = {
  'zh-CN': { root: '库', tabs: ['资料', '历史待审', '实体'] },
  'en-US': { root: 'Library', tabs: ['Materials', 'Pending legacy materials', 'Entities'] },
}
const databaseNames: [string, LibraryInfo][] = [
  ['before the library loads', null],
  ['with the Chinese default database name', { name: '库' }],
  ['with an English database name', { name: 'Library' }],
  ['with a custom database name', { name: 'Personal research' }],
]

function textContent(node: any): string {
  if (typeof node === 'string') return node
  if (!node || typeof node !== 'object') return ''
  if (typeof node.children === 'string') return node.children
  return Array.isArray(node.children) ? node.children.map(textContent).join('') : ''
}

function createHarness(locale: Locale, libraryInfo: LibraryInfo) {
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const context = {
    libraryInfo,
    activeLibraryTab: 'documents' as KbTab,
    $t: (...args: Parameters<typeof i18n.global.t>) => i18n.global.t(...args),
  }
  let tree: any
  const component = defineComponent({ setup: () => () => { tree = exports.render(context, []); return tree } })
  return {
    i18n,
    context,
    async render() {
      const app = createSSRApp(component)
      app.component('t-icon', defineComponent({ setup: () => () => h('svg') }))
      app.component('PendingCountBadge', defineComponent({ setup: () => () => h('span') }))
      const html = await renderToString(app)
      return { html, tree }
    },
  }
}

function assertBreadcrumb(tree: any, locale: Locale, activeTab: KbTab) {
  assert.equal(tree.type, 'h2')
  assert.equal(tree.props.class, 'document-breadcrumb')
  const rootLabel = tree.children.find((node: any) => node.type === 'span' && !node.props?.class)
  assert.equal(textContent(rootLabel), expectedLabels[locale].root)
  const buttons = tree.children.filter((node: any) => node.type === 'button')
  assert.equal(buttons.length, tabs.length)
  assert.deepEqual(buttons.map((node: any) => textContent(node).trim()), expectedLabels[locale].tabs)
  assert.deepEqual(buttons.map((node: any) => node.props['aria-pressed']), tabs.map(tab => tab === activeTab))
  return buttons
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const [name, libraryInfo] of databaseNames) {
    test(`${locale} breadcrumb uses the fixed localized library name ${name}`, async () => {
      const originalInfo = libraryInfo ? { ...libraryInfo } : null
      const harness = createHarness(locale, libraryInfo)
      for (const activeTab of tabs) {
        harness.context.activeLibraryTab = activeTab
        const { html, tree } = await harness.render()
        assert.ok(html.includes(`<span>${expectedLabels[locale].root}</span>`))
        assertBreadcrumb(tree, locale, activeTab)
      }
      assert.deepEqual(harness.context.libraryInfo, originalInfo, 'rendering preserves the database name')
    })
  }
}

test('the existing breadcrumb switches language without changing the database name or selected tab', async () => {
  const harness = createHarness('zh-CN', { name: '库' })
  const first = await harness.render()
  const buttons = assertBreadcrumb(first.tree, 'zh-CN', 'documents')
  buttons[2].props.onClick()
  assert.equal(harness.context.activeLibraryTab, 'entities')

  harness.i18n.global.locale.value = 'en-US'
  const english = await harness.render()
  assertBreadcrumb(english.tree, 'en-US', 'entities')
  assert.ok(!english.html.includes('库'))

  harness.i18n.global.locale.value = 'zh-CN'
  const chinese = await harness.render()
  assertBreadcrumb(chinese.tree, 'zh-CN', 'entities')
  assert.deepEqual(harness.context.libraryInfo, { name: '库' })
})
