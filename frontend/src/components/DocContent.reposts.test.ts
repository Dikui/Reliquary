import { formatCountCopy } from '../i18n/countCopy'
import assert from 'node:assert/strict'
import * as entrySourceLabels from '../utils/entrySourceLabels'
import * as markdownPlainText from '../utils/markdownPlainText'
import { safeTextareaTestModule } from './__tests__/safeTextareaStub'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { compileScript, compileStyleAsync, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zhCN from '../i18n/locales/zh-CN'
import enUS from '../i18n/locales/en-US'
import { safeExternalUrl } from '../utils/externalUrl'
import type { EntryRepostSource } from '../api/library'
import { createSafeImage, isValidImageURL, protectProviderImageSrcInHTML } from '../utils/security'
import { buildProtectedFileRequest } from '../utils/protectedFileAccess'
import { CONTENT_TYPES } from '../types/entryClassification'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const compile = (code: string) => ts.transpileModule(code, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
function evaluate(code: string, modules: Record<string, unknown>, environment: Record<string, unknown> = {}) {
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', 'window', 'localStorage', compile(code))(
    (name: string) => name.endsWith('.css') ? {} : name === '@/utils/entrySourceLabels' ? entrySourceLabels : (name === './BodyReference.vue' ? { __esModule: true, default: defineComponent({ setup: () => () => null }) } : modules[name]) ?? (name === '@/components/SafeTextarea.vue' ? safeTextareaTestModule : name === '@/i18n/countCopy' ? { formatCountCopy } : require(name)), module, module.exports,
    environment.window, environment.localStorage,
  )
  return module.exports
}
function compileSFC(path: string) {
  const filename = path.split('/').at(-1)!
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const { descriptor } = parse(source, { filename })
  const script = compileScript(descriptor, { id: `repost-test-${filename}` })
  const template = compileTemplate({
    id: `repost-test-${filename}`, filename, source: descriptor.template!.content,
    ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings },
  })
  assert.deepEqual(template.errors, [])
  return { source, filename: fileURLToPath(new URL(path, import.meta.url)), styles: descriptor.styles, create(modules: Record<string, unknown> = {}, environment: Record<string, unknown> = {}) {
    const component = evaluate(script.content, modules, environment).default
    component.ssrRender = evaluate(template.code, modules, environment).ssrRender
    return component
  } }
}
const repostSFC = compileSFC('../views/library/components/RepostSources.vue')
const detailSFC = compileSFC('./DocContent.vue')
const actionMenuSFC = compileSFC('../views/library/components/DocumentActionMenu.vue')
const batchBarSFC = compileSFC('../views/library/components/DocumentBatchBar.vue')
const actualRepostSources = repostSFC.create({ '@/utils/externalUrl': { safeExternalUrl } })
const sharedRepostModule = { __esModule: true, default: actualRepostSources }
const emptyComponent = defineComponent({ setup: () => () => null })
const emptyModule = { __esModule: true, default: emptyComponent }
const envelope = (data: unknown) => Promise.resolve({ success: true, data })
const unexpectedAPI = () => { throw new Error('The repost rendering test must not call a remote API') }
const deleteConfirmModule = { __esModule: true, default: compileSFC('./DocumentDeleteConfirm.vue').create() }
const personalDeleteModule = { __esModule: true, default: compileSFC('./PersonalDocumentDelete.vue').create({
  '@/components/DocumentDeleteConfirm.vue': deleteConfirmModule,
  '@/api/library': { deleteDocument: unexpectedAPI },
  'tdesign-vue-next': { MessagePlugin: { success: unexpectedAPI } },
}) }

function source(overrides: Partial<EntryRepostSource> = {}): EntryRepostSource {
  return {
    id: 'repost-a', entry_id: 'message-a', publisher: 'P4 News',
    original_url: 'https://news.example/repost', published_at: '2026-10-06T08:30:00+08:00',
    match_method: 'content_hash', ...overrides,
  }
}
const sourceRows = (['url', 'content_hash', 'simhash', 'file_hash'] as const).map((method, index) => source({
  id: `repost-${method}`, publisher: `Publisher ${method}`, match_method: method,
  original_url: `https://news.example/${method}`, published_at: index % 2 ? undefined : `2026-10-0${index + 1}T08:30:00+08:00`,
  published_at_missing: Boolean(index % 2),
}))

async function renderComponent(component: any, props: Record<string, unknown>, locale: keyof typeof messages, ssrContext: Record<string, any> = {}) {
  const app = createSSRApp(component, props)
  app.use(createI18n({ legacy: false, locale, messages }))
  const stub = defineComponent({
    props: ['visible', 'message', 'description', 'disabled', 'content', 'size'],
    setup(props, { attrs, slots }) {
      return () => props.visible === false ? null : h('div', { ...attrs, 'data-drawer-size': props.size }, [props.message, props.description, slots.header?.(), slots.default?.()])
    },
  })
  for (const name of ['t-drawer', 't-dialog', 't-button', 't-icon', 't-tag', 't-tooltip', 't-loading', 't-empty', 't-select', 't-input', 't-input-number', 't-textarea', 't-checkbox', 't-alert', 't-pagination', 't-popup', 't-popconfirm', 't-switch']) app.component(name, stub)
  const errors: unknown[] = []
  const warnings: string[] = []
  app.config.errorHandler = error => errors.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  const html = await renderToString(app, ssrContext)
  await nextTick()
  assert.deepEqual(errors, [], 'the full SFC renders without browser APIs or template exceptions')
  assert.deepEqual(warnings, [], 'all actual template components are resolved and render without warnings')
  return html
}

