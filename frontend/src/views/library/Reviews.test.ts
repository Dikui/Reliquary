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
const processing = { id: 'uncertain', title: 'Uncertain article', parse_status: 'completed', review_kind: 'processing',  processing_mode: 'full', content_type_uncertain: true, processing_review_status: 'pending', processing_review_content_type: 'tutorial', processing_review_reason: 'Mixes a research finding and instructions', metadata: {} }
type Call = { method: string; url: string; body?: any }
function createReviewAPI(handler: (call: Call) => Promise<any>) {
  const calls: Call[] = []
  const request = Object.fromEntries(['get', 'post'].map(method => [method, (url: string, body?: any) => { const call = { method, url, ...(body === undefined ? {} : { body }) }; calls.push(call); return handler(call) }]))
  const api = evaluate(source('../../api/entry-review.ts'), { '@/utils/request': request })
  return { api, calls }
}
function reviewComponent(api: object) {
  return component('../../components/ReviewActions.vue', { '@/types/entryClassification': { }, '@/api/entry-review': api, '@/utils/security': security, 'tdesign-vue-next': { MessagePlugin: { success() {} } } })
}
function pendingComponent(api: object, child = reviewComponent(api)) {
  return component('./components/PendingReviews.vue', { '@/api/entry-review': api, '@/utils/security': security, '@/components/ReviewActions.vue': moduleOf(child) })
}
const settle = () => new Promise<void>(resolve => setImmediate(resolve))
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
    assert.ok(menuHTML.includes(`>${messages[locale].library.reprocess}<`))
    assert.ok(!menuHTML.includes('name="edit"'))
    const html = await render(modal, {}, locale, async state => { await state.open(item) })
    assert.ok(html.includes('My source'))
    assert.ok(html.includes(messages[locale].personalDocument.deleteConfirm.replace('{title}', 'My source')))
    assert.ok(!html.includes(locale === 'zh-CN' ? '删除后重新导入' : 'import it again'))
  })
  test(`${locale}: intake conflicts show the existing article link and save as new wording from the pending response`, async () => {
    const conflict = { ...intake, id: 'conflict', acquisition: 'model_written', related_entry_id: 'existing/article', metadata: { intake: { reason: 'A conflicting supplement', source_info: { original_url: 'https://example.com/same' } } } }
    const { api, calls } = createReviewAPI(async call => call.method === 'get' ? { success: true, data: [conflict], total: 1 } : { success: true, data: conflict })
    const html = await render(pendingComponent(api), {}, locale, async state => { await state.load() })
    for (const text of [messages[locale].knowledgeReview.conflictReason, messages[locale].knowledgeReview.relatedArticle, '/platform/entries?entry_id=existing%2Farticle', messages[locale].knowledgeReview.saveAsNew, messages[locale].ingestStatus.modelWritten, messages[locale].ingestStatus.modelWrittenHint, messages[locale].articleJudgments.rejectReview]) assert.ok(html.includes(text.replaceAll("'", '&#39;')), text)
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

  test(`${locale}: historical pending entries retain manual collection and confirmed rejection`, async () => {
    let fail = false, removed = false
    const { api, calls } = createReviewAPI(async () => {
      if (fail) throw new Error('Delete failed')
      return { success: true, data: intake }
    })
    const html = await render(reviewComponent(api), { entry: intake, onChanged: (_entry: unknown, deleted: boolean) => { removed = deleted } }, locale, async state => {
      await state.submit('admit_unverified', true)
      assert.deepEqual(calls[0].body, { action: 'admit_unverified', confirmed: true })
      await state.submit('reject')
      assert.equal(calls.length, 1)
      assert.equal(state.confirmation.value, 'reject')
      state.confirmation.value = ''
      assert.equal(removed, false)
      await state.submit('reject')
      fail = true
      await state.submit('reject', true)
      assert.equal(state.confirmation.value, 'reject')
      assert.equal(state.error.value, 'Delete failed')
      fail = false
      await state.submit('reject', true)
      assert.equal(removed, true)
      assert.equal(state.confirmation.value, '')
      assert.deepEqual(calls.at(-1)!.body, { action: 'reject', confirmed: true })
    })
    assert.ok(!html.includes(messages[locale].library.intakeRetry))
    assert.ok(!html.includes('确认不收录'))
    const confirmation = await render(reviewComponent(api), { entry: intake }, locale, async state => { await state.submit('reject') })
    assert.ok(confirmation.includes(messages[locale].knowledgeReview.confirmations.reject.description))
  })

  test(`${locale}: batch management removes new reviews but preserves historical results`, async () => {
    const api = {
      getEntryBatch: async () => ({ success: true, data: { batch_id: 'legacy-review', action: 'review', status: 'completed', total: 1, succeeded: 1, failed: 0, skipped: 0, pending: 0, running: 0, warnings: 0, items: [], page: 1, page_size: 20 } }),
      submitEntryBatch: async () => assert.fail('Historical results must not create a review task'),
    }
    const dialog = component('../../components/EntryBatchDialog.vue', { '@/api/entry-batches': api, '@/api/library': { listLibraries: async () => ({ success: true, data: [] }) } })
    const html = await render(dialog, { visible: true, batchId: 'legacy-review', libraries: [] }, locale, async state => {
      state.ready.value = true
      assert.ok(!state.actions.includes('review'))
      await state.refresh()
    })
    assert.ok(html.includes(messages[locale].entryBatch.actions.review))
    assert.ok(html.includes('batch_id=legacy-review'))
  })

}



test('pending paging clamps after the last record is resolved and requests the correct kind', async () => {
  const { api, calls } = createReviewAPI(async call => ({ success: true, data: call.url.includes('page=2&') ? [] : [intake], total: 1 }))
  await render(pendingComponent(api), {}, 'en-US', async state => { state.page.value = 2; await state.reviewed(); assert.equal(state.page.value, 1) })
  assert.deepEqual(calls.map(call => call.url), ['/api/v1/entries/pending?page=2&page_size=20&kind=intake', '/api/v1/entries/pending?page=1&page_size=20&kind=intake'])
})

for (const locale of ['zh-CN', 'en-US'] as const) {

}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: pending page loads intake only and has no image-completion tab`, async () => {
    const { api, calls } = createReviewAPI(async () => ({ success: true, data: [], total: 0 }))
    const html = await render(pendingComponent(api), {}, locale, async state => { await state.load() })
    assert.equal(calls.length, 1)
    assert.ok(calls[0].url.includes('kind=intake'))
    assert.ok(html.includes(messages[locale].knowledgeReview.emptyIntake))
    assert.ok(!html.includes('value="incomplete"'))
    assert.ok(!source('./components/PendingReviews.vue').includes('listIncomplete'))
    assert.ok(!source('./components/PendingReviews.vue').includes('LibraryReviewAction'))
  })
}
