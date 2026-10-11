import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import { CONTENT_TYPES } from '../../types/entryClassification'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zh, 'en-US': en }
const moduleOf = (value: unknown) => ({ __esModule: true, default: value })
const empty = moduleOf(defineComponent({ setup: () => () => null }))
const security = { isValidURL: (url: string) => /^https?:\/\/[\w.-]+/.test(url) }
function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => name.endsWith('.png') ? '/image.png' : modules[name] ?? require(name), module, module.exports)
  return module.exports
}
function source(path: string) { return readFileSync(new URL(path, import.meta.url), 'utf8') }
function component(path: string, modules: Record<string, unknown>) {
  const filename = path.split('/').at(-1)!
  const { descriptor, errors } = parse(source(path), { filename })
  assert.deepEqual(errors, [])
  const script = compileScript(descriptor, { id: filename })
  const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const value = evaluate(script.content, modules).default
  value.ssrRender = evaluate(template.code, modules).ssrRender
  return value
}
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
  const stub = defineComponent({ props: ['visible', 'label', 'description', 'message'], setup: (props, { attrs, slots }) => () => props.visible === false ? null : h('div', attrs, [props.label, props.description, props.message, slots.default?.(), slots.operation?.()]) })
  for (const name of ['t-tabs', 't-tab-panel', 't-button', 't-select', 't-input', 't-dialog', 't-empty', 't-loading', 't-pagination', 't-alert', 't-tag', 't-icon', 't-tooltip', 't-checkbox', 't-popup']) app.component(name, stub)
  const errors: unknown[] = [], warnings: string[] = []
  app.config.errorHandler = cause => errors.push(cause)
  app.config.warnHandler = warning => warnings.push(warning)
  const html = await renderToString(app)
  assert.deepEqual(errors, []); assert.deepEqual(warnings, [])
  return html
}
const intake = { id: 'missing-url', title: 'Missing link article', review_kind: 'intake', parse_status: 'rejected', metadata: { intake: { source: 'upload', decision: 'rejected', reason: '缺少原始链接', source_info: {} } } }
const processing = { id: 'uncertain', title: 'Uncertain article', parse_status: 'completed', review_kind: 'processing', content_type: 'tutorial', processing_mode: 'full', content_type_uncertain: true, processing_review_status: 'pending', processing_review_content_type: 'tutorial', processing_review_reason: 'Mixes a research finding and instructions', metadata: {} }
type Call = { method: string; url: string; body?: any }
function createReviewAPI(handler: (call: Call) => Promise<any>) {
  const calls: Call[] = []
  const request = Object.fromEntries(['get', 'post'].map(method => [method, (url: string, body?: any) => { const call = { method, url, ...(body === undefined ? {} : { body }) }; calls.push(call); return handler(call) }]))
  const api = evaluate(source('../../api/entry-review.ts'), { '@/utils/request': request })
  return { api, calls }
}
function reviewComponent(api: object) {
  return component('../../components/ReviewActions.vue', { '@/types/entryClassification': { CONTENT_TYPES }, '@/api/entry-review': api, '@/utils/security': security, 'tdesign-vue-next': { MessagePlugin: { success() {} } } })
}
function pendingComponent(api: object, child = reviewComponent(api)) {
  return component('./components/PendingReviews.vue', { '@/api/entry-review': api, '@/utils/security': security, '@/components/ReviewActions.vue': moduleOf(child) })
}
const settle = () => new Promise<void>(resolve => setImmediate(resolve))

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: pending tabs load records through the real API wrapper and show intake actions`, async () => {
    const { api, calls } = createReviewAPI(async () => ({ success: true, data: [intake], total: 1 }))
    const html = await render(pendingComponent(api), {}, locale, async state => { await state.load() })
    assert.deepEqual(calls, [{ method: 'get', url: '/api/v1/entries/pending?page=1&page_size=20&kind=intake' }])
    for (const text of [messages[locale].knowledgeReview.intake, messages[locale].knowledgeReview.processing, intake.title, '缺少原始链接', ...['supplement_url', 'admit_unverified', 'convert_to_mine'].map(key => messages[locale].knowledgeReview.actions[key as 'supplement_url'])]) assert.ok(html.includes(text), text)
  })
  test(`${locale}: processing tab loads its kind and the model type and reason`, async () => {
    const { api, calls } = createReviewAPI(async () => ({ success: true, data: [processing], total: 1 }))
    const html = await render(pendingComponent(api), {}, locale, async state => { state.changeKind('processing'); await settle() })
    assert.deepEqual(calls, [{ method: 'get', url: '/api/v1/entries/pending?page=1&page_size=20&kind=processing' }])
    for (const text of [processing.title, processing.processing_review_reason, messages[locale].knowledgeBase.contentTypes.tutorial, messages[locale].knowledgeReview.actions.confirm, messages[locale].knowledgeReview.actions.correct, messages[locale].knowledgeReview.actions.ignore]) assert.ok(html.includes(text), text)
  })
  test(`${locale}: intake supplement and confirmed no-link actions use records loaded by the pending API`, async () => {
    for (const action of ['supplement_url', 'admit_unverified', 'convert_to_mine'] as const) {
      const { api, calls } = createReviewAPI(async call => call.method === 'get' ? { success: true, data: [intake], total: 1 } : { success: true, data: { ...intake, source_unverified: action === 'admit_unverified', processing_mode: 'full' } })
      const record = reactive({ ...intake })
      const page = await api.listPendingEntry({ kind: 'intake', page: 1, page_size: 20 })
      Object.assign(record, page.data[0])
      let changed = false, removed = false
      await render(reviewComponent(api), { entry: record, onChanged: (_value: unknown, deleted: boolean) => { changed = true; removed = deleted } }, locale, async state => {
        if (action === 'supplement_url') { state.sourceDraft.value.original_url = 'https://example.com/source'; await state.submit(action) }
        else { await state.submit(action); assert.equal(calls.length, 1); assert.equal(state.confirmation.value, action); await state.submit(action, true); assert.equal(state.confirmation.value, '') }
      })
      assert.equal(changed, true); assert.equal(removed, action === 'convert_to_mine')
      assert.deepEqual(calls[1], { method: 'post', url: '/api/v1/entries/missing-url/review', body: action === 'supplement_url' ? { action, source_info: { original_url: 'https://example.com/source' } } : { action, confirmed: true } })
    }
  })
  test(`${locale}: processing confirm and correction require body removal confirmation, ignore preserves the article`, async () => {
    for (const action of ['confirm', 'correct', 'ignore'] as const) {
      const { api, calls } = createReviewAPI(async call => call.method === 'get' ? { success: true, data: [processing], total: 1 } : { success: true, data: { ...processing, processing_review_status: action === 'confirm' ? 'confirmed' : action === 'correct' ? 'corrected' : 'ignored' } })
      const loaded = await api.listPendingEntry({ kind: 'processing', page: 1, page_size: 20 })
      await render(reviewComponent(api), { entry: loaded.data[0] }, locale, async state => {
        if (action === 'correct') state.typeDraft.value = 'reference'
        await state.submit(action)
        if (action !== 'ignore') { assert.equal(calls.length, 1); assert.equal(state.confirmation.value, action); await state.submit(action, true) }
      })
      assert.deepEqual(calls[1], { method: 'post', url: '/api/v1/entries/uncertain/processing-review', body: { action, ...(action === 'correct' ? { content_type: 'reference' } : {}), confirm_remove_body: action !== 'ignore' } })
    }
  })
  test(`${locale}: no-link action failures retain the confirmation and draft, invalid links never send a request`, async () => {
    const { api, calls } = createReviewAPI(async () => { throw new Error('Temporary server failure') })
    const html = await render(reviewComponent(api), { entry: intake }, locale, async state => {
      state.sourceDraft.value.original_url = 'javascript:bad'
      await state.submit('supplement_url')
      assert.equal(calls.length, 0)
      assert.equal(state.error.value, messages[locale].knowledgeReview.invalidLink)
      state.sourceDraft.value.original_url = 'https://example.com/kept'
      await state.submit('admit_unverified'); await state.submit('admit_unverified', true)
      assert.equal(state.confirmation.value, 'admit_unverified'); assert.equal(state.sourceDraft.value.original_url, 'https://example.com/kept')
    })
    assert.ok(html.includes('Temporary server failure')); assert.ok(html.includes(messages[locale].knowledgeReview.confirmations.admit_unverified.description))
  })
  test(`${locale}: source unverified processing corrections preserve full text without removal confirmation`, async () => {
    const { api, calls } = createReviewAPI(async () => ({ success: true, data: { ...processing, source_unverified: true, processing_mode: 'full' } }))
    await render(reviewComponent(api), { entry: { ...processing, source_unverified: true } }, locale, async state => { state.typeDraft.value = 'reference'; await state.submit('correct'); assert.equal(state.confirmation.value, '') })
    assert.equal(calls.length, 1); assert.equal(calls[0].body.confirm_remove_body, false)
  })
}

test('pending requests ignore a stale response and retain records after refresh failures', async () => {
  let resolveOld!: (value: unknown) => void, fail = false
  const { api } = createReviewAPI(async call => {
    if (fail) throw new Error('Refresh failed')
    if (call.url.includes('kind=intake')) return new Promise(resolve => { resolveOld = resolve })
    return { success: true, data: [processing], total: 1 }
  })
  const html = await render(pendingComponent(api), {}, 'en-US', async state => {
    const pending = state.load(); state.changeKind('processing'); await settle()
    resolveOld({ success: true, data: [intake], total: 1 }); await pending
    assert.equal(state.records.value[0].id, processing.id)
    fail = true; await state.load(); assert.equal(state.records.value[0].id, processing.id)
  })
  assert.ok(html.includes('Refresh failed')); assert.ok(html.includes(processing.title)); assert.ok(!html.includes(intake.title))
})

test('pending paging clamps after the last record is resolved and requests the correct kind', async () => {
  const { api, calls } = createReviewAPI(async call => ({ success: true, data: call.url.includes('page=2&') ? [] : [intake], total: 1 }))
  await render(pendingComponent(api), {}, 'en-US', async state => { state.page.value = 2; await state.reviewed(); assert.equal(state.page.value, 1) })
  assert.deepEqual(calls.map(call => call.url), ['/api/v1/entries/pending?page=2&page_size=20&kind=intake', '/api/v1/entries/pending?page=1&page_size=20&kind=intake'])
})

test('the pending API defaults to both kinds and escapes resource IDs in review actions', async () => {
  const { api, calls } = createReviewAPI(async () => ({ success: true }))
  await api.listPendingEntry({ page: 2, page_size: 50 })
  await api.resolveIntakeReview('a/b', { action: 'supplement_url', source_info: { original_url: 'https://example.com' } })
  await api.resolveProcessingReview('a/b', { action: 'correct', content_type: 'reference', confirm_remove_body: true })
  assert.deepEqual(calls.map(call => call.url), ['/api/v1/entries/pending?page=2&page_size=50', '/api/v1/entries/a%2Fb/review', '/api/v1/entries/a%2Fb/processing-review'])
})

test('source unverified and review fields survive the API, actual Pinia store and detail loading hook', async () => {
  const pinia = require('pinia')
  pinia.setActivePinia(pinia.createPinia())
  const store = evaluate(source('../../stores/entry.ts'), {}).entryStore
  const records = [{ ...processing, source_kind: 'article', source_unverified: true, updated_at: '2026-10-11T00:00:00Z', type: 'manual', file_name: 'source.md' }, { id: 'verified', source_kind: 'article', updated_at: '2026-10-11T00:00:00Z' }]
  const requests: string[] = []
  const api = evaluate(source('../../api/knowledge-base.ts'), { '../utils/request': { get: async (url: string) => { requests.push(url); if (url.includes('knowledge-bases/')) return { success: true, data: records, total: 2 }; if (url.includes('/chunks/')) return { success: true, data: [], total: 0 }; return { success: true, data: records.find(record => url.endsWith(record.id)) } } } })
  const hook = evaluate(source('../../composables/useKnowledgeBase.ts'), {
    '../utils/index': { formatStringDate: () => '2026-10-11' }, 'tdesign-vue-next': { MessagePlugin: {} }, '@/api/knowledge-base': api,
    '@/stores/entry': { entryStore: store }, 'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) }, '@/stores/resources': { useResourcesStore: () => ({}) }, './useEntryDuplicates': { useEntryDuplicates: () => ({}) },
  }).default('kb-reviews')
  await hook.loadEntries()
  assert.equal(store().cardList[0].source_unverified, true)
  hook.getCardDetails({ id: processing.id }); await settle()
  for (const field of ['source_unverified', 'content_type_uncertain', 'processing_review_status', 'processing_review_content_type', 'processing_review_reason']) assert.equal(hook.details[field], records[0][field as keyof typeof records[0]], field)
  const classification = component('../../components/EntryClassification.vue', { './FollowToggle.vue': empty, '@/types/entryClassification': { CONTENT_TYPES }, '@/utils/security': security })
  const listModules = { '@/utils/files': { formatFileSize: () => '', getFileIcon: () => 'file' }, './DocumentActionMenu.vue': empty, '@/components/EntryProcessingTimeline.vue': empty, '@/components/EntryClassification.vue': moduleOf(classification), '@/components/EntryStarToggle.vue': empty }
  for (const locale of ['zh-CN', 'en-US'] as const) {
    assert.ok((await render(classification, { entry: hook.details }, locale)).includes(messages[locale].knowledgeReview.sourceUnverified))
    for (const file of ['./components/DocumentListView.vue', './components/DocumentCardView.vue']) assert.ok((await render(component(file, listModules), { items: hook.cardList.value, selectedIds: new Set(), batchMode: false, traceAvailableById: {}, traceVisibleIds: {} }, locale)).includes(messages[locale].knowledgeReview.sourceUnverified))
  }
  hook.getCardDetails({ id: 'verified' }); await settle()
  assert.equal(hook.details.source_unverified, false); assert.equal(hook.details.processing_review_status, '')
  assert.ok(requests.includes('/api/v1/knowledge-bases/kb-reviews/entries?page=1&page_size=35'))
})

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: converted and directly indexed vault documents retain source labels through API and store loading`, async () => {
    const document = { id: 'converted-mine', title: 'Converted note', source_kind: 'mine', source: 'mine/Converted.md', vault_path: 'mine/Converted.md', type: 'manual', channel: 'api', file_type: 'md', file_name: 'Converted.md', parse_status: 'completed', updated_at: '2026-10-11T00:00:00Z', metadata: null }
    const { api: review, calls } = createReviewAPI(async () => ({ success: true, data: document }))
    const converted = (await review.resolveIntakeReview('original-upload', { action: 'convert_to_mine', confirmed: true })).data
    assert.equal(calls[0].url, '/api/v1/entries/original-upload/review')
    const records = [converted, { ...document, id: 'direct-mine', title: 'Direct note', vault_path: 'mine/Direct.md', source: 'mine/Direct.md' }, { ...document, id: 'report', source_kind: 'report', vault_path: 'reports/Report.md', source: 'reports/Report.md' }, { ...document, id: 'article', source_kind: 'article', channel: 'web' }]
    const pinia = require('pinia'); pinia.setActivePinia(pinia.createPinia())
    const store = evaluate(source('../../stores/entry.ts'), {}).entryStore
    const api = evaluate(source('../../api/knowledge-base.ts'), { '../utils/request': { get: async () => ({ success: true, data: records, total: records.length }) } })
    const hook = evaluate(source('../../composables/useKnowledgeBase.ts'), {
      '../utils/index': { formatStringDate: () => '2026-10-11' }, 'tdesign-vue-next': { MessagePlugin: {} }, '@/api/knowledge-base': api,
      '@/stores/entry': { entryStore: store }, 'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) }, '@/stores/resources': { useResourcesStore: () => ({}) }, './useEntryDuplicates': { useEntryDuplicates: () => ({}) },
    }).default('kb-reviews')
    await hook.loadEntries()
    const modules = { '@/utils/files': { formatFileSize: () => '', getFileIcon: () => 'file' }, './DocumentActionMenu.vue': empty, '@/components/EntryProcessingTimeline.vue': empty, '@/components/EntryClassification.vue': empty, '@/components/EntryStarToggle.vue': empty }
    for (const file of ['./components/DocumentListView.vue', './components/DocumentCardView.vue']) {
      const view = component(file, modules)
      for (const record of hook.cardList.value) {
        const html = await render(view, { items: [record], selectedIds: new Set(), batchMode: false, traceAvailableById: {}, traceVisibleIds: {} }, locale)
        if (record.source_kind === 'article') {
          assert.ok(html.includes(file.includes('List') ? messages[locale].knowledgeBase.channelManual : messages[locale].knowledgeBase.typeManual))
        } else {
          assert.ok(html.includes(messages[locale].knowledgeBase.sourceKinds[record.source_kind as 'mine' | 'report']), record.id)
          assert.ok(!html.includes(messages[locale].knowledgeBase.typeManual), record.id)
          assert.ok(!html.includes(`>${messages[locale].knowledgeBase.channelManual}<`), record.id)
        }
      }
    }
  })
}