interface DetailRenderOptions {
  reprocessRequests?: string[]
  copyRequests?: { text: string; successKey: string }[]
  environment?: Record<string, unknown>
  scenario?: (state: any) => void | Promise<void>
  ssrContext?: Record<string, any>
  details?: Record<string, unknown>
  loadedDetails?: Record<string, unknown>
  classification?: unknown
  missingMaterials?: unknown
  incompleteActions?: unknown
  reviewActions?: unknown
  articleJudgments?: unknown
  personalDelete?: unknown
  onReviewChanged?: (...args: unknown[]) => void
}
async function renderDetails(sources: EntryRepostSource[] | null | undefined, locale: keyof typeof messages = 'zh-CN', options: DetailRenderOptions = {}) {
  const component = detailSFC.create({
    marked: { marked: { use() {}, Renderer: class {}, parse: (value: string) => value } },
    'marked-katex-extension': { __esModule: true, default: () => ({}) },
    'highlight.js': { __esModule: true, default: {} },
    mermaid: { __esModule: true, default: { initialize() {} } },
    'tdesign-vue-next': { MessagePlugin: { error: unexpectedAPI, warning: unexpectedAPI, success() {} } },
    '@/api/library': { ENTRY_CHUNK_PAGE_SIZE: 20, getEntryDetails: unexpectedAPI, reparseEntry: (id: string) => { options.reprocessRequests?.push(id); return envelope({ parse_status: 'pending' }) } },
    '@/utils/clipboard': { copyWithToast: async (text: string, successKey: string) => { options.copyRequests?.push({ text, successKey }); return true } },
    '@/utils/security': { isValidURL: (value: string) => Boolean(safeExternalUrl(value)), createSafeImage, isValidImageURL,
      safeMarkdownToHTML: (value: string) => value, sanitizeHTML: (value: string) => value },
    '@/utils/markdownTableNormalize': { normalizeSpuriousTablePrefixes: (value: string) => value }, '@/utils/mermaidViewer': {}, '@/utils/textLineDiff': {},
    '@/stores/auth': { useAuthStore: () => ({ isLoggedIn: true }) },
    '@/components/ReviewActions.vue': options.reviewActions || emptyModule, '@/components/EntryArticleJudgments.vue': options.articleJudgments || emptyModule, '@/components/EntryClassification.vue': options.classification || emptyModule, '@/components/EntryStarToggle.vue': emptyModule,
    '@/components/EntryUpdateNotice.vue': emptyModule,
    '@/components/ReportAssociations.vue': emptyModule,
    '@/components/DocumentPreview.vue': emptyModule,
    '@/components/EntryProcessingTimeline.vue': emptyModule,
    '@/components/PersonalDocumentDelete.vue': options.personalDelete || emptyModule,
    '@/components/MissingMaterials.vue': options.missingMaterials || emptyModule, '@/components/IncompleteActions.vue': options.incompleteActions || emptyModule,
    '@/views/library/components/RepostSources.vue': sharedRepostModule,
    '@/views/library/entryDownloadFileName': {}, '@/utils/filePreview': {}, '@/utils/api-base': {},
  }, options.environment)
  if (options.scenario) {
    const setup = component.setup
    component.setup = (props: object, context: object) => {
      const state = setup(props, context)
      onServerPrefetch(async () => { await options.scenario!(state); await nextTick() })
      return state
    }
  }
  return renderComponent(component, {
    visible: true, canEditLibrary: false, canDownloadLibrary: false, libraryId: 'library-a', entryType: 'document',
    onReviewChanged: options.onReviewChanged,
    details: options.loadedDetails ?? { id: 'message-a', title: 'Original source', type: 'manual', parse_status: 'completed',
      md: [], total: 0, metadata: {}, repost_sources: sources, ...options.details },
  }, locale, options.ssrContext)
}

