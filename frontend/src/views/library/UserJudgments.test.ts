import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { parse, compileScript, compileTemplate } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'

import { buildSearchRequest, defaultSearchFilters, searchStateFromURL, searchStateToURL } from '../../utils/search'
import { safeTextareaTestModule } from '../../components/__tests__/safeTextareaStub'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zh, 'en-US': en }
const empty = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), module, module.exports)
  return module.exports
}
function component(path: string, modules: Record<string, unknown>) {
  modules = { '@/api/library': { listLibraries: async () => ({ success: true, data: [{ id: 'lib-a', name: 'Library A', display_name: 'Library A' }, { id: 'lib-b', name: 'Library B', display_name: 'Library B' }] }) }, '@/components/FollowToggle.vue': empty, '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) }, ...modules }
  const filename = path.split('/').at(-1)!
  const { descriptor, errors } = parse(readFileSync(new URL(path, import.meta.url), 'utf8'), { filename })
  assert.deepEqual(errors, [])
  const script = compileScript(descriptor, { id: filename })
  const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const result = evaluate(script.content, modules).default
  result.ssrRender = evaluate(template.code, modules).ssrRender
  return result
}
const OPERATION_TYPES = evaluate(readFileSync(new URL('../../api/operation-logs.ts', import.meta.url), 'utf8'), { '@/utils/request': { get() {} } }).OPERATION_TYPES
async function render(value: any, props: Record<string, unknown>, locale: keyof typeof messages, action?: (state: any) => Promise<void>) {
  if (action) {
    const setup = value.setup
    value.setup = (props: object, context: object) => {
      const state = setup(props, context)
      onServerPrefetch(() => action(state))
      return state
    }
  }
  const app = createSSRApp(value, props)
  app.use(createI18n({ legacy: false, locale, messages }))
  const stub = defineComponent({ props: ['visible'], setup: (props, { attrs, slots }) => () => props.visible === false ? null : h('div', attrs, [slots.header?.(), slots.icon?.(), slots.default?.()]) })
  for (const name of ['t-button', 't-icon', 't-select', 't-input', 't-dialog', 't-empty', 't-loading', 't-pagination', 't-textarea']) app.component(name, stub)
  const errors: unknown[] = [], warnings: string[] = []
  app.config.errorHandler = error => errors.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  const html = await renderToString(app)
  assert.deepEqual(errors, [])
  assert.deepEqual(warnings, [])
  return html
}
const notifications = { MessagePlugin: { success() {} } }
const topic = { id: 'topic-a', name: 'Space research', description: 'Space missions and research', entry_count: 3, created_at: '', updated_at: '' }
const article = { id: 'article-a',  processing_mode: 'full', starred: false,  parse_status: 'completed' }
function judgments(api: Record<string, unknown>, details?: unknown, notificationModule: { MessagePlugin: { success: (message: string) => unknown } } = notifications) {
  return component('../../components/EntryArticleJudgments.vue', {
    '@/types/entryClassification': { }, '@/api/entry-judgments': api,

    '@/api/library': { getEntryDetails: async () => ({ success: true, data: details }) },
    './EntryStarToggle.vue': empty, 'tdesign-vue-next': notificationModule,
  })
}
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: star actions wait for confirmation, emit updates and preserve state after failure`, async () => {
    const calls: unknown[] = [], updates: unknown[] = []
    let fail = false
    const star = component('../../components/EntryStarToggle.vue', {
      'tdesign-vue-next': notifications,
      '@/api/entry-judgments': { setEntryStar: async (id: string, starred: boolean) => { calls.push({ id, starred }); if (fail) throw new Error('network'); return { success: true, data: { id, starred } } } },
    })
    const value = { ...article }
    const html = await render(star, { entry: value, onUpdated: (update: any) => { updates.push(update); Object.assign(value, update) } }, locale, async state => {
      await state.toggle()
      assert.equal(value.starred, true)
      await state.toggle()
      assert.equal(value.starred, false)
      fail = true
      await state.toggle()
      assert.equal(value.starred, false)
    })
    assert.deepEqual(calls, [{ id: 'article-a', starred: true }, { id: 'article-a', starred: false }, { id: 'article-a', starred: true }])
    assert.equal(updates.length, 2)
    assert.ok(html.includes(messages[locale].articleJudgments.starFailed))
    assert.match(html, /aria-pressed="false"/)
  })









  test(`${locale}: operation log displays before and after, time and type filters`, async () => {
    const calls: unknown[] = [], urls: unknown[] = [], route = { path: '/platform/settings', query: { section: 'operation-logs', operation_type: 'star', page: '2' } }
    const view = component('./OperationLogs.vue', {
      'vue-router': { useRoute: () => route, useRouter: () => ({ replace: (value: any) => { urls.push(value); return Promise.resolve() } }) },
      '@/api/operation-logs': { OPERATION_TYPES, listOperationLogs: async (input: any) => { calls.push(input); return { success: true, data: { items: [{ id: 'log-a', operation_type: 'star', object_id: 'article-a', before: { starred: false }, after: { starred: true }, created_at: '2026-10-11T04:00:00Z' }], total: 1 } } } },
    })
    const html = await render(view, {}, locale, async state => { state.readURL(); await state.load(); state.operationType.value = 'correct_topics'; state.filterChanged(); await nextTick() })
    assert.deepEqual(calls[0], { operation_type: 'star', page: 2, page_size: 30 })
    assert.deepEqual(calls[1], { operation_type: 'correct_topics', page: 1, page_size: 30 })
    assert.ok(html.includes(messages[locale].operationLogs.types.star))
    assert.ok(html.includes('article-a'))
    assert.ok(html.includes('&quot;starred&quot;: false'))
    assert.ok(html.includes('&quot;starred&quot;: true'))
    assert.match(html, /datetime="2026-10-11T04:00:00Z"/)
    assert.deepEqual(urls, [{ query: { section: 'operation-logs', operation_type: 'correct_topics' } }])
  })
  test(`${locale}: operation log renders web actors, named API keys and missing-name fallback`, async () => {
    const actors = [
      { actor: 'web_user:1' },
      { actor: 'api_key:2', actor_name: 'Collector <mobile>' },
      { actor: 'api_key:3' },
      { actor: 'api_key:4', actor_name: '   ' },
    ]
    const view = component('./OperationLogs.vue', {
      'vue-router': { useRoute: () => ({ query: {} }), useRouter: () => ({ replace: async () => {} }) },
      '@/api/operation-logs': { OPERATION_TYPES, listOperationLogs: async () => ({ success: true, data: {
        items: actors.map((actor, index) => ({ ...actor, id: String(index), operation_type: 'star', object_id: 'article-a', before: false, after: true, created_at: '2026-10-11T04:00:00Z' })), total: actors.length,
      } }) },
    })
    const html = await render(view, {}, locale, async state => { await state.load() })
    const rows = [...html.matchAll(/<p class="log-actor">(.*?)<\/p>/g)].map(match => match[1])
    assert.deepEqual(rows, [
      `${messages[locale].operationLogs.actor} ${messages[locale].operationLogs.webActor}`,
      `${messages[locale].operationLogs.actor} API Key Collector &lt;mobile&gt;`,
      `${messages[locale].operationLogs.actor} API Key #3`,
      `${messages[locale].operationLogs.actor} API Key #4`,
    ])
  })


}
test('star filters preserve all, starred and unstarred through requests and URL state', () => {
  for (const starred of [undefined, true, false]) {
    const filters = { ...defaultSearchFilters(), starred }
    const request = buildSearchRequest('Example', filters)
    const url = searchStateToURL('Example', filters)
    const restored = searchStateFromURL(url)
    assert.equal(request.starred, starred)
    assert.equal(restored.filters.starred, starred)
    assert.equal('starred' in request, starred !== undefined)
  }
})
