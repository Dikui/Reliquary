import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zhCN from '../i18n/locales/zh-CN'
import enUS from '../i18n/locales/en-US'
import * as entrySourceLabels from './entrySourceLabels'
import * as markdownPlainText from './markdownPlainText'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const emptyModule = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
const settle = () => new Promise<void>(resolve => setImmediate(resolve))

function evaluate(source: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)(
    (name: string) => modules[name] ?? (name.endsWith('.vue') ? emptyModule : name.endsWith('.css') ? {} : name.endsWith('.png') ? '/image.png' : require(name)), module, module.exports,
  )
  return module.exports
}

function createComponent(path: string, modules: Record<string, unknown>) {
  const filename = path.split('/').at(-1)!
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const { descriptor, errors } = parse(source, { filename })
  assert.deepEqual(errors, [])
  const script = compileScript(descriptor, { id: filename })
  const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const component = evaluate(script.content, modules).default
  component.ssrRender = evaluate(template.code, modules).ssrRender
  return component
}

async function renderComponent(component: any, props: Record<string, unknown>, locale: keyof typeof messages) {
  const app = createSSRApp(component, props)
  app.use(createI18n({ legacy: false, locale, messages }))
  const stub = defineComponent({ props: ['visible'], setup: (props, { attrs, slots }) => () => props.visible === false ? null : h('span', attrs, [slots.header?.(), slots.default?.()]) })
  for (const name of ['t-drawer', 't-dialog', 't-button', 't-checkbox', 't-tag', 't-icon', 't-tooltip', 't-popup', 't-loading', 't-empty', 't-select', 't-input', 't-input-number', 't-textarea', 't-alert', 't-pagination', 't-switch']) app.component(name, stub)
  const errors: unknown[] = []
  const warnings: string[] = []
  app.config.errorHandler = error => errors.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  const html = await renderToString(app)
  await nextTick()
  assert.deepEqual(errors, [])
  assert.deepEqual(warnings, [])
  return html
}

async function renderView(name: 'DocumentCardView' | 'DocumentListView', record: Record<string, unknown>, locale: keyof typeof messages) {
  const component = createComponent(`../views/library/components/${name}.vue`, {
    '@/utils/entrySourceLabels': entrySourceLabels,
    '@/utils/markdownPlainText': markdownPlainText,
    '@/utils/files': { formatFileSize: () => '', getFileIcon: () => 'file' },
  })
  return renderComponent(component, {
    items: [{ id: 'article', file_name: 'Article.md', parse_status: 'completed', ...record }],
    selectedIds: new Set(), batchMode: false, traceAvailableById: {}, traceVisibleIds: {},
  }, locale)
}

async function renderDetail(details: Record<string, unknown>, locale: keyof typeof messages) {
  const unexpectedRequest = () => { throw new Error('Rendering the source label must not call an API') }
  const component = createComponent('../components/DocContent.vue', {
    '@/utils/entrySourceLabels': entrySourceLabels,
    marked: { marked: { use() {}, Renderer: class {}, parse: (value: string) => value } },
    'marked-katex-extension': { __esModule: true, default: () => ({}) },
    'highlight.js': { __esModule: true, default: {} },
    mermaid: { __esModule: true, default: { initialize() {} } },
    'tdesign-vue-next': { MessagePlugin: { error: unexpectedRequest, warning: unexpectedRequest } },
    '@/api/library': { ENTRY_CHUNK_PAGE_SIZE: 20, getEntryDetails: unexpectedRequest },
    '@/utils/security': { isValidURL: () => false, safeMarkdownToHTML: (value: string) => value, sanitizeHTML: (value: string) => value },
    '@/utils/markdownTableNormalize': { normalizeSpuriousTablePrefixes: (value: string) => value },
    '@/utils/mermaidViewer': {}, '@/utils/clipboard': {},
    '@/views/library/entryDownloadFileName': {}, '@/utils/filePreview': {}, '@/utils/api-base': {},
  })
  return renderComponent(component, { visible: true, details }, locale)
}