// Use the production article-detail loader; only its network and store dependencies
// are replaced. The resulting reactive details object is passed to the real drawer.
function createArticleDetailsLoader(records: Record<string, Record<string, unknown>>) {
  const calls: string[] = []
  const hookSource = readFileSync(new URL('../composables/useLibrary.ts', import.meta.url), 'utf8')
  const modules = {
    pinia: { storeToRefs: () => ({ cardList: ref([]), total: ref(0) }) },
    '../utils/index': { formatStringDate: () => '2026-10-11' },
    'tdesign-vue-next': { MessagePlugin: {} },
    '@/api/library': {
      getEntryDetails: async (id: string) => {
        calls.push(id)
        return { success: true, data: { id, title: id, type: 'manual', source_kind: 'article',
          parse_status: 'completed', updated_at: '2026-10-11T00:00:00Z', ...records[id] } }
      },
      getEntryDetailsCon: async () => ({ success: true, data: [], total: 0 }),
    },
    '@/stores/entry': { entryStore: () => ({}) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    '@/stores/resources': { useResourcesStore: () => ({}) },
  }
  const hook = evaluate(hookSource, modules).default('library-a')
  return { hook, calls, async load(id: string) {
    hook.getCardDetails({ id })
    await new Promise<void>(resolve => setImmediate(resolve))
    return hook.details
  } }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: detail conflict keeps its approval label after loading the actual related article field`, async () => {
    const loader = createArticleDetailsLoader({ conflict: { related_entry_id: 'original-article', parse_status: 'rejected', metadata: { intake: { source: 'api', decision: 'rejected', source_info: { original_url: 'https://example.com/conflict' } } } } })
    const review = compileSFC('./ReviewActions.vue').create({ '@/types/entryClassification': { CONTENT_TYPES }, '@/api/entry-review': {}, '@/utils/security': { isValidURL: (value: string) => Boolean(safeExternalUrl(value)) }, 'tdesign-vue-next': { MessagePlugin: {} } })
    const loadedDetails = await loader.load('conflict')
    const html = await renderDetails([], locale, { loadedDetails, reviewActions: { __esModule: true, default: review } })
    assert.equal(loadedDetails.related_entry_id, 'original-article')
    assert.ok(html.includes(messages[locale].knowledgeReview.saveAsNew))
  })
  test(`${locale}: details prevent cross-action submissions and refresh review counts only on article judgments`, async () => {
    const loader = createArticleDetailsLoader({ article: { incomplete: true } })
    const loadedDetails = await loader.load('article')
    const events: unknown[][] = []
    const disabled: boolean[] = []
    const judgments = defineComponent({ props: ['disabled'], setup(props) { disabled.push(Boolean(props.disabled)); return () => null } })
    await renderDetails([], locale, { loadedDetails, articleJudgments: { __esModule: true, default: judgments }, onReviewChanged: (...args: unknown[]) => events.push(args), async scenario(state) {
      state.incompleteBusy.value = true
      await state.reprocess()
      state.applyProcessingState({ parse_status: 'completed' })
      assert.equal(events.length, 0, 'processing polls do not refresh pending counts')
      state.applyEntryJudgment({ content_type: 'research' })
      assert.equal(events.length, 1)
    } })
    assert.deepEqual(disabled, [true])
    assert.equal(events[0]![1], false)
    const accepts: boolean[] = []
    const action = defineComponent({ props: ['disabled'], setup(props) { accepts.push(Boolean(props.disabled)); return () => null } })
    await renderDetails([], locale, { loadedDetails, incompleteActions: { __esModule: true, default: action }, scenario(state) { state.articleJudgmentsBusy.value = true } })
    assert.deepEqual(accepts, [true])
  })
  test(`${locale}: actual detail loader displays ingest markers and missing reasons, then accepting clears its state and refresh event`, async () => {
    const missing = [{ type: 'image', source_url: 'https://example.com/image.png', reason: 'Access denied', api_key_name: 'External Collector', submitted_at: '2026-10-12T08:00:00Z' }]
    const loader = createArticleDetailsLoader({ incomplete: { acquisition: 'model_written', incomplete: true, missing_materials: missing }, complete: { acquisition: 'extracted', incomplete: false }, unspecified: { acquisition: 'extracted' } })
    const classified = compileSFC('./EntryClassification.vue').create({ './FollowToggle.vue': emptyModule, '@/utils/markdownPlainText': markdownPlainText, '@/types/entryClassification': { CONTENT_TYPES }, '@/utils/security': { isValidURL: (value: string) => Boolean(safeExternalUrl(value)) } })
    const materials = compileSFC('./MissingMaterials.vue').create({ '@/utils/security': { isValidURL: (value: string) => Boolean(safeExternalUrl(value)) } })
    const requests: string[] = []
    const api = evaluate(readFileSync(new URL('../api/ingest.ts', import.meta.url), 'utf8'), { '@/utils/request': { post: async (url: string) => { requests.push(url); return { success: true } } } })
    const action = () => compileSFC('./IncompleteActions.vue').create({ '@/api/ingest': api, 'tdesign-vue-next': { MessagePlugin: { success() {} } } })
    const modules = { classification: { __esModule: true, default: classified }, missingMaterials: { __esModule: true, default: materials } }
    const details = await loader.load('incomplete')
    const initial = await renderDetails([], locale, { ...modules, loadedDetails: details, incompleteActions: { __esModule: true, default: action() } })
    assert.ok(initial.includes(messages[locale].ingestStatus.modelWritten))
    assert.ok(initial.includes(`title="${messages[locale].ingestStatus.modelWrittenHint.replaceAll("'", "&#39;")}"`))
    assert.ok(initial.includes(messages[locale].ingestStatus.incomplete))
    assert.ok(initial.includes(`title="${messages[locale].ingestStatus.incompleteHint.replaceAll("'", "&#39;")}"`))
    for (const text of ['Access denied', 'External Collector', 'https://example.com/image.png', messages[locale].ingestStatus.acceptIncomplete]) assert.ok(initial.includes(text), text)
    assert.equal(requests.length, 0, 'opening article details never accepts automatically')
    const events: unknown[][] = []
    const accepting = action(), setup = accepting.setup
    accepting.setup = (props: object, context: object) => {
      const state = setup(props, context)
      onServerPrefetch(async () => {
        state.request('accept'); state.cancel(); await state.confirm()
        assert.equal(requests.length, 0, 'cancel confirmation does not call the endpoint')
        state.request('accept'); await state.confirm()
      })
      return state
    }
    await renderDetails([], locale, { ...modules, loadedDetails: details, incompleteActions: { __esModule: true, default: accepting }, onReviewChanged: (...args: unknown[]) => events.push(args) })
    assert.deepEqual(requests, ['/api/v1/ingest/incomplete/accept-incomplete'])
    assert.equal(details.incomplete, false)
    assert.deepEqual(details.missing_materials, [])
    assert.equal(events.length, 1)
    assert.equal(events[0]![1], false)
    const after = await renderDetails([], locale, { ...modules, loadedDetails: details })
    assert.doesNotMatch(after, /class="[^"]*incomplete-materials|Access denied/)
    assert.ok(after.includes(messages[locale].ingestStatus.modelWritten))
    assert.ok(after.includes(`title="${messages[locale].ingestStatus.modelWrittenHint.replaceAll("'", "&#39;")}"`))
    assert.ok(!after.includes(`title="${messages[locale].ingestStatus.incompleteHint.replaceAll("'", "&#39;")}"`))
    for (const id of ['complete', 'unspecified']) {
      const complete = await renderDetails([], locale, { ...modules, loadedDetails: await loader.load(id) })
      assert.ok(!complete.includes(messages[locale].ingestStatus.modelWritten))
      assert.doesNotMatch(complete, /class="[^"]*incomplete-materials/)
      for (const hint of [messages[locale].ingestStatus.modelWrittenHint, messages[locale].ingestStatus.incompleteHint]) {
        assert.ok(!complete.includes(`title="${hint.replaceAll("'", "&#39;")}"`), `${id}: ${hint}`)
      }
    }
  })
  test(`${locale}: source unverified marker reaches the actual article drawer from the detail loading hook`, async () => {
    const loader = createArticleDetailsLoader({ unverified: { source_unverified: true }, verified: {} })
    const classification = compileSFC('./EntryClassification.vue').create({ './FollowToggle.vue': emptyModule, '@/utils/markdownPlainText': markdownPlainText, '@/types/entryClassification': { CONTENT_TYPES }, '@/utils/security': { isValidURL: (value: string) => Boolean(safeExternalUrl(value)) } })
    const flagged = await renderDetails([], locale, { loadedDetails: await loader.load('unverified'), classification: { __esModule: true, default: classification } })
    assert.ok(flagged.includes(messages[locale].knowledgeReview.sourceUnverified))
    const verified = await renderDetails([], locale, { loadedDetails: await loader.load('verified'), classification: { __esModule: true, default: classification } })
    assert.ok(!verified.includes(messages[locale].knowledgeReview.sourceUnverified))
  })
  test(`${locale}: API collection reason reaches the actual loading hook and article drawer`, async () => {
    const reason = locale === 'zh-CN' ? '保存测量方法与来源。' : 'Keep measurement methods and sources.'
    const loader = createArticleDetailsLoader({ article: { collect_reason: reason } })
    const loadedDetails = await loader.load('article')
    const html = await renderDetails([], locale, { loadedDetails })
    assert.deepEqual(loader.calls, ['article'], 'The production hook requested article details')
    assert.match(html, /doc-collect-reason-row/, 'The API reason survives projection into drawer props')
    assert.ok(html.includes(messages[locale].library.collectReason))
    assert.ok(html.includes(reason))
  })

  test(`${locale}: actual loading path clears the previous reason and hides empty API values`, async () => {
    const loader = createArticleDetailsLoader({
      saved: { collect_reason: 'Keep the source.' },
      empty: { collect_reason: '' }, missing: {}, nullable: { collect_reason: null },
      whitespace: { collect_reason: '   ' },
    })
    for (const id of ['empty', 'missing', 'nullable', 'whitespace']) {
      await loader.load('saved')
      loader.hook.getCardDetails({ id })
      assert.equal(loader.hook.details.collect_reason, '', 'Changing articles clears the reason immediately')
      await new Promise<void>(resolve => setImmediate(resolve))
      const html = await renderDetails([], locale, { loadedDetails: loader.hook.details })
      assert.doesNotMatch(html, /doc-collect-reason-row/)
      assert.ok(!html.includes(messages[locale].library.collectReason))
      assert.ok(!html.includes('Keep the source.'))
    }
  })
}

