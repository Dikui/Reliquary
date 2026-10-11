import { changedSettings } from '../utils/settingsPatch'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zhCN from '../i18n/locales/zh-CN'
import enUS from '../i18n/locales/en-US'
import { CONTENT_TYPES } from '../types/knowledgeClassification'
import { safeTextareaTestModule } from './__tests__/safeTextareaStub'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const emptyModule = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => name.endsWith('.css') ? {} : name.endsWith('.png') ? '/image.png' : modules[name] ?? require(name), module, module.exports)
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
const classification = component('./KnowledgeClassification.vue', {
  './FollowToggle.vue': emptyModule,
  '@/types/knowledgeClassification': { CONTENT_TYPES }, '@/utils/security': { isValidURL: (value: string) => /^https?:\/\//.test(value) },
})
const reviewModule = { __esModule: true, default: component('./KnowledgeReviewActions.vue', { '@/types/knowledgeClassification': { CONTENT_TYPES }, '@/api/knowledge-review': {}, '@/utils/security': { isValidURL: (value: string) => /^https?:\/\//.test(value) }, 'tdesign-vue-next': { MessagePlugin: {} } }) }
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
  '@/components/KnowledgeProcessingTimeline.vue': emptyModule, '@/components/KnowledgeClassification.vue': classificationModule, '@/components/KnowledgeStarToggle.vue': emptyModule,
}
const props = { selectedIds: new Set(), batchMode: false, traceAvailableById: {}, traceVisibleIds: {} }
for (const locale of ['zh-CN', 'en-US'] as const) {
  const copy = messages[locale].knowledgeBase
  test(`${locale}: cards and rows render the content type, mode, three summary sections and relations`, async () => {
    for (const path of ['../views/knowledge/components/DocumentCardView.vue', '../views/knowledge/components/DocumentListView.vue']) {
      const html = await render(component(path, listModules), { ...props, items: [article] }, locale)
      for (const text of [copy.contentTypes.event, copy.processingModes.full, copy.summaryOverview, copy.summaryPoints, copy.summaryKeyTerms, article.structured_summary.overview, ...article.structured_summary.points, 'Example Company', 'Product releases']) assert.ok(html.includes(text), `${path}: ${text}`)
      assert.match(html, /<button[^>]*type="button"[^>]*>Example Company<\/button>/)
    }
  })
  test(`${locale}: unclassified summary-only articles show a safe original link`, async () => {
    const html = await render(classification, { knowledge: { ...article, content_type: '', processing_mode: 'summary_only', source: 'https://example.com/tutorial' } }, locale)
    assert.ok(html.includes(copy.contentTypes.unclassified)); assert.ok(html.includes(copy.processingModes.summary_only))
    assert.match(html, /href="https:\/\/example.com\/tutorial"/)
    const unsafe = await render(classification, { knowledge: { ...article, processing_mode: 'summary_only', source: 'javascript:alert(1)' } }, locale)
    assert.ok(unsafe.includes(copy.originalLinkMissing)); assert.doesNotMatch(unsafe, /href="javascript:/)
  })
  test(`${locale}: document details hide removed body and queue reprocessing with readable errors`, async () => {
    const requests: string[] = []; const notifications: string[] = []
    const modules = {
      marked: { marked: { use() {}, Renderer: class {}, parse: (value: string) => value } }, 'marked-katex-extension': { __esModule: true, default: () => ({}) }, 'highlight.js': { __esModule: true, default: {} }, mermaid: { __esModule: true, default: { initialize() {} } },
      'tdesign-vue-next': { MessagePlugin: { success: (text: string) => notifications.push(text), error: (text: string) => notifications.push(text) } },
      '@/api/knowledge-base': { KNOWLEDGE_CHUNK_PAGE_SIZE: 25, reprocessKnowledge: async (id: string) => { requests.push(id); return { success: true, data: { parse_status: 'pending' } } } },
      '@/utils/security': { isValidURL: (value: string) => /^https?:\/\//.test(value) }, '@/utils/markdownTableNormalize': {}, '@/utils/mermaidViewer': {}, '@/components/SafeTextarea.vue': safeTextareaTestModule, '@/components/DocumentPreview.vue': emptyModule,
      '@/utils/clipboard': { copyWithToast() {} },
      '@/components/KnowledgeReviewActions.vue': reviewModule, '@/components/KnowledgeArticleJudgments.vue': emptyModule, '@/components/KnowledgeProcessingTimeline.vue': emptyModule, '@/views/knowledge/components/RepostSources.vue': emptyModule, '@/components/KnowledgeClassification.vue': classificationModule, '@/views/knowledge/knowledgeDownloadFileName': {}, '@/utils/filePreview': {}, '@/utils/api-base': {},
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
test('content type filters and RSS defaults use all six types and support unclassified and unspecified', () => {
  const page = readFileSync(new URL('../views/knowledge/KnowledgeBase.vue', import.meta.url), 'utf8')
  const api = readFileSync(new URL('../api/knowledge-base.ts', import.meta.url), 'utf8')
  const datasource = readFileSync(new URL('../views/knowledge/settings/DataSourceEditorDialog.vue', import.meta.url), 'utf8')
  assert.match(page, /v-model="selectedContentType"/); assert.match(page, /content_type: selectedContentType\.value/); assert.match(page, /CONTENT_TYPES, 'unclassified'/)
  assert.match(api, /query\.append\('content_type', params\.content_type\)/)
  assert.match(datasource, /CONTENT_TYPES\.map/); assert.match(datasource, /value: '', label: t\('datasource\.defaultContentTypeUnspecified'\)/)
  assert.match(datasource, /default_content_type: form\.value\.default_content_type/)
  assert.match(page, /@openEntity="openArticleEntity"/); assert.match(page, /:initial-entity-id="selectedEntityId"/)
})
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: RSS editor renders all defaults and submits the selected content type`, async () => {
    const requests: Record<string, unknown>[] = []
    const editor = component('../views/knowledge/settings/DataSourceEditorDialog.vue', {
      '@/utils/settingsPatch': { changedSettings },
      '@/types/knowledgeClassification': { CONTENT_TYPES }, '@/components/SafeTextarea.vue': safeTextareaTestModule,
      '@/components/settings/SettingDrawer.vue': { __esModule: true, default: defineComponent({ setup: (_props, { slots }) => () => h('div', slots.default?.()) }) },
      'tdesign-vue-next': { MessagePlugin: { success() {}, warning() {} } },
      '@/api/datasource': { createDataSource: async (payload: Record<string, unknown>) => { requests.push(payload); return { data: { id: 'rss' } } }, triggerSync: async () => ({}) },
    })
    const setup = editor.setup
    editor.setup = (props: object, context: object) => {
      const state = setup(props, context)
      onServerPrefetch(async () => {
        assert.deepEqual(state.contentTypeOptions.value.map((option: { value: string }) => option.value), ['', ...CONTENT_TYPES])
        assert.deepEqual(state.contentTypeOptions.value.map((option: { label: string }) => option.label), [messages[locale].datasource.defaultContentTypeUnspecified, ...CONTENT_TYPES.map(type => messages[locale].knowledgeBase.contentTypes[type])])
        state.form.value = { ...state.form.value, name: 'Updates', feedUrls: 'https://example.com/feed.xml', default_content_type: 'research' }
        await state.save()
      })
      return state
    }
    const app = createSSRApp(editor, { visible: true, dataSource: null })
    app.use(createI18n({ legacy: false, locale, messages }))
    const stub = defineComponent({ setup: (_props, { slots }) => () => h('div', slots.default?.()) })
    for (const name of ['t-form', 't-form-item', 't-input', 't-textarea', 't-select', 't-radio-group', 't-radio', 't-switch', 't-button', 't-alert', 't-popconfirm']) app.component(name, stub)
    const html = await renderToString(app)
    assert.ok(html.includes(messages[locale].datasource.defaultContentType))
    assert.equal(requests.length, 1); assert.equal(requests[0].default_content_type, 'research')
    assert.equal(requests[0].type, 'rss')
  })
}
