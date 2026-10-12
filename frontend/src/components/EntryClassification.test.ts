import assert from 'node:assert/strict'
import * as entrySourceLabels from '../utils/entrySourceLabels'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zhCN from '../i18n/locales/zh-CN'
import enUS from '../i18n/locales/en-US'
import { CONTENT_TYPES } from '../types/entryClassification'
import { safeTextareaTestModule } from './__tests__/safeTextareaStub'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const emptyModule = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => name === '@/components/SelectionCheckbox.vue'
    ? { __esModule: true, default: component('./SelectionCheckbox.vue', {}) }
    : name === '@/utils/entrySourceLabels' ? entrySourceLabels : name.endsWith('.css') ? {} : name.endsWith('.png') ? '/image.png' : modules[name] ?? require(name), module, module.exports)
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
const classification = component('./EntryClassification.vue', {
  './FollowToggle.vue': emptyModule,
  '@/types/entryClassification': { CONTENT_TYPES }, '@/utils/security': { isValidURL: (value: string) => /^https?:\/\//.test(value) },
})
const reviewModule = { __esModule: true, default: component('./ReviewActions.vue', { '@/types/entryClassification': { CONTENT_TYPES }, '@/api/entry-review': {}, '@/utils/security': { isValidURL: (value: string) => /^https?:\/\//.test(value) }, 'tdesign-vue-next': { MessagePlugin: {} } }) }
const classificationModule = { __esModule: true, default: classification }
async function render(value: any, props: Record<string, unknown>, locale: keyof typeof messages) {
  const app = createSSRApp(value, props)
  app.use(createI18n({ legacy: false, locale, messages }))
  const stub = defineComponent({ props: ['visible'], setup(props, { attrs, slots }) { return () => props.visible === false ? null : h('div', attrs, [slots.header?.(), slots.default?.()]) } })
  for (const name of ['t-drawer', 't-button', 't-icon', 't-tag', 't-tooltip', 't-loading', 't-select', 't-input', 't-textarea', 't-checkbox', 't-popconfirm', 't-popup', 't-empty', 't-switch', 't-pagination', 't-radio', 't-dialog']) app.component(name, stub)
  const errors: unknown[] = []; const warnings: string[] = []
  app.config.errorHandler = error => errors.push(error); app.config.warnHandler = warning => warnings.push(warning)
  const html = await renderToString(app)
  assert.deepEqual(errors, []); assert.deepEqual(warnings, [])
  return html
}
const article = {
  id: 'article', file_name: 'Example.md', type: 'manual', parse_status: 'completed', content_type: 'event', processing_mode: 'full',
  structured_summary: { overview: 'A product was announced.', points: ['Launch date', 'Main feature', 'Availability'], key_terms: ['Example Company', '42'] },
  entities: [{ id: 'entity-1', canonical_name: 'Example Company', entity_type: 'company' }], topics: [{ id: 'topic-1', name: 'Product releases', description: 'New product announcements' }], md: [], total: 0, metadata: {},
}
const listModules = {
  '@/utils/files': { formatFileSize: () => '100 B', getFileIcon: () => 'file' }, './DocumentActionMenu.vue': emptyModule,
  '@/components/EntryProcessingTimeline.vue': emptyModule, '@/components/EntryClassification.vue': classificationModule, '@/components/EntryStarToggle.vue': emptyModule,
}
const props = { selectedIds: new Set(), batchMode: false, traceAvailableById: {}, traceVisibleIds: {} }

function articleLoadingHook(record: Record<string, unknown>) {
  return evaluate(readFileSync(new URL('../composables/useKnowledgeBase.ts', import.meta.url), 'utf8'), {
    pinia: { storeToRefs: () => ({ cardList: ref([]), total: ref(0) }) },
    '../utils/index': { formatStringDate: () => '2026-10-12' },
    'tdesign-vue-next': { MessagePlugin: {} },
    '@/api/knowledge-base': { listEntryFiles: async () => ({ data: [record], total: 1 }) },
    '@/stores/entry': { entryStore: () => ({}) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    '@/stores/resources': { useResourcesStore: () => ({}) },
  }).default('kb-articles')
}
for (const locale of ['zh-CN', 'en-US'] as const) {
  const copy = messages[locale].knowledgeBase
  test(`${locale}: cards and rows show and hide ingest markers through the production list loading hook`, async () => {
    for (const parse_status of ['completed', 'processing', 'failed']) for (const flagged of [true, false, undefined]) {
      const hook = articleLoadingHook({ ...article, parse_status, acquisition: flagged ? 'model_written' : 'verbatim', ...(flagged !== undefined ? { incomplete: flagged } : {}), updated_at: '2026-10-12T00:00:00Z' })
      await hook.loadEntries()
      for (const path of ['../views/knowledge-base/components/DocumentCardView.vue', '../views/knowledge-base/components/DocumentListView.vue']) {
        const html = await render(component(path, listModules), { ...props, items: hook.cardList.value }, locale)
        assert.equal(html.includes(messages[locale].ingestStatus.modelWritten), Boolean(flagged), path)
        assert.equal(html.includes(messages[locale].ingestStatus.incomplete), Boolean(flagged), path)
        for (const hint of [messages[locale].ingestStatus.modelWrittenHint, messages[locale].ingestStatus.incompleteHint]) {
          assert.equal(html.includes(`title="${hint.replaceAll("'", "&#39;")}"`), Boolean(flagged), `${path}: ${hint}`)
        }
      }
    }
  })
  test(`${locale}: cards and rows render the content type, mode, three summary sections and relations`, async () => {
    for (const path of ['../views/knowledge-base/components/DocumentCardView.vue', '../views/knowledge-base/components/DocumentListView.vue']) {
      const html = await render(component(path, listModules), { ...props, items: [article] }, locale)
      for (const text of [copy.contentTypes.event, copy.processingModes.full, copy.summaryOverview, copy.summaryPoints, copy.summaryKeyTerms, article.structured_summary.overview, ...article.structured_summary.points, 'Example Company', 'Product releases']) assert.ok(html.includes(text), `${path}: ${text}`)
      assert.match(html, /<button[^>]*type="button"[^>]*>Example Company<\/button>/)
    }
  })
  test(`${locale}: unclassified summary-only articles show a safe original link`, async () => {
    const html = await render(classification, { entry: { ...article, content_type: '', processing_mode: 'summary_only', source: 'https://example.com/tutorial' } }, locale)
    assert.ok(html.includes(copy.contentTypes.unclassified)); assert.ok(html.includes(copy.processingModes.summary_only))
    assert.match(html, /href="https:\/\/example.com\/tutorial"/)
    const unsafe = await render(classification, { entry: { ...article, processing_mode: 'summary_only', source: 'javascript:alert(1)' } }, locale)
    assert.ok(unsafe.includes(copy.originalLinkMissing)); assert.doesNotMatch(unsafe, /href="javascript:/)
  })
  test(`${locale}: document details hide removed body and queue reprocessing with readable errors`, async () => {
    const requests: string[] = []; const notifications: string[] = []
    const modules = {
      marked: { marked: { use() {}, Renderer: class {}, parse: (value: string) => value } }, 'marked-katex-extension': { __esModule: true, default: () => ({}) }, 'highlight.js': { __esModule: true, default: {} }, mermaid: { __esModule: true, default: { initialize() {} } },
      'tdesign-vue-next': { MessagePlugin: { success: (text: string) => notifications.push(text), error: (text: string) => notifications.push(text) } },
      '@/api/knowledge-base': { ENTRY_CHUNK_PAGE_SIZE: 25, reprocessEntry: async (id: string) => { requests.push(id); return { success: true, data: { parse_status: 'pending' } } } },
      '@/utils/security': { isValidURL: (value: string) => /^https?:\/\//.test(value) }, '@/utils/markdownTableNormalize': {}, '@/utils/mermaidViewer': {}, '@/components/SafeTextarea.vue': safeTextareaTestModule, '@/components/DocumentPreview.vue': emptyModule,
      '@/utils/clipboard': { copyWithToast() {} },
      '@/components/MissingMaterials.vue': emptyModule, '@/components/IncompleteActions.vue': emptyModule,
      '@/components/ReviewActions.vue': reviewModule, '@/components/EntryArticleJudgments.vue': emptyModule, '@/components/EntryProcessingTimeline.vue': emptyModule, '@/views/knowledge-base/components/RepostSources.vue': emptyModule, '@/components/EntryClassification.vue': classificationModule, '@/views/knowledge-base/entryDownloadFileName': {}, '@/utils/filePreview': {}, '@/utils/api-base': {},
    }
    const detail = component('./DocContent.vue', modules)
    const html = await render(detail, { visible: true, details: { ...article, processing_mode: 'summary_only', source: 'https://example.com/tutorial' } }, locale)
    assert.ok(html.includes(copy.reprocess)); assert.ok(html.includes(article.structured_summary.overview)); assert.ok(html.includes('https://example.com/tutorial'))
    assert.doesNotMatch(html, /class="[^\"]*doc-content-section/)
    const failed = await render(detail, { visible: true, details: { ...article, parse_status: 'failed', error_message: 'Cannot fetch original article: HTTP 404' } }, locale)
    assert.match(failed, /role="alert"[^>]*>Cannot fetch original article: HTTP 404/)
    const rejected = await render(detail, { visible: true, details: { ...article, parse_status: 'rejected', metadata: { intake: { source: 'url', decision: 'rejected', source_info: { original_url: 'https://example.com/article' } } } } }, locale)
    for (const text of [messages[locale].articleJudgments.approveReview, messages[locale].articleJudgments.rejectReview, copy.intakeRetry, copy.intakeDiscard]) assert.ok(rejected.includes(text))
    const setup = detail.setup
    detail.setup = (props: object, context: object) => {
      const state = setup(props, context)
      onServerPrefetch(async () => { await state.reprocess(); await nextTick(); state.stopProcessingPolling() })
      return state
    }
    await render(detail, { visible: true, details: { ...article } }, locale)
    assert.deepEqual(requests, ['article']); assert.deepEqual(notifications, [copy.reprocessQueued])
  })
}
test('content type filters retain all six types and unclassified articles', () => {
  const page = readFileSync(new URL('../views/knowledge-base/KnowledgeBase.vue', import.meta.url), 'utf8')
  const api = readFileSync(new URL('../api/knowledge-base.ts', import.meta.url), 'utf8')
  assert.match(page, /v-model="selectedContentType"/)
  assert.match(page, /content_type: selectedContentType\.value/)
  assert.match(page, /CONTENT_TYPES, 'unclassified'/)
  assert.match(api, /query\.append\('content_type', params\.content_type\)/)
  assert.match(page, /@openEntity="openArticleEntity"/)
  assert.match(page, /:initial-entity-id="selectedEntityId"/)
})

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: historical RSS and web article channels keep their localized labels`, async () => {
    const card = component('../views/knowledge-base/components/DocumentCardView.vue', listModules)
    const setup = card.setup
    card.setup = (props: object, context: object) => {
      const state = setup(props, context)
      onServerPrefetch(() => {
        assert.equal(state.getEntryType({ ...article, channel: 'rss' }), messages[locale].knowledgeBase.channelRSS)
        assert.equal(state.getChannelLabel('rss'), messages[locale].knowledgeBase.channelRSS)
        assert.equal(state.getChannelLabel('web'), messages[locale].knowledgeBase.channelWeb)
      })
      return state
    }
    await render(card, { ...props, items: [{ ...article, channel: 'rss' }, { ...article, channel: 'web' }] }, locale)
    const row = component('../views/knowledge-base/components/DocumentListView.vue', listModules)
    const rowSetup = row.setup
    row.setup = (props: object, context: object) => {
      const state = rowSetup(props, context)
      onServerPrefetch(() => {
        assert.equal(state.getSourceInfo({ ...article, channel: 'rss' }).label, messages[locale].knowledgeBase.channelRSS)
        assert.equal(state.getSourceInfo({ ...article, type: 'url', channel: 'web' }).label, messages[locale].knowledgeBase.channelUrl)
      })
      return state
    }
    await render(row, { ...props, items: [{ ...article, channel: 'rss' }, { ...article, type: 'url', channel: 'web' }] }, locale)
  })
}