function repostBlock(html: string) {
  const block = html.match(/<div class="repost-sources"[^>]*>[\s\S]*?<\/ul><\/div>/)?.[0]
  assert.ok(block, 'the actual shared component rendered a repost list')
  return block
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: article basic information displays and escapes the collection reason`, async () => {
    const html = await renderDetails([], locale, { details: { collect_reason: '保存研究来源 <script>alert(1)</script>' } })
    const row = html.match(/<div class="doc-detail-row doc-collect-reason-row">([\s\S]*?)<\/div>/)?.[1]
    assert.ok(row, 'The reason is a basic-information row')
    assert.ok(row.includes(messages[locale].library.collectReason))
    assert.ok(row.includes('保存研究来源 &lt;script&gt;alert(1)&lt;/script&gt;'))
    assert.doesNotMatch(row, /<script>/)
  })

  for (const collect_reason of [undefined, null, '', '   ']) {
    test(`${locale}: article basic information hides an empty collection reason ${JSON.stringify(collect_reason)}`, async () => {
      const html = await renderDetails([], locale, { details: { collect_reason } })
      assert.doesNotMatch(html, /doc-collect-reason-row/)
      assert.ok(!html.includes(messages[locale].library.collectReason))
    })
  }

  test(`${locale}: article renderer sends relative attachment images through protected vault access`, async () => {
    const ref = '../../attachments/vault-article/图表.png'
    let imageHTML = ''
    await renderDetails([], locale, {
      details: { id: 'vault-article', source_kind: 'article', vault_path: 'articles/2026-10/标题-vaultart.md' },
      scenario: async state => { imageHTML = state.renderer.image({ href: ref, title: '图表', text: 'Chart' }) },
    })
    const source = `vault://vault-article/${encodeURIComponent(ref)}`
    assert.ok(imageHTML.includes(`src="${source}"`))
    const protectedHTML = protectProviderImageSrcInHTML(imageHTML)
    assert.ok(protectedHTML.includes(`data-protected-src="${source}"`))
    const request = buildProtectedFileRequest(source, { mode: 'space' })!
    const url = new URL(request.url, 'http://localhost')
    assert.equal(url.pathname, '/api/v1/entries/vault-article/vault-attachment')
    assert.equal(url.searchParams.get('path'), ref)
    assert.doesNotMatch(request.url, /token=/)
  })

  test(`${locale}: article HTML images also use protected relative attachment access`, async () => {
    const ref = '../../attachments/vault-article/图表.png'
    let html = ''
    await renderDetails([], locale, {
      details: { id: 'vault-article', source_kind: 'article', vault_path: 'articles/2026-10/标题-vaultart.md' },
      scenario: state => { html = state.processMarkdown(`<img src="${ref}" alt="Chart">`) },
    })
    const source = `vault://vault-article/${encodeURIComponent(ref)}`
    assert.ok(protectProviderImageSrcInHTML(html).includes(`data-protected-src="${source}"`))
  })

  for (const source_kind of ['mine', 'report']) {
    test(`${locale}: ${source_kind} detail submits processing or index rebuilding`, async () => {
      const calls: string[] = []
      const html = await renderDetails([], locale, {
        details: { source_kind, vault_path: `${source_kind}/note.md`, parse_status: 'failed' },
        reprocessRequests: calls,
        scenario: state => state.reprocess(),
      })
      assert.deepEqual(calls, ['message-a'])
      assert.ok(html.includes(source_kind === 'report' ? messages[locale].library.reindexDocument : messages[locale].library.reprocess))
    })
  }

  test(`${locale}: all library documents expose reprocessing or report index rebuilding without a body editor`, async () => {
    const component = actionMenuSFC.create({
      '@/components/PersonalDocumentDelete.vue': personalDeleteModule,
      '@/components/DocumentDeleteConfirm.vue': deleteConfirmModule,
    })
    const item = { id: 'article', type: 'manual', parse_status: 'completed', processing_mode: 'full' }
    const render = (fields: Record<string, unknown>) => renderComponent(component, { item: { ...item, ...fields }, traceVisible: false }, locale)
    const editable = await render({})
    const hasEditAction = (html: string) => /class="[^"]*doc-action-menu-item[^"]*"[^>]*>[\s\S]*?name="edit"/.test(html)
    const reprocessLabel = `<span>${messages[locale].library.rebuildDocument}</span>`
    assert.equal(hasEditAction(editable), false)
    assert.ok(editable.includes(reprocessLabel))
    const article = await render({ source_kind: 'article', vault_path: 'articles/note.md' })
    assert.equal(hasEditAction(article), false)
    assert.ok(article.includes(reprocessLabel))
    for (const source_kind of ['mine', 'report']) {
      for (const parse_status of ['completed', 'processing']) {
        const html = await render({ source_kind, parse_status })
        assert.equal(hasEditAction(html), false)
        assert.ok(html.includes(source_kind === 'report' ? messages[locale].library.reindexDocument : messages[locale].library.rebuildDocument))
      }
    }
    const mixedBatch = await renderComponent(batchBarSFC.create(), { count: 2 }, locale)
    assert.ok(mixedBatch.includes(messages[locale].entryBatch.title))
    assert.ok(mixedBatch.includes(messages[locale].library.selectedCount.replace('{count}', '2')))
  })

  test(`${locale}: article detail shows the relative vault path and copies it through the shared clipboard helper`, async () => {
    const path = 'articles/2026-10/中文文章-abc12345.md'
    const copyRequests: { text: string; successKey: string }[] = []
    const html = await renderDetails([], locale, {
      details: { vault_path: path }, copyRequests,
      scenario: async state => { await state.copyWithToast(path, 'common.copySuccess') },
    })
    assert.ok(html.includes(messages[locale].library.vaultPath))
    assert.ok(html.includes(messages[locale].library.copyVaultPath))
    assert.ok(html.includes(`<code>${path}</code>`))
    assert.deepEqual(copyRequests, [{ text: path, successKey: 'common.copySuccess' }])
    assert.doesNotMatch(await renderDetails([], locale), /doc-vault-path-row/)
  })

  test(`${locale} full message detail SFC shows publishers, safe URLs, times and all four duplicate methods`, async () => {
    const html = await renderDetails(sourceRows, locale)
    assert.match(html, /class="setting-drawer__section doc-repost-section"/)
    const block = repostBlock(html)
    const copy = messages[locale].knowledgeWorkbench.sourceTime
    assert.ok(block.includes(copy.reposts))
    assert.ok(block.includes(copy.missing))
    assert.ok(block.includes(copy.matchMethod))
    for (const item of sourceRows) {
      assert.ok(block.includes(item.publisher!))
      assert.ok(block.includes(item.original_url!))
      if (item.published_at) assert.ok(block.includes(item.published_at))
      assert.ok(block.includes((copy.matchMethods as Record<string, string>)[item.match_method]))
    }
    assert.equal([...block.matchAll(/target="_blank" rel="noopener noreferrer"/g)].length, 4)
    assert.doesNotMatch(block, /entryWorkbench\.sourceTime\./)
  })


  test(`${locale} fallback publisher, missing publication time and unknown method stay localized`, async () => {
    const html = await renderDetails([source({ publisher: '', author: '', published_at_missing: true, match_method: 'future' })], locale)
    const block = repostBlock(html)
    const copy = messages[locale].knowledgeWorkbench.sourceTime
    assert.ok(block.includes(copy.unknownPublisher))
    assert.ok(block.includes(copy.missing))
    assert.ok(block.includes(copy.matchMethods.other))
    assert.ok(!block.includes('2026-10-06T08:30:00+08:00'), 'missing flag has priority over a stale timestamp')
  })
}

