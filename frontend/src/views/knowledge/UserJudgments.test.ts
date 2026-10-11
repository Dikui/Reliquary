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
import { CONTENT_TYPES } from '../../types/knowledgeClassification'
import { buildKnowledgeSearchRequest, defaultSearchFilters, searchStateFromURL, searchStateToURL } from '../../utils/knowledgeSearch'
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
const topic = { id: 'topic-a', name: 'Space research', description: 'Space missions and research', article_count: 3, created_at: '', updated_at: '' }
const article = { id: 'article-a', content_type: 'event', processing_mode: 'full', starred: false, topics: [{ id: 'old-topic', name: 'Old topic' }], parse_status: 'completed' }
function judgments(api: Record<string, unknown>, details?: unknown) {
  return component('../../components/KnowledgeArticleJudgments.vue', {
    '@/types/knowledgeClassification': { CONTENT_TYPES }, '@/api/knowledge-judgments': api,
    '@/api/topics': { listTopics: async () => ({ success: true, data: [topic] }) },
    '@/api/knowledge-base/index': { getKnowledgeDetails: async () => ({ success: true, data: details }) },
    './KnowledgeStarToggle.vue': empty, 'tdesign-vue-next': notifications,
  })
}
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: star actions wait for confirmation, emit updates and preserve state after failure`, async () => {
    const calls: unknown[] = [], updates: unknown[] = []
    let fail = false
    const star = component('../../components/KnowledgeStarToggle.vue', {
      'tdesign-vue-next': notifications,
      '@/api/knowledge-judgments': { setKnowledgeStar: async (id: string, starred: boolean) => { calls.push({ id, starred }); if (fail) throw new Error('network'); return { success: true, data: { id, starred } } } },
    })
    const value = { ...article }
    const html = await render(star, { knowledge: value, onUpdated: (update: any) => { updates.push(update); Object.assign(value, update) } }, locale, async state => {
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
  test(`${locale}: content type corrections cover both same-mode changes and summary to full`, async () => {
    for (const [mode, before, after] of [['full', 'event', 'research'], ['summary_only', 'tutorial', 'reference'], ['summary_only', 'tutorial', 'event']] as const) {
      const calls: unknown[] = [], value = { ...article, processing_mode: mode, content_type: before }
      const view = judgments({ correctKnowledgeContentType: async (...input: unknown[]) => { calls.push(input); return { success: true, data: { ...value, content_type: after, processing_mode: after === 'event' || after === 'research' ? 'full' : 'summary_only' } } } })
      const html = await render(view, { knowledge: value, onUpdated: (update: any) => Object.assign(value, update) }, locale, async state => { state.typeDraft.value = after; await state.saveType(); await nextTick() })
      assert.deepEqual(calls, [['article-a', after, false]])
      assert.equal(value.content_type, after)
      assert.ok(html.includes(messages[locale].articleJudgments.saveContentType))
    }
  })
  test(`${locale}: full to summary requires a second confirmation and retains it after a failed save`, async () => {
    const calls: unknown[] = [], value = { ...article }
    const view = judgments({ correctKnowledgeContentType: async (...input: unknown[]) => { calls.push(input); throw new Error('Original change failed') } })
    const html = await render(view, { knowledge: value }, locale, async state => {
      state.typeDraft.value = 'tutorial'
      await state.saveType()
      assert.equal(calls.length, 0)
      assert.equal(state.confirmRemoveBody.value, true)
      await state.saveType(true)
      assert.equal(state.confirmRemoveBody.value, true)
      assert.equal(value.content_type, 'event')
    })
    assert.deepEqual(calls, [['article-a', 'tutorial', true]])
    assert.ok(html.includes(messages[locale].articleJudgments.removeBodyDescription))
    assert.ok(html.includes('Original change failed'))
  })
  test(`${locale}: confirmed full to summary updates the article and closes the dialog`, async () => {
    const value = { ...article }, calls: unknown[] = []
    const view = judgments({ correctKnowledgeContentType: async (...input: unknown[]) => { calls.push(input); return { success: true, data: { ...value, content_type: 'tutorial', processing_mode: 'summary_only' } } } })
    await render(view, { knowledge: value, onUpdated: (update: any) => Object.assign(value, update) }, locale, async state => {
      state.typeDraft.value = 'tutorial'
      await state.saveType()
      assert.equal(calls.length, 0)
      await state.saveType(true)
      assert.equal(state.confirmRemoveBody.value, false)
    })
    assert.equal(value.processing_mode, 'summary_only')
    assert.deepEqual(calls, [['article-a', 'tutorial', true]])
  })
  test(`${locale}: source unverified content type corrections keep full text without a removal dialog`, async () => {
    const value = { ...article, source_unverified: true }, calls: unknown[] = []
    const view = judgments({ correctKnowledgeContentType: async (...input: unknown[]) => { calls.push(input); return { success: true, data: { ...value, content_type: 'tutorial', processing_mode: 'full' } } } })
    await render(view, { knowledge: value, onUpdated: (update: any) => Object.assign(value, update) }, locale, async state => {
      state.typeDraft.value = 'tutorial'
      await state.saveType()
      assert.equal(state.confirmRemoveBody.value, false)
    })
    assert.deepEqual(calls, [['article-a', 'tutorial', false]])
    assert.equal(value.processing_mode, 'full')
  })
  test(`${locale}: summary to full fetch failure keeps the original article and visible reason`, async () => {
    const value = { ...article, content_type: 'tutorial', processing_mode: 'summary_only' }
    const view = judgments({ correctKnowledgeContentType: async () => { throw new Error('Cannot fetch original article: HTTP 404') } })
    const html = await render(view, { knowledge: value }, locale, async state => { state.typeDraft.value = 'event'; await state.saveType() })
    assert.equal(value.content_type, 'tutorial')
    assert.equal(value.processing_mode, 'summary_only')
    assert.ok(html.includes('Cannot fetch original article: HTTP 404'))
  })
  test(`${locale}: topic corrections submit selected IDs and reload the article after a success-only response`, async () => {
    const calls: unknown[] = [], updates: any[] = []
    const view = judgments({ correctKnowledgeTopics: async (...input: unknown[]) => { calls.push(input); return { success: true } } }, { ...article, topics: [topic] })
    await render(view, { knowledge: { ...article }, onUpdated: (value: any) => updates.push(value) }, locale, async state => {
      await state.loadTopics()
      state.topicDraft.value = ['topic-a']
      await state.saveTopics()
      state.topicDraft.value = []
      await state.saveTopics()
    })
    assert.deepEqual(calls, [['article-a', ['topic-a']], ['article-a', []]])
    assert.equal(updates[0].topics[0].id, 'topic-a')
  })
  test(`${locale}: topic management renders counts and failed batches, retries, creates, edits and deletes`, async () => {
    const calls: unknown[] = []
    let run = { id: 'run-a', topic_id: topic.id, status: 'failed', total: 31, processed: 31, failed_articles: 1, failed_batches: 1, batches: [{ id: 'batch-a', ordinal: 2, article_count: 1, status: 'failed', attempts: 1, error: 'Model timeout' }] }
    const view = component('./TopicManagement.vue', {
      '@/components/SafeTextarea.vue': safeTextareaTestModule, 'tdesign-vue-next': notifications,
      '@/api/topics': {
        listTopics: async () => ({ success: true, data: [topic] }), getTopicBackfill: async () => ({ success: true, data: run }),
        createTopic: async (input: any) => { calls.push(['create', input]); return { success: true, data: topic } },
        updateTopic: async (id: string, input: any) => { calls.push(['update', id, input]); return { success: true, data: topic } },
        deleteTopic: async (id: string) => { calls.push(['delete', id]); return { success: true } },
        retryTopicBackfill: async (id: string) => { calls.push(['retry', id]); run = { ...run, status: 'completed', failed_articles: 0, failed_batches: 0, batches: [] }; return { success: true, data: run } },
      },
    })
    const html = await render(view, {}, locale, async state => { await state.load() })
    assert.ok(html.includes(topic.name))
    assert.ok(html.includes('31 / 31'))
    assert.ok(html.includes('Model timeout'))
    assert.ok(html.includes(messages[locale].topicManagement.retryFailedBatches))
    const fresh = component('./TopicManagement.vue', {
      '@/components/SafeTextarea.vue': safeTextareaTestModule, 'tdesign-vue-next': notifications,
      '@/api/topics': {
        listTopics: async () => ({ success: true, data: [topic] }), getTopicBackfill: async () => ({ success: true, data: run }),
        createTopic: async (input: any) => { calls.push(['create', input]); return { success: true, data: topic } },
        updateTopic: async (id: string, input: any) => { calls.push(['update', id, input]); return { success: true, data: topic } },
        deleteTopic: async (id: string) => { calls.push(['delete', id]); return { success: true } },
        retryTopicBackfill: async (id: string) => { calls.push(['retry', id]); run = { ...run, status: 'completed', failed_articles: 0, failed_batches: 0, batches: [] }; return { success: true, data: run } },
      },
    })
    await render(fresh, {}, locale, async state => {
      await state.load()
      await state.retry(topic.id)
      state.openEditor()
      state.draft.value = { name: ' New topic ', description: ' New description ' }
      await state.save()
      state.openEditor(topic)
      state.draft.value.description = 'Changed description'
      await state.save()
      state.requestDelete(topic)
      await state.remove()
    })
    assert.deepEqual(calls, [['retry', topic.id], ['create', { name: 'New topic', description: 'New description' }], ['update', topic.id, { name: topic.name, description: 'Changed description' }], ['delete', topic.id]])
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
  test(`${locale}: external Agent writes have localized operation labels and URL filters`, async () => {
    const route = { query: { operation_type: 'ingest' } }, calls: unknown[] = []
    const view = component('./OperationLogs.vue', {
      'vue-router': { useRoute: () => route, useRouter: () => ({ replace: async () => {} }) },
      '@/api/operation-logs': { OPERATION_TYPES, listOperationLogs: async (input: unknown) => { calls.push(input); return { success: true, data: {
        items: ['ingest', 'write_report', 'processing_review_confirm', 'processing_review_correct', 'processing_review_ignore', 'review_supplement_url', 'review_admit_unverified', 'review_convert_to_mine'].map((operation_type, index) => ({ id: String(index), actor: 'api_key:2', actor_name: 'Grok Bot', operation_type, object_id: 'article-a', before: null, after: { source_name: 'grok-bot' }, created_at: '2026-10-11T04:00:00Z' })), total: 8,
      } } } },
    })
    const html = await render(view, {}, locale, async state => {
      state.readURL()
      assert.ok(state.options.value.some((option: any) => option.value === 'ingest' && option.label === messages[locale].operationLogs.types.ingest))
      assert.ok(state.options.value.some((option: any) => option.value === 'write_report' && option.label === messages[locale].operationLogs.types.write_report))
      await state.load()
    })
    assert.deepEqual(calls, [{ operation_type: 'ingest', page: 1, page_size: 30 }])
    assert.ok(html.includes(messages[locale].operationLogs.types.ingest))
    assert.ok(html.includes(messages[locale].operationLogs.types.write_report))
    for (const type of ['processing_review_confirm', 'processing_review_correct', 'processing_review_ignore', 'review_supplement_url', 'review_admit_unverified', 'review_convert_to_mine'] as const) {
      assert.ok(html.includes(messages[locale].operationLogs.types[type]))
      assert.ok(OPERATION_TYPES.includes(type))
    }
    assert.doesNotMatch(html, /operationLogs\.types\./)
  })
  test(`${locale}: a duplicate topic name keeps the editor and entered text`, async () => {
    const view = component('./TopicManagement.vue', {
      '@/components/SafeTextarea.vue': safeTextareaTestModule, 'tdesign-vue-next': notifications,
      '@/api/topics': { createTopic: async () => { throw { status: 409 } } },
    })
    const html = await render(view, {}, locale, async state => {
      state.openEditor()
      state.draft.value = { name: topic.name, description: 'My description' }
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
    const request = buildKnowledgeSearchRequest('Example', filters)
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
  const topics = api('../../api/topics.ts'), judgments = api('../../api/knowledge-judgments.ts'), logs = api('../../api/operation-logs.ts')
  await topics.listTopics()
  await topics.createTopic({ name: 'Name', description: 'Description' })
  await topics.updateTopic('t/a', { name: 'Name 2', description: 'Description 2' })
  await topics.deleteTopic('t/a')
  await topics.getTopicBackfill('t/a')
  await topics.retryTopicBackfill('t/a')
  await judgments.setKnowledgeStar('k/a', false)
  await judgments.correctKnowledgeContentType('k/a', 'tutorial', true)
  await judgments.correctKnowledgeTopics('k/a', [])
  await logs.listOperationLogs({ operation_type: 'unstar', page: 2, page_size: 30 })
  assert.deepEqual(calls, [
    { method: 'get', url: '/api/v1/topics' },
    { method: 'post', url: '/api/v1/topics', body: { name: 'Name', description: 'Description' } },
    { method: 'put', url: '/api/v1/topics/t%2Fa', body: { name: 'Name 2', description: 'Description 2' } },
    { method: 'del', url: '/api/v1/topics/t%2Fa' },
    { method: 'get', url: '/api/v1/topics/t%2Fa/backfill' },
    { method: 'post', url: '/api/v1/topics/t%2Fa/backfill/retry', body: {} },
    { method: 'put', url: '/api/v1/knowledge/k%2Fa/star', body: { starred: false } },
    { method: 'put', url: '/api/v1/knowledge/k%2Fa/content-type', body: { content_type: 'tutorial', confirm_remove_body: true } },
    { method: 'put', url: '/api/v1/knowledge/k%2Fa/topics', body: { topic_ids: [] } },
    { method: 'get', url: '/api/v1/operation-logs?operation_type=unstar&page=2&page_size=30' },
  ])
})
