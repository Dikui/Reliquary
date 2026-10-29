import { settingsTestUi } from '../settings/settingsTestUi'
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
import { CONTENT_TYPES } from '../../types/entryClassification'
import { buildSearchRequest, defaultSearchFilters, searchStateFromURL, searchStateToURL } from '../../utils/search'
import { reactive } from 'vue'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zh, 'en-US': en }
const empty = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? (name === '@/components/ui' ? settingsTestUi() : name.endsWith('.css') ? {} : require(name)), module, module.exports)
  return module.exports
}
function component(path: string, modules: Record<string, unknown>) {
  const route = reactive({ query: { libraries: 'lib-a' } as Record<string, string> })
  modules = { 'vue-router': { RouterLink: defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { href: props.to.path }, slots.default?.()) }), useRoute: () => route, useRouter: () => ({ replace: async ({ query }: any) => { route.query = query } }) }, '@/components/ui/feedback': { toast: { success() {} }, confirm: (options: any) => { confirmation = options; return { destroy() {}, update() {} } } }, '@/components/LibraryScopeSelect.vue': empty, '@/api/library': { listLibraries: async () => ({ success: true, data: [{ id: 'lib-a', color: 1, name: 'Library A', display_name: 'Library A' }, { id: 'lib-b', color: 2, name: 'Library B', display_name: 'Library B' }] }) }, '@/components/FollowToggle.vue': empty, '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) }, ...modules }
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
let confirmation: any
const topic = { id: 'topic-a', library_id: 'lib-a', name: 'Space research', description: 'Space missions and research', entry_count: 3, created_at: '', updated_at: '' }
const article = { id: 'article-a', library_id: 'lib-a', content_type: 'event', processing_mode: 'full', starred: false, topics: [{ id: 'old-topic', name: 'Old topic' }], parse_status: 'completed' }
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: topic management renders counts and failed batches, retries, creates, edits and deletes`, async () => {
    const calls: unknown[] = []
    let run = { id: 'run-a', topic_id: topic.id, status: 'failed', total: 31, processed: 31, failed_entries: 1, failed_batches: 1, batches: [{ id: 'batch-a', ordinal: 2, entry_count: 1, status: 'failed', attempts: 1, error: 'Model timeout' }] }
    const view = component('./TopicManagement.vue', {

      '@/api/topics': {
        listTopics: async () => ({ success: true, data: [topic] }), getTopicBackfill: async () => ({ success: true, data: run }),
        createTopic: async (input: any) => { calls.push(['create', input]); return { success: true, data: topic } },
        updateTopic: async (id: string, input: any) => { calls.push(['update', id, input]); return { success: true, data: topic } },
        deleteTopic: async (id: string) => { calls.push(['delete', id]); return { success: true } },
        retryTopicBackfill: async (id: string) => { calls.push(['retry', id]); run = { ...run, status: 'completed', failed_entries: 0, failed_batches: 0, batches: [] }; return { success: true, data: run } },
      },
    })
    const html = await render(view, {}, locale, async state => { state.libraries.value = [{ id: 'lib-a', color: 1, name: 'Library A' }]; await state.load() })
    assert.ok(html.includes(topic.name))
    assert.ok(html.includes('31 / 31'))
    assert.ok(html.includes('Model timeout'))
    assert.ok(html.includes(messages[locale].topicManagement.retryFailedBatches))
    const fresh = component('./TopicManagement.vue', {

      '@/api/topics': {
        listTopics: async () => ({ success: true, data: [topic] }), getTopicBackfill: async () => ({ success: true, data: run }),
        createTopic: async (input: any) => { calls.push(['create', input]); return { success: true, data: topic } },
        updateTopic: async (id: string, input: any) => { calls.push(['update', id, input]); return { success: true, data: topic } },
        deleteTopic: async (id: string) => { calls.push(['delete', id]); return { success: true } },
        retryTopicBackfill: async (id: string) => { calls.push(['retry', id]); run = { ...run, status: 'completed', failed_entries: 0, failed_batches: 0, batches: [] }; return { success: true, data: run } },
      },
    })
    await render(fresh, {}, locale, async state => {
      state.libraries.value = [{ id: 'lib-a', color: 1, name: 'Library A' }]
      await state.load()
      await state.retry(topic.id)
      state.openEditor()
      state.draft.value = { name: ' New topic ', description: ' New description ', libraryId: 'lib-a' }
      await state.save()
      state.openEditor(topic)
      assert.equal('library_ids' in state.draft.value, false)
      state.draft.value.description = 'Changed description'
      await state.save()
      state.progress[topic.id] = { ...run, status: 'cancelled', failed_batches: 1 }
      await state.retry(topic.id)
      state.requestDelete(topic)
      await confirmation.onConfirm()
    })
    assert.deepEqual(calls, [['retry', topic.id], ['create', { name: 'New topic', description: 'New description', library_id: 'lib-a' }], ['update', topic.id, { name: topic.name, description: 'Changed description' }], ['delete', topic.id]])
  })
  test(`${locale}: operation log displays before and after, time and type filters`, async () => {
    const calls: unknown[] = [], urls: unknown[] = [], route = { path: '/platform/settings', query: { section: 'operation-logs', operation_type: 'star', page: '2' } }
    const view = component('./OperationLogs.vue', {
      'vue-router': { RouterLink: defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { href: props.to.path }, slots.default?.()) }), useRoute: () => route, useRouter: () => ({ replace: (value: any) => { urls.push(value); return Promise.resolve() } }) },
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
      'vue-router': { RouterLink: defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { href: props.to.path }, slots.default?.()) }), useRoute: () => ({ query: {} }), useRouter: () => ({ replace: async () => {} }) },
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

  test(`${locale}: a duplicate topic name keeps the editor and entered text`, async () => {
    const view = component('./TopicManagement.vue', {

      '@/api/topics': { createTopic: async () => { throw { status: 409 } } },
    })
    const html = await render(view, {}, locale, async state => {
      state.libraries.value = [{ id: 'lib-a', color: 1, name: 'Library A' }]
      await nextTick()
      state.openEditor()
      state.draft.value = { name: topic.name, description: 'My description', libraryId: 'lib-a' }
      await state.save()
      assert.equal(state.editorVisible.value, true)
      assert.equal(state.draft.value.description, 'My description')
    })
    assert.ok(html.includes(messages[locale].topicManagement.nameExists))
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
test('topic, judgment and operation log API wrappers use the supported routes and payloads', async () => {
  const calls: unknown[] = []
  const request = Object.fromEntries(['get', 'post', 'put', 'del'].map(method => [method, (url: string, body?: unknown) => { calls.push({ method, url, ...(body === undefined ? {} : { body }) }); return Promise.resolve({ success: true }) }]))
  const api = (path: string) => evaluate(readFileSync(new URL(path, import.meta.url), 'utf8'), { '@/utils/request': request })
  const topics = api('../../api/topics.ts'), judgments = api('../../api/entry-judgments.ts'), logs = api('../../api/operation-logs.ts')
  await topics.listTopics('lib/a')
  await topics.createTopic({ library_id: 'lib/a', name: 'Name', description: 'Description' })
  await topics.updateTopic('t/a', { name: 'Name 2', description: 'Description 2' })
  await topics.deleteTopic('t/a')
  await topics.getTopicBackfill('t/a')
  await topics.retryTopicBackfill('t/a')
  await judgments.setEntryStar('k/a', false)
  await judgments.correctEntryContentType('k/a', 'tutorial')
  await judgments.correctEntryTopics('k/a', [])
  await logs.listOperationLogs({ operation_type: 'unstar', page: 2, page_size: 30 })
  assert.deepEqual(calls, [
    { method: 'get', url: '/api/v1/topics?library_id=lib%2Fa' },
    { method: 'post', url: '/api/v1/topics', body: { library_id: 'lib/a', name: 'Name', description: 'Description' } },
    { method: 'put', url: '/api/v1/topics/t%2Fa', body: { name: 'Name 2', description: 'Description 2' } },
    { method: 'del', url: '/api/v1/topics/t%2Fa' },
    { method: 'get', url: '/api/v1/topics/t%2Fa/backfill' },
    { method: 'post', url: '/api/v1/topics/t%2Fa/backfill/retry', body: {} },
    { method: 'put', url: '/api/v1/entries/k%2Fa/star', body: { starred: false } },
    { method: 'put', url: '/api/v1/entries/k%2Fa/content-type', body: { content_type: 'tutorial' } },
    { method: 'put', url: '/api/v1/entries/k%2Fa/topics', body: { topic_ids: [] } },
    { method: 'get', url: '/api/v1/operation-logs?operation_type=unstar&page=2&page_size=30' },
  ])
})
