import { detailPresentation, entryPresentationTestModules } from '../views/library/entryPresentationTestModules'
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
    (name: string) => modules[name] ?? entryPresentationTestModules[name] ?? (name.endsWith('.vue') ? emptyModule : name.endsWith('.css') ? {} : name.endsWith('.png') ? '/image.png' : require(name)), module, module.exports,
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

async function renderView(name: 'DocumentListView', record: Record<string, unknown>, locale: keyof typeof messages) {
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
  const component = detailPresentation({
    '@/utils/entrySourceLabels': entrySourceLabels,
    marked: { marked: { use() {}, Renderer: class {}, parse: (value: string) => value } },
    'marked-katex-extension': { __esModule: true, default: () => ({}) },
    'highlight.js': { __esModule: true, default: {} },
    mermaid: { __esModule: true, default: { initialize() {} } },
    '@/api/library': { ENTRY_CHUNK_PAGE_SIZE: 20, getEntryDetails: unexpectedRequest },
    '@/utils/security': { createProtectedFileImageScope: () => ({ signal: new AbortController().signal, dispose() {} }), isValidURL: () => false, safeMarkdownToHTML: (value: string) => value, sanitizeHTML: (value: string) => value },
    '@/utils/markdownTableNormalize': { normalizeSpuriousTablePrefixes: (value: string) => value },
    '@/utils/mermaidViewer': {}, '@/utils/clipboard': {},
    '@/views/library/entryDownloadFileName': {}, '@/utils/filePreview': {}, '@/utils/api-base': {},
  })
  const previousDocument = globalThis.document
  globalThis.document = { title: '' } as Document
  try { return await renderComponent(component, { visible: true, details }, locale) }
  finally { globalThis.document = previousDocument }
}

function createLoadingHook(read: (id: string) => Promise<unknown>, records: object[] = []) {
  return evaluate(readFileSync(new URL('../composables/useLibrary.ts', import.meta.url), 'utf8'), {
    pinia: { storeToRefs: () => ({ cardList: ref([]), total: ref(0) }) },
    '../utils/index': { formatStringDate: () => '2026-10-12' },
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
  test(`${locale}: the material list and details separate Markdown format from submission channel`, async () => {
    const record = { id: 'ingest', title: 'Article', file_name: 'Article.md', source_kind: 'article', type: 'manual', file_type: 'manual', channel: 'api', parse_status: 'completed', md: [], total: 0,
      acquisition: 'verbatim', metadata: { intake: { source: 'ingest' } }, ingest_api_key_name: 'Research Bot' }
    const hook = createLoadingHook(async () => ({ success: true, data: record }), [record])
    await hook.loadEntries()
    hook.getCardDetails(record)
    await settle()
    const views = [await renderView('DocumentListView', hook.cardList.value[0], locale), await renderDetail(hook.details, locale)]
    for (const html of views) {
      assert.ok(html.includes('Markdown'))
      assert.ok(html.includes(messages[locale].library.channelApi))
    }
    assert.ok(views[1].includes('Research Bot'))
    const legacy = await renderView('DocumentListView', { ...record, channel: 'web', metadata: { intake: { source: 'manual' } } }, locale)
    assert.ok(legacy.includes(messages[locale].library.channelWeb))
    assert.ok(legacy.includes('Markdown'))
  })
}