for (const sources of [undefined, null, []]) {
  test(`full detail SFC hides the repost section for ${JSON.stringify(sources)}`, async () => {
    const html = await renderDetails(sources)
    assert.doesNotMatch(html, /doc-repost-section|class="repost-sources"/)
  })
}

for (const original_url of ['javascript:alert(1)', 'data:text/html,<script>alert(1)</script>', 'file:///tmp/source', '/source', '//news.example/repost', 'invalid URL']) {
  test(`full detail SFC never turns an unsafe repost URL into a link: ${original_url}`, async () => {
    const html = await renderDetails([source({ original_url })])
    const block = repostBlock(html)
    assert.doesNotMatch(block, /<a[\s>]|<script>/)
    assert.ok(block.includes(original_url.replaceAll('<', '&lt;').replaceAll('>', '&gt;')), 'the original URL remains available as escaped text')
    assert.match(block, /P4 News/)
  })
}

test('author is shown when publisher is missing and an absent method creates no method label', async () => {
  const html = await renderDetails([source({ publisher: '', author: 'Reporter A', match_method: '' })])
  const block = repostBlock(html)
  assert.match(block, /Reporter A/)
  assert.ok(!block.includes(zhCN.knowledgeWorkbench.sourceTime.matchMethod))
})

for (const field of ['publisher', 'author'] as const) {
  test(`repost ${field} is escaped by the actual child SFC`, async () => {
    const html = await renderDetails([source({ publisher: '', [field]: '<img src=x onerror=alert(1)>' })])
    const block = repostBlock(html)
    assert.match(block, /&lt;img src=x onerror=alert\(1\)&gt;/)
    assert.doesNotMatch(block, /<img[\s>]/)
  })
}

function drawerTag(html: string) {
  const tag = html.match(/<div[^>]*class="[^"]*\bdoc-main-drawer\b[^"]*"[^>]*>/)?.[0]
  assert.ok(tag, 'the full detail template renders its main TDrawer')
  return tag
}

for (const [width, size] of [[390, '100vw'], [720, '100vw'], [721, '654px'], [1440, '654px']] as const) {
  test(`the full detail SFC gives TDrawer size ${size} at viewport ${width}`, async () => {
    const ssrContext: Record<string, any> = {}
    const original_url = `https://news.example/repost?details=${'long-source-'.repeat(100)}`
    const html = await renderDetails([source({ original_url })], 'zh-CN', {
      environment: { window: { innerWidth: width } }, ssrContext,
    })
    assert.ok(drawerTag(html).includes(`data-drawer-size="${size}"`), 'size is read from the actual TDrawer prop')
    assert.ok(repostBlock(html).includes(original_url), 'the full source URL is retained without truncation')
    const teleports = ssrContext.teleports?.body || ''
    if (width <= 720) assert.doesNotMatch(teleports, /class="doc-drawer-resize-handle"/)
    else assert.match(teleports, /class="doc-drawer-resize-handle"/)
  })
}

