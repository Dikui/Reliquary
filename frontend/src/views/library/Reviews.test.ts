import * as bodyReference from '../../utils/bodyReference'
import * as libraryScopeHelpers from '../../utils/libraryScope'
import assert from 'node:assert/strict'
import * as entrySourceLabels from '../../utils/entrySourceLabels'
import * as markdownPlainText from '../../utils/markdownPlainText'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch, reactive, ref } from 'vue'
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
  new Function('require', 'module', 'exports', output)((name: string) => name === '@/components/SelectionCheckbox.vue'
    ? moduleOf(component('../../components/SelectionCheckbox.vue', {}))
    : name === '@/utils/entrySourceLabels' ? entrySourceLabels : name === '@/utils/markdownPlainText' ? markdownPlainText : name.endsWith('.png') ? '/image.png' : (name === '@/utils/bodyReference' ? bodyReference : name === '@/utils/libraryScope' ? libraryScopeHelpers : name === '@/composables/useLibraryScope' ? { useLibraryScope: () => ref([]) } : (name.endsWith('LibraryScopeSelect.vue') ? { __esModule: true, default: defineComponent({ setup: () => () => null }) } : modules[name])) ?? require(name), module, module.exports)
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
  for (const name of ['t-tabs', 't-tab-panel', 't-button', 't-select', 't-input', 't-dialog', 't-empty', 't-loading', 't-pagination', 't-alert', 't-tag', 't-icon', 't-tooltip', 't-checkbox', 't-popup', 't-popconfirm']) app.component(name, stub)
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
function createIngestAPI(handler: (call: Call) => Promise<any>) {
  const calls: Call[] = []
  const request = Object.fromEntries(['get', 'post'].map(method => [method, (url: string, body?: any) => { const call = { method, url, ...(body === undefined ? {} : { body }) }; calls.push(call); return handler(call) }]))
  return { api: evaluate(source('../../api/ingest.ts'), { '@/utils/request': request }), calls }
}
function reviewComponent(api: object) {
  return component('../../components/ReviewActions.vue', { '@/types/entryClassification': { CONTENT_TYPES }, '@/api/entry-review': api, '@/utils/security': security, 'tdesign-vue-next': { MessagePlugin: { success() {} } } })
}
function missingComponent() { return component('../../components/MissingMaterials.vue', { '@/utils/security': security }) }
function incompleteActions(api: object) {
  return component('../../components/IncompleteActions.vue', { '@/api/ingest': api, 'tdesign-vue-next': { MessagePlugin: { success() {} } } })
}
function pendingComponent(api: object, child = reviewComponent(api), ingest = createIngestAPI(async () => ({ success: true, data: [], total: 0 })).api, incompleteChild = incompleteActions(ingest)) {
  return component('./components/PendingReviews.vue', { '@/api/entry-review': api, '@/api/ingest': ingest, '@/utils/security': security, '@/components/ReviewActions.vue': moduleOf(child), '@/components/MissingMaterials.vue': moduleOf(missingComponent()), '@/components/IncompleteActions.vue': moduleOf(incompleteChild) })
}
const settle = () => new Promise<void>(resolve => setImmediate(resolve))
const incomplete = {
  article_id: 'needs-files', title: 'Materials to complete', url: 'https://example.com/needs-files', acquisition: 'extracted', external_completion_attempted: true,
  missing: [
    { type: 'image', source_url: 'https://example.com/source.png', reason: 'Image required a login', api_key_name: 'Research Bot', submitted_at: '2026-10-12T03:04:05Z' },
    { type: 'image', source_url: 'https://example.com/figure.png', reason: 'Image request was blocked', api_key_name: 'Research Bot', submitted_at: '2026-10-12T03:04:05Z' },
  ],
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: rejected personal documents show their source and approve without a URL`, async () => {
    const mine = { ...intake, source_kind: 'mine', metadata: { intake: { decision: 'rejected', reason: 'Needs user decision', source_info: {} } } }
    const { api, calls } = createReviewAPI(async call => ({ success: true, data: call.method === 'get' ? [mine] : mine, total: 1 }))
    const html = await render(pendingComponent(api), {}, locale, async state => { await state.load() })
    assert.ok(html.includes(messages[locale].library.sourceKinds.mine))
    assert.ok(html.includes(messages[locale].articleJudgments.approveReview))
    assert.ok(!html.includes(messages[locale].knowledgeReview.missingLinkHint))
    assert.ok(!html.includes(messages[locale].knowledgeReview.actions.supplement_url))
    await render(reviewComponent(api), { entry: mine }, locale, async state => { await state.submit('approve') })
    assert.deepEqual(calls.at(-1), { method: 'post', url: '/api/v1/entries/missing-url/review', body: { action: 'approve', source_info: {} } })
  })
  test(`${locale}: personal document menu keeps deletion independent while offering normal reprocessing`, async () => {
    const shared = component('../../components/DocumentDeleteConfirm.vue', {})
    const setup = shared.setup
    shared.setup = (props: object, context: object) => { const state = setup(props, context); onServerPrefetch(() => state.open()); return state }
    const modal = component('./components/DocumentMenuDelete.vue', {
      '@/components/DocumentDeleteConfirm.vue': moduleOf(shared),
      '@/api/library': { deleteDocument: () => assert.fail('Opening the menu never deletes its document') },
      'tdesign-vue-next': { MessagePlugin: { success() {} } },
    })
    const menu = component('./components/DocumentActionMenu.vue', {})
    const item = { id: 'mine', title: 'My source', source_kind: 'mine', parse_status: 'completed' }
    const menuHTML = await render(menu, { item, traceVisible: false }, locale)
    assert.ok(menuHTML.includes(messages[locale].personalDocument.delete))
    assert.ok(!menuHTML.includes(messages[locale].personalDocument.deleteConfirm.replace('{title}', 'My source')), 'deletion confirmation remains in its independent dialog')
    assert.ok(menuHTML.includes(`>${messages[locale].library.rebuildDocument}<`))
    assert.ok(!menuHTML.includes('name="edit"'))
    const html = await render(modal, {}, locale, async state => { await state.open(item) })
    assert.ok(html.includes('My source'))
    assert.ok(html.includes(messages[locale].personalDocument.deleteConfirm.replace('{title}', 'My source')))
    assert.ok(!html.includes(locale === 'zh-CN' ? '删除后重新导入' : 'import it again'))
  })
  test(`${locale}: incomplete articles and personal documents load with the correct completion instructions and shared action copy`, async () => {
    const records = [
      { ...incomplete, kind: 'article' },
      { ...incomplete, library_id: 'mine-library', library_template: 'own', article_id: 'personal-files', title: 'My incomplete note', url: '', acquisition: 'verbatim' },
    ]
    const { api: review } = createReviewAPI(async () => ({ success: true, data: [], total: 0 }))
    const { api: ingest, calls } = createIngestAPI(async () => ({ success: true, data: records, total: records.length }))
    let loadedMine: typeof records[number] | undefined
    const html = await render(pendingComponent(review, reviewComponent(review), ingest), {}, locale, async state => {
      state.changeKind('incomplete'); await settle()
      assert.deepEqual(state.incompleteRecords.value, records)
      loadedMine = state.incompleteRecords.value[1]
    })
    assert.deepEqual(calls, [{ method: 'get', url: '/api/v1/ingest/incomplete?page=1&page_size=20&include_attempted=true' }])
    assert.ok(html.includes(records[0].title)); assert.ok(html.includes(records[1].title))
    assert.ok(html.includes(messages[locale].library.sourceKinds.mine))
    const instructions = locale === 'zh-CN'
      ? '按资料 ID 补交图片'
      : 'material ID'
    assert.ok(html.includes(instructions))
    assert.ok(messages[locale].ingestStatus.incompleteHint.includes(instructions))
    assert.ok(html.includes(messages[locale].ingestStatus.attemptedHint))
    assert.ok(!/该文章|This article/.test(messages[locale].ingestStatus.attemptedHint))
    for (const action of ['accept', 'discard'] as const) {
      const confirmationHTML = await render(incompleteActions(ingest), { articleId: loadedMine!.article_id, title: loadedMine!.title, allowDiscard: true }, locale, async state => { state.request(action) })
      assert.ok(confirmationHTML.includes(loadedMine!.title))
      assert.ok(confirmationHTML.includes(messages[locale].ingestStatus[action === 'accept' ? 'acceptDescription' : 'discardDescription']))
      assert.ok(!confirmationHTML.includes(locale === 'zh-CN' ? '这篇文章' : 'this article'))
    }
    const emptyIngest = createIngestAPI(async () => ({ success: true, data: [], total: 0 })).api
    const emptyHTML = await render(pendingComponent(review, reviewComponent(review), emptyIngest), {}, locale, async state => { state.changeKind('incomplete'); await settle() })
    assert.ok(emptyHTML.includes(messages[locale].knowledgeReview.emptyIncomplete))
  })

  test(`${locale}: incomplete records without an external attempt hide the attempt notice and refresh after an action`, async () => {
    let completed = false, changed = 0
    const { api: review } = createReviewAPI(async () => ({ success: true, data: [], total: 0 }))
    const { api: ingest, calls } = createIngestAPI(async call => {
      if (call.method === 'post') { completed = true; return { success: true, data: { article_id: incomplete.article_id } } }
      return { success: true, data: completed ? [] : [{ ...incomplete, external_completion_attempted: false }], total: completed ? 0 : 1 }
    })
    const child = incompleteActions(ingest)
    const setup = child.setup
    child.setup = (props: any, context: any) => {
      const state = setup(props, context)
      onServerPrefetch(async () => { state.request('accept'); await state.confirm(); await settle() })
      return state
    }
    const html = await render(pendingComponent(review, reviewComponent(review), ingest, child), { onChanged: () => changed++ }, locale, async state => { state.changeKind('incomplete'); await settle() })
    assert.equal(changed, 1)
    assert.deepEqual(calls.map(call => call.url), ['/api/v1/ingest/incomplete?page=1&page_size=20&include_attempted=true', '/api/v1/ingest/needs-files/accept-incomplete', '/api/v1/ingest/incomplete?page=1&page_size=20&include_attempted=true'])
    assert.ok(!html.includes(messages[locale].ingestStatus.attemptedTitle)); assert.ok(!html.includes(messages[locale].ingestStatus.attemptedHint))
  })
  test(`${locale}: incomplete accept and discard require confirmation, cancellation sends nothing and confirmed actions call the ingest API`, async () => {
    for (const action of ['accept', 'discard'] as const) {
      const { api, calls } = createIngestAPI(async call => call.method === 'get' ? { success: true, data: [incomplete], total: 1 } : { success: true, data: { article_id: incomplete.article_id } })
      const loaded = (await api.listIncompleteArticles({ page: 1, page_size: 20 })).data[0]
      const confirmationHTML = await render(incompleteActions(api), { articleId: loaded.article_id, title: loaded.title, allowDiscard: true }, locale, async state => { state.request(action); assert.equal(state.confirmation.value, action); assert.equal(calls.length, 1) })
      assert.ok(confirmationHTML.includes(loaded.title))
      assert.ok(confirmationHTML.includes(messages[locale].ingestStatus[action === 'accept' ? 'acceptDescription' : 'discardDescription']))
      let changed: boolean | undefined
      await render(incompleteActions(api), { articleId: loaded.article_id, title: loaded.title, allowDiscard: true, onChanged: (removed: boolean) => { changed = removed } }, locale, async state => {
        state.request(action); state.cancel(); await state.confirm(); assert.equal(calls.length, 1)
        state.request(action); await state.confirm(); assert.equal(state.confirmation.value, '')
      })
      assert.equal(changed, action === 'discard')
      assert.deepEqual(calls[1], { method: 'post', url: `/api/v1/ingest/needs-files/${action === 'accept' ? 'accept-incomplete' : 'discard'}`, ...(action === 'discard' ? { body: {} } : {}) })
    }
  })
  test(`${locale}: failed incomplete actions keep confirmation and allow retry, detail actions hide discard by default`, async () => {
    const { api, calls } = createIngestAPI(async () => ({ success: false, message: 'Please retry later' }))
    const html = await render(incompleteActions(api), { articleId: incomplete.article_id, title: incomplete.title }, locale, async state => {
      state.request('discard'); assert.equal(state.confirmation.value, '')
      state.request('accept'); await state.confirm(); assert.equal(state.confirmation.value, 'accept'); assert.equal(state.busy.value, false)
    })
    assert.equal(calls.length, 1); assert.ok(html.includes('Please retry later')); assert.ok(!html.includes(`>${messages[locale].ingestStatus.discard}<`))
  })
  test(`${locale}: intake conflicts show the existing article link and save as new wording from the pending response`, async () => {
    const conflict = { ...intake, id: 'conflict', acquisition: 'model_written', related_article_id: 'existing/article', metadata: { intake: { reason: 'A conflicting supplement', source_info: { original_url: 'https://example.com/same' } } } }
    const { api, calls } = createReviewAPI(async call => call.method === 'get' ? { success: true, data: [conflict], total: 1 } : { success: true, data: conflict })
    const html = await render(pendingComponent(api), {}, locale, async state => { await state.load() })
    for (const text of [messages[locale].knowledgeReview.conflictReason, messages[locale].knowledgeReview.relatedArticle, '/platform/entries?entry_id=existing%2Farticle', messages[locale].knowledgeReview.saveAsNew, messages[locale].ingestStatus.modelWritten, messages[locale].ingestStatus.modelWrittenHint, messages[locale].library.intakeDiscard]) assert.ok(html.includes(text.replaceAll("'", '&#39;')), text)
    assert.ok(!html.includes(messages[locale].articleJudgments.approveReview))
    await render(reviewComponent(api), { entry: (await api.listPendingEntry({ kind: 'intake', page: 1, page_size: 20 })).data[0] }, locale, async state => { await state.submit('approve') })
    assert.deepEqual(calls.at(-1), { method: 'post', url: '/api/v1/entries/conflict/review', body: { action: 'approve', source_info: { original_url: 'https://example.com/same' } } })
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

}



test('pending paging clamps after the last record is resolved and requests the correct kind', async () => {
  const { api, calls } = createReviewAPI(async call => ({ success: true, data: call.url.includes('page=2&') ? [] : [intake], total: 1 }))
  await render(pendingComponent(api), {}, 'en-US', async state => { state.page.value = 2; await state.reviewed(); assert.equal(state.page.value, 1) })
  assert.deepEqual(calls.map(call => call.url), ['/api/v1/entries/pending?page=2&page_size=20&kind=intake', '/api/v1/entries/pending?page=1&page_size=20&kind=intake'])
})

test('pending toolbar refresh and error retry notify navigation and load the current queue again', async () => {
  let fail = true, refreshed = 0
  const { api } = createReviewAPI(async () => ({ success: true, data: [], total: 0 }))
  const { api: ingest, calls } = createIngestAPI(async () => {
    if (fail) throw new Error('Incomplete queue unavailable')
    return { success: true, data: [incomplete], total: 1 }
  })
  await render(pendingComponent(api, reviewComponent(api), ingest), { onRefreshed: () => refreshed++ }, 'en-US', async state => {
    state.changeKind('incomplete'); await settle()
    assert.equal(state.error.value, 'Incomplete queue unavailable')
    fail = false; await state.refresh()
    assert.equal(state.error.value, ''); assert.equal(state.incompleteRecords.value[0].article_id, incomplete.article_id)
    await state.refresh()
    state.setBusy('another-operation', true); await state.refresh()
  })
  assert.equal(refreshed, 2)
  assert.equal(calls.length, 3)
  assert.ok(calls.every(call => call.url.endsWith('include_attempted=true')))
})





test('shared incomplete actions honor external disabled state before either confirmation or HTTP submission', async () => {
  const { api, calls } = createIngestAPI(async () => ({ success: true }))
  await render(incompleteActions(api), { articleId: incomplete.article_id, allowDiscard: true, disabled: true }, 'en-US', async state => {
    state.request('accept'); assert.equal(state.confirmation.value, '')
    state.confirmation.value = 'discard'; await state.confirm()
  })
  assert.deepEqual(calls, [])
})





for (const locale of ['zh-CN', 'en-US'] as const) {

}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: pending page exposes only intake and incomplete tabs`, async () => {
    const { api, calls } = createReviewAPI(async () => ({ success: true, data: [], total: 0 }))
    const html = await render(pendingComponent(api), {}, locale, async state => {
      await state.load()
      const count = calls.length
      state.changeKind('processing')
      await settle()
      assert.equal(state.kind.value, 'intake')
      assert.equal(calls.length, count)
    })
    assert.ok(html.includes('value="intake"'))
    assert.ok(html.includes('value="incomplete"'))
    assert.ok(!html.includes('value="processing"'))
    assert.equal((source('./components/PendingReviews.vue').match(/<t-tab-panel\b/g) || []).length, 2)
  })
}