function createLoadingHook(read: (id: string) => Promise<unknown>, records: object[] = []) {
  return evaluate(readFileSync(new URL('../composables/useLibrary.ts', import.meta.url), 'utf8'), {
    pinia: { storeToRefs: () => ({ cardList: ref([]), total: ref(0) }) },
    '../utils/index': { formatStringDate: () => '2026-10-12' },
    'tdesign-vue-next': { MessagePlugin: {} },
    '@/stores/entry': { entryStore: () => ({}) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    '@/api/library': {
      listEntryFiles: async () => ({ success: true, data: records, total: records.length }),
      getEntryDetails: read,
      getEntryDetailsCon: async () => ({ success: true, data: [], total: 0 }),
    },
  }).default('library-source-labels')
}

test('ingest labels use original intake evidence and preserve legacy manual, RSS and web origins', () => {
  const base = { source_kind: 'article', type: 'manual' }
  for (const record of [
    { ...base, metadata: { intake: { source: 'ingest' } } },
    { ...base, channel: 'api', acquisition: 'verbatim' },
    { ...base, channel: 'api', acquisition: 'extracted' },
    { ...base, channel: 'api', acquisition: 'model_written' },
    { ...base, channel: 'api', ingest_api_key_name: 'Research Bot' },
    { ...base, channel: 'api', ingest_api_key_id: 42 },
  ]) assert.equal(entrySourceLabels.isIngestAPIEntry(record), true, JSON.stringify(record))
  for (const record of [
    base,
    { ...base, channel: 'web' },
    { ...base, channel: 'api', metadata: { intake: { source: 'manual' } } },
    { ...base, channel: 'api', acquisition: 'verbatim', metadata: { intake: { source: 'manual' } }, last_ingest_api_key_name: 'Supplement Bot' },
    { ...base, channel: 'api', last_ingest_api_key_name: 'Supplement Bot' },
    { ...base, channel: 'rss', acquisition: 'extracted', last_ingest_api_key_name: 'Supplement Bot' },
    { ...base, channel: 'web', acquisition: 'verbatim', last_ingest_api_key_name: 'Supplement Bot' },
    { ...base, channel: 'api', acquisition: 'unknown' },
    { ...base, channel: 'api', ingest_api_key_name: '  ' },
    { ...base, source_kind: 'mine', metadata: { intake: { source: 'ingest' } } },
    { ...base, source_kind: 'report', channel: 'api', acquisition: 'verbatim' },
  ]) assert.equal(entrySourceLabels.isIngestAPIEntry(record), false, JSON.stringify(record))
})

test('external tool names use the initial API key and fall back to the latest key without exposing an ID', () => {
  assert.equal(entrySourceLabels.getExternalToolName({ ingest_api_key_name: ' Research Bot ', last_ingest_api_key_name: 'Supplement Bot' }), 'Research Bot')
  assert.equal(entrySourceLabels.getExternalToolName({ ingest_api_key_name: ' ', last_ingest_api_key_name: ' Supplement Bot ' }), 'Supplement Bot')
  assert.equal(entrySourceLabels.getExternalToolName({ ingest_api_key_id: 42 }), '')
  assert.equal(entrySourceLabels.getExternalToolName({}), '')
})

test('actual detail loading preserves API key names and clears them immediately when switching articles', async () => {
  const responses = new Map<string, (value: unknown) => void>()
  const hook = createLoadingHook(id => new Promise(resolve => responses.set(id, resolve)))
  hook.getCardDetails({ id: 'ingest' })
  responses.get('ingest')!({ success: true, data: { id: 'ingest', ingest_api_key_name: 'Research Bot', last_ingest_api_key_name: 'Supplement Bot' } })
  await settle()
  assert.equal(hook.details.ingest_api_key_name, 'Research Bot')
  assert.equal(hook.details.last_ingest_api_key_name, 'Supplement Bot')
  hook.getCardDetails({ id: 'legacy' })
  assert.equal(hook.details.ingest_api_key_name, '')
  assert.equal(hook.details.last_ingest_api_key_name, '')
  hook.getCardDetails({ id: 'newer' })
  responses.get('newer')!({ success: true, data: { id: 'newer', ingest_api_key_name: 'Current Bot' } })
  await settle()
  responses.get('legacy')!({ success: true, data: { id: 'legacy', ingest_api_key_name: 'Stale Bot', last_ingest_api_key_name: 'Stale Supplement' } })
  await settle()
  assert.equal(hook.details.id, 'newer')
  assert.equal(hook.details.ingest_api_key_name, 'Current Bot')
  assert.equal(hook.details.last_ingest_api_key_name, '')
  hook.getCardDetails({ id: 'no-key' })
  responses.get('no-key')!({ success: true, data: { id: 'no-key', type: 'manual', channel: 'web', metadata: null } })
  await settle()
  assert.equal(hook.details.ingest_api_key_name, '')
  assert.equal(hook.details.last_ingest_api_key_name, '')
})

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: actual article details show the ingest label and external tool name without relabeling old manual articles`, async () => {
    const record = { id: 'article', title: 'Article', source_kind: 'article', type: 'manual', channel: 'api', parse_status: 'completed', md: [], total: 0, metadata: { intake: { source: 'ingest' } }, ingest_api_key_name: 'Research & Analysis Bot', last_ingest_api_key_name: 'Supplement Bot' }
    const hook = createLoadingHook(async () => ({ success: true, data: record }))
    hook.getCardDetails(record)
    await settle()
    const html = await renderDetail(hook.details, locale)
    assert.ok(html.includes(messages[locale].library.typeIngestAPI))
    assert.ok(html.includes(messages[locale].library.externalTool))
    assert.ok(html.includes('Research &amp; Analysis Bot'))
    assert.ok(!html.includes('Supplement Bot'))
    assert.ok(!html.includes(messages[locale].library.typeManual))
    const fallback = await renderDetail({ ...record, ingest_api_key_name: ' ', last_ingest_api_key_name: ' Supplement Bot ' }, locale)
    assert.ok(fallback.includes('Supplement Bot'))
    for (const record of [
      { ...hook.details, metadata: { intake: { source: 'ingest' } }, ingest_api_key_name: ' ', last_ingest_api_key_name: ' ' },
      { ...hook.details, metadata: null, acquisition: '', ingest_api_key_name: '', last_ingest_api_key_name: '' },
    ]) {
      const empty = await renderDetail(record, locale)
      assert.ok(!empty.includes(messages[locale].library.externalTool), 'empty tool names do not create an empty detail row')
    }
    for (const channel of ['web', 'api']) {
      const legacy = await renderDetail({ ...hook.details, metadata: { intake: { source: 'manual' } }, acquisition: 'verbatim', channel, ingest_api_key_name: '', last_ingest_api_key_name: 'Supplement Bot' }, locale)
      assert.ok(legacy.includes(messages[locale].library.typeManual), channel)
      assert.ok(!legacy.includes(messages[locale].library.typeIngestAPI), channel)
      assert.ok(legacy.includes('Supplement Bot'))
    }
  })
  test(`${locale}: production list loading renders ingest labels in cards and rows while keeping historical labels`, async () => {
    const ingest = { id: 'ingest', source_kind: 'article', type: 'manual', channel: 'api', acquisition: 'verbatim', ingest_api_key_name: 'Research Bot', last_ingest_api_key_name: 'Supplement Bot', metadata: { intake: { source: 'ingest' } }, updated_at: '2026-10-12T08:00:00Z' }
    const hook = createLoadingHook(async () => ({ success: true, data: ingest }), [ingest])
    await hook.loadEntries()
    assert.equal(hook.cardList.value[0].ingest_api_key_name, 'Research Bot')
    for (const view of ['DocumentCardView', 'DocumentListView'] as const) {
      const html = await renderView(view, hook.cardList.value[0], locale)
      assert.ok(html.includes(messages[locale].library.typeIngestAPI), view)
      assert.ok(!html.includes(view === 'DocumentCardView' ? messages[locale].library.typeManual : messages[locale].library.channelManual), view)
      const cases = [
        { record: { type: 'manual', channel: 'web' }, copy: view === 'DocumentCardView' ? messages[locale].library.typeManual : messages[locale].library.channelManual },
        { record: { type: 'manual', channel: 'api', last_ingest_api_key_name: 'Supplement Bot' }, copy: view === 'DocumentCardView' ? messages[locale].library.typeManual : messages[locale].library.channelManual },
        { record: { type: 'manual', channel: 'api', acquisition: 'verbatim', metadata: { intake: { source: 'manual' } }, last_ingest_api_key_name: 'Supplement Bot' }, copy: view === 'DocumentCardView' ? messages[locale].library.typeManual : messages[locale].library.channelManual },
        { record: { type: 'manual', channel: 'rss', acquisition: 'extracted', last_ingest_api_key_name: 'Supplement Bot' }, copy: messages[locale].library.channelRSS },
        { record: { type: 'url', channel: 'web' }, copy: view === 'DocumentCardView' ? messages[locale].library.typeURL : messages[locale].library.channelUrl },
        { record: { source_kind: 'mine', channel: 'api', acquisition: 'verbatim', type: 'manual' }, copy: messages[locale].library.sourceKinds.mine },
        { record: { source_kind: 'report', channel: 'api', acquisition: 'verbatim', type: 'manual' }, copy: messages[locale].library.sourceKinds.report },
      ]
      for (const item of cases) {
        const legacyHTML = await renderView(view, item.record, locale)
        assert.ok(legacyHTML.includes(item.copy), `${view}: ${JSON.stringify(item.record)}`)
        assert.ok(!legacyHTML.includes(messages[locale].library.typeIngestAPI), `${view}: historical origin remains visible`)
      }
    }
  })
}