test('one detail instance preserves default desktop width through desktop, narrow and desktop resize', async () => {
  const viewport = { innerWidth: 1440 }
  const widths: Array<[number, string]> = []
  const html = await renderDetails(sourceRows, 'en-US', {
    environment: { window: viewport },
    scenario(state) {
      widths.push([state.mainDrawerWidth.value, state.mainDrawerSize.value])
      viewport.innerWidth = 390
      state.onTraceDrawerWindowResize()
      widths.push([state.mainDrawerWidth.value, state.mainDrawerSize.value])
      assert.equal(state.drawerViewportWidth.value, 390)
      state.onMainDrawerResizeStart({ clientX: 0 })
      assert.equal(state.mainDrawerResizing.value, false, 'narrow dragging returns before accessing DOM listeners')
      viewport.innerWidth = 1440
      state.onTraceDrawerWindowResize()
      widths.push([state.mainDrawerWidth.value, state.mainDrawerSize.value])
    },
  })
  assert.deepEqual(widths, [[654, '654px'], [654, '100vw'], [654, '654px']])
  assert.ok(drawerTag(html).includes('data-drawer-size="654px"'))
})

test('loading a saved desktop width on a narrow screen restores that width when returning to desktop', async () => {
  const viewport = { innerWidth: 390 }
  const storageReads: string[] = []
  const widths: Array<[number, string]> = []
  const html = await renderDetails(sourceRows, 'zh-CN', {
    environment: { window: viewport, localStorage: {
      getItem(key: string) { storageReads.push(key); return '920' },
    } },
    scenario(state) {
      state.loadMainDrawerWidth()
      widths.push([state.mainDrawerWidth.value, state.mainDrawerSize.value])
      state.onTraceDrawerWindowResize()
      widths.push([state.mainDrawerWidth.value, state.mainDrawerSize.value])
      viewport.innerWidth = 1440
      state.onTraceDrawerWindowResize()
      widths.push([state.mainDrawerWidth.value, state.mainDrawerSize.value])
    },
  })
  assert.deepEqual(storageReads, ['reliquary-doc-drawer-width'])
  assert.deepEqual(widths, [[920, '100vw'], [920, '100vw'], [920, '920px']])
  assert.ok(drawerTag(html).includes('data-drawer-size="920px"'))
})

for (const [saved, expected] of [['2', 480], ['920', 920], ['2300', 1600]] as const) {
  test(`narrow saved width ${saved} retains desktop preference within ${expected}px while drawer stays 100vw`, async () => {
    const html = await renderDetails(sourceRows, 'zh-CN', {
      environment: { window: { innerWidth: 390 }, localStorage: { getItem: () => saved } },
      scenario(state) {
        state.loadMainDrawerWidth()
        assert.equal(state.mainDrawerWidth.value, expected)
        assert.equal(state.mainDrawerSize.value, '100vw')
      },
    })
    assert.ok(drawerTag(html).includes('data-drawer-size="100vw"'))
  })
}

async function compiledStyleRules(sfc: ReturnType<typeof compileSFC>) {
  const rules: Array<{ selector: string; media: boolean; declarations: Record<string, string> }> = []
  for (const style of sfc.styles) {
    const compiled = await compileStyleAsync({
      filename: sfc.filename, id: 'data-v-layout-test', source: style.content,
      scoped: style.scoped, preprocessLang: style.lang === 'less' ? 'less' : undefined,
    })
    assert.deepEqual(compiled.errors, [], 'the actual Less and scoped CSS compile successfully')
    compiled.rawResult!.root.walkRules(rule => {
      let parent: any = rule.parent
      let media = false
      while (parent) {
        if (parent.type === 'atrule' && parent.name === 'media') media = true
        parent = parent.parent
      }
      const declarations: Record<string, string> = {}
      rule.walkDecls(declaration => { declarations[declaration.prop] = declaration.value })
      rule.selector.split(',').forEach(selector => rules.push({ selector: selector.trim(), media, declarations }))
    })
  }
  return rules
}

test('compiled all-width drawer CSS reaches TDesign teleported wrapper, content and body without scoped selectors or media limits', async () => {
  const rules = await compiledStyleRules(detailSFC)
  const declarations = (selector: string) => Object.assign({}, ...rules.filter(rule => !rule.media && rule.selector === selector).map(rule => rule.declarations))
  const wrapper = declarations('.t-drawer.doc-main-drawer .t-drawer__content-wrapper')
  assert.equal(wrapper['max-width'], '100vw')
  assert.equal(wrapper['min-width'], '0')
  for (const selector of ['.t-drawer.doc-main-drawer .t-drawer__content', '.t-drawer.doc-main-drawer .t-drawer__body']) {
    const style = declarations(selector)
    assert.equal(style['min-width'], '0')
    assert.equal(style['max-width'], '100%')
    assert.equal(style['box-sizing'], 'border-box')
  }
  assert.equal(declarations('.t-drawer.doc-main-drawer .t-drawer__body')['overflow-x'], 'hidden')
})

test('compiled all-width content, section and header CSS prevents flex and padding overflow while images shrink', async () => {
  const rules = await compiledStyleRules(detailSFC)
  for (const selector of ['.doc-drawer-header', '.doc-drawer-body', '.doc-drawer-body .setting-drawer__section']) {
    const normalized = (value: string) => value.replace(/\[data-v-layout-test\]/g, '')
    const style = Object.assign({}, ...rules.filter(rule => !rule.media && normalized(rule.selector) === selector).map(rule => rule.declarations))
    assert.equal(style['min-width'], '0', selector)
    assert.equal(style['max-width'], '100%', selector)
    assert.equal(style['box-sizing'], 'border-box', selector)
    assert.equal(style['overflow-wrap'], 'anywhere', selector)
  }
  assert.ok(rules.some(rule => !rule.media && /\.doc-drawer-body.*\.md-content img/.test(rule.selector) && rule.declarations['max-width'] === 'min(444px, 100%)'))
})

test('compiled shared repost CSS keeps long URLs wrapping inside the available panel width', async () => {
  const rules = await compiledStyleRules(repostSFC)
  const style = (selector: string) => Object.assign({}, ...rules.filter(rule => !rule.media && rule.selector.replace(/\[data-v-layout-test\]/g, '') === selector).map(rule => rule.declarations))
  assert.equal(style('.repost-sources')['min-width'], '0')
  assert.equal(style('.repost-sources')['max-width'], '100%')
  assert.equal(style('li')['overflow-wrap'], 'anywhere')
  assert.equal(style('ul')['min-width'], '0')
  assert.equal(style('ul')['max-width'], '100%')
  assert.equal(style('ul')['box-sizing'], 'border-box')
  assert.equal(style('a')['overflow-wrap'], 'anywhere')
  assert.equal(style('a')['white-space'], 'normal')
  assert.equal(style('a')['word-break'], 'break-word')
})

const longOriginalURL = 'http://127.0.0.1:18091/samples/orion-original/'
  + 'LongSourceSegment0123456789'.repeat(8)
  + '?review=' + 'abcdefghij'.repeat(12)
const longRepostURL = longOriginalURL.replace('/orion-original/', '/orion-repost/')

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const width of [390, 1440]) {
    test(`${locale} detail at ${width} keeps the full long original source and repost URLs in their actual sections`, async () => {
      const html = await renderDetails([source({ original_url: longRepostURL })], locale, {
        environment: { window: { innerWidth: width } },
        details: { type: 'url', source: longOriginalURL, metadata: { intake: {
          source: 'rss', source_info: { original_url: longOriginalURL, publisher: 'Original publisher' },
        } } },
      })
      assert.equal(longOriginalURL.length, 390, 'the original URL recreates the browser recheck fixture')
      assert.ok(drawerTag(html).includes(`data-drawer-size="${width === 390 ? '100vw' : '654px'}"`))
      const intake = html.match(/<section class="setting-drawer__section intake-review-panel">[\s\S]*?<\/section>/)?.[0]
      assert.ok(intake, 'the actual original preview panel is rendered')
      assert.ok(intake.includes(messages[locale].library.intakePreviewOriginal))
      assert.ok(intake.includes(`<a href="${longOriginalURL}" target="_blank" rel="noopener">${longOriginalURL}</a>`),
        'the full original preview URL retains its exact href and visible text')
      const urlSource = html.match(/<div class="url_link_box">[\s\S]*?<\/a><\/div>/)?.[0]
      assert.ok(urlSource, 'the URL source row with its inline flex link is rendered')
      assert.ok(urlSource.includes(`href="${longOriginalURL}"`))
      assert.ok(urlSource.includes(`<span class="url_text">${longOriginalURL}</span>`))
      const repost = repostBlock(html)
      assert.ok(repost.includes(`<a href="${longRepostURL}" target="_blank" rel="noopener noreferrer">${longRepostURL}</a>`))
    })
  }
}

test('compiled source anchor and inline flex URL styles allow shrinking and wrapping at every viewport width', async () => {
  const rules = await compiledStyleRules(detailSFC)
  const style = (selector: string) => Object.assign({}, ...rules.filter(rule => !rule.media && rule.selector.replace(/\[data-v-layout-test\]/g, '') === selector).map(rule => rule.declarations))
  const anchor = style('.doc-drawer-body a')
  for (const [property, value] of Object.entries({ 'min-width': '0', 'max-width': '100%', 'white-space': 'normal', 'overflow-wrap': 'anywhere', 'word-break': 'break-word' })) {
    assert.equal(anchor[property], value, `the actual original preview anchor receives ${property} outside media rules`)
  }
  const parent = style('.doc-detail-value')
  assert.equal(parent.display, 'inline-flex')
  assert.equal(parent['min-width'], '0', 'existing detail value flex containers can shrink')
  for (const selector of ['.url_link_box', '.url_link_box .url_link', '.url_link_box .url_link .url_text']) {
    assert.equal(style(selector)['min-width'], '0', selector)
    assert.equal(style(selector)['max-width'], '100%', selector)
  }
  assert.equal(style('.url_link_box')['box-sizing'], 'border-box')
  const urlText = style('.url_link_box .url_link .url_text')
  assert.equal(urlText['white-space'], 'normal')
  assert.equal(urlText['overflow-wrap'], 'anywhere')
  assert.equal(urlText['word-break'], 'break-word')
})

test('the original preview URL has a bounded block container at desktop and narrow widths', async () => {
  const rules = await compiledStyleRules(detailSFC)
  const selector = '.doc-drawer-body .intake-review-panel > a'
  const style = Object.assign({}, ...rules.filter(rule => !rule.media
    && rule.selector.replace(/\[data-v-layout-test\]/g, '') === selector).map(rule => rule.declarations))
  for (const [property, value] of Object.entries({
    display: 'block', width: '100%', 'min-width': '0', 'max-width': '100%',
    'box-sizing': 'border-box', 'white-space': 'normal', 'overflow-wrap': 'anywhere', 'word-break': 'break-word',
  })) {
    assert.equal(style[property], value, `the original preview link receives ${property} outside media rules`)
  }
  assert.equal(style['overflow-x'], undefined, 'the source wraps rather than adding a horizontal scrolling region')
  assert.equal(style['text-overflow'], undefined, 'the complete original URL remains visible')
})

function relativeLuminance(hex: string) {
  assert.match(hex, /^#(?:[a-f\d]{3}|[a-f\d]{6})$/i, 'contrast is calculated from the actual opaque theme colors')
  const color = hex.length === 4 ? hex.slice(1).split('').map(value => value.repeat(2)).join('') : hex.slice(1)
  const linear = color.match(/.{2}/g)!.map(value => {
    const channel = parseInt(value, 16) / 255
    return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
  })
  return linear[0]! * 0.2126 + linear[1]! * 0.7152 + linear[2]! * 0.0722
}

test('dark original preview links and visited links use the theme token with body-text AA contrast', async () => {
  const rules = await compiledStyleRules(detailSFC)
  const anchor = '[theme-mode="dark"] .t-drawer.doc-main-drawer .intake-review-panel > a'
  for (const state of ['', ':visited']) {
    const style = Object.assign({}, ...rules.filter(rule => !rule.media && rule.selector === `${anchor}${state}`)
      .map(rule => rule.declarations))
    assert.equal(style.color, 'var(--td-text-color-link)', `the ${state || 'unvisited'} source link overrides the browser color`)
  }
  assert.ok(!rules.some(rule => rule.selector.includes('.intake-review-panel > a') && rule.declarations.color
    && !rule.selector.startsWith('[theme-mode="dark"]')), 'the color correction stays limited to the dark theme')

  const theme = await compileStyleAsync({
    filename: 'theme.css', id: 'source-link-token-test', scoped: false,
    source: readFileSync(new URL('../assets/theme/theme.css', import.meta.url), 'utf8'),
  })
  assert.deepEqual(theme.errors, [])
  const tokens: Record<string, string> = {}
  theme.rawResult!.root.walkRules(rule => {
    if (rule.selector === ':root:root[theme-mode="dark"]') {
      rule.walkDecls(declaration => { tokens[declaration.prop] = declaration.value })
    }
  })
  function resolveToken(name: string): string {
    const value = tokens[name]
    assert.ok(value, `the dark theme defines ${name}`)
    const alias = value.match(/^var\((--[^)]+)\)$/)?.[1]
    return alias ? resolveToken(alias) : value
  }
  const drawer = await compileStyleAsync({
    filename: 'tdesign-drawer.css', id: 'drawer-background-test', scoped: false,
    source: readFileSync(new URL('../../node_modules/tdesign-vue-next/es/drawer/style/index.css', import.meta.url), 'utf8'),
  })
  assert.deepEqual(drawer.errors, [])
  let drawerBackground = ''
  drawer.rawResult!.root.walkRules(rule => {
    if (rule.selector === '.t-drawer__content-wrapper') {
      rule.walkDecls('background', declaration => { drawerBackground = declaration.value })
    }
  })
  assert.equal(drawerBackground, 'var(--td-bg-color-container)', 'contrast uses the installed drawer background token')
  const foreground = relativeLuminance(resolveToken('--td-text-color-link'))
  const background = relativeLuminance(resolveToken('--td-bg-color-container'))
  const contrast = (Math.max(foreground, background) + 0.05) / (Math.min(foreground, background) + 0.05)
  assert.ok(contrast >= 4.5, `the source link contrast is ${contrast.toFixed(2)}:1, below the required 4.5:1 if this fails`)
})

for (const count of [1, 2]) {
  test(`English message details use the segment quantity ${count}`, async () => {
    const html = await renderDetails([], 'en-US', { details: { total: count } })
    assert.ok(html.includes(`Total ${count} ${count === 1 ? 'segment' : 'segments'}<`), html)
  })
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: vault source identity reaches full detail through the production loading hook`, async () => {
    const loader = createArticleDetailsLoader({ converted: { source_kind: 'mine', channel: 'api', file_type: 'md', vault_path: 'personal/Converted.md' }, direct: { source_kind: 'mine', channel: 'api', file_type: 'md', vault_path: 'personal/Direct.md' }, report: { source_kind: 'report', channel: 'api', file_type: 'md', vault_path: 'ai-output/Report.md' } })
    for (const [id, kind] of [['converted', 'mine'], ['direct', 'mine'], ['report', 'report']] as const) {
      const html = await renderDetails([], locale, { loadedDetails: await loader.load(id) })
      assert.ok(html.includes(messages[locale].library.sourceKinds[kind]), id)
      assert.ok(!html.includes(messages[locale].library.typeManual), id)
      assert.ok(html.includes(`>${messages[locale].library.channelApi}<`), id)
      assert.ok(html.includes('Markdown'), id)
    }
    assert.deepEqual(loader.calls, ['converted', 'direct', 'report'])
  })
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: historical manual drafts without chunks retain a read-only body from metadata`, async () => {
    const body = '# Preserved draft\n\nDraft body with a source reference.'
    const loader = createArticleDetailsLoader({ draft: { type: 'manual', parse_status: 'draft', metadata: { content: body, status: 'draft' } } })
    const loadedDetails = await loader.load('draft')
    const html = await renderDetails([], locale, { loadedDetails, scenario: state => {
      assert.equal(state.mergedContent.value, body)
      assert.equal(state.isLegacyManualDraft.value, true)

    } })
    assert.ok(html.includes('Preserved draft'))
    assert.ok(html.includes('Draft body with a source reference.'))
    assert.doesNotMatch(html, /ManualEntryEditor|contenteditable/)
    assert.equal(loadedDetails.metadata.content, body)
  })


}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: personal document detail supports metadata editing and processing alongside deletion`, async () => {
    const deletion = { __esModule: true, default: defineComponent({ props: ['documentId'], setup: props => () => h('button', { 'data-delete-document': props.documentId }, messages[locale].personalDocument.delete) }) }
    const html = await renderDetails([], locale, { details: { source_kind: 'mine', title: 'My imported note', custom_metadata: {} }, personalDelete: deletion })
    assert.match(html, /data-delete-document="message-a"/)
    assert.ok(html.includes(messages[locale].personalDocument.delete))
    assert.ok(html.includes('metadata-empty-action'))
    assert.ok(html.includes(messages[locale].library.addMetadataField))
    assert.ok(html.includes(messages[locale].library.reprocess))
  })
}

for (const width of [390, 1440]) test(`trace drawer is bounded by its ${width}px viewport`, async () => {
  const html = await renderDetails([], 'en-US', {
    environment: { window: { innerWidth: width }, localStorage: { getItem: () => '1200' } },
    scenario: state => state.openTimeline(),
  })
  assert.match(html, /data-drawer-size="min\(820px, 100vw\)"/)
  if (width === 390) assert.ok(!html.includes('trace-drawer-resize-handle'))
})
