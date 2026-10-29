import { entryPresentationTestModules } from '../views/library/entryPresentationTestModules'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, onServerPrefetch, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { Marked, Renderer } from 'marked'
import ts from 'typescript'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import * as markdownPlainText from '../utils/markdownPlainText'
import * as entrySourceLabels from '../utils/entrySourceLabels'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zh, 'en-US': en }
const empty = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
const source = '# Article heading\n\n- **bold** [read](https://example.com/read)\n\n> _quote_\n\n![remote](https://example.com/chart.png) <img src="https://example.com/photo.png"> <b>HTML</b>'
const record = { id: 'excerpt-article', title: 'Article', file_name: 'Article.md', type: 'manual', parse_status: 'completed', processing_mode: 'full', source_kind: 'article', description: source, updated_at: '2026-10-13T00:00:00Z', metadata: {} }

function evaluate(code: string, modules: Record<string, unknown>) {
  const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const value = { exports: {} as any }
  new Function('require', 'module', 'exports', compiled)((name: string) => modules[name] ?? entryPresentationTestModules[name] ?? (name.endsWith('.vue') ? empty : name.endsWith('.css') ? {} : name.endsWith('.png') ? '/image.png' : require(name)), value, value.exports)
  return value.exports
}

function component(path: string, modules: Record<string, unknown>) {
  const filename = new URL(path, import.meta.url).pathname
  const { descriptor, errors } = parse(readFileSync(filename, 'utf8'), { filename })
  assert.deepEqual(errors, [])
  const script = compileScript(descriptor, { id: filename })
  const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const value = evaluate(script.content, modules).default
  value.ssrRender = evaluate(template.code, modules).ssrRender
  return value
}

async function render(value: any, props: Record<string, unknown>, locale: keyof typeof messages) {
  const app = createSSRApp(value, props)
  app.use(createI18n({ legacy: false, locale, messages }))
  const primitive = defineComponent({ props: ['visible'], setup: (props, { attrs, slots }) => () => props.visible === false ? null : h('div', attrs, [slots.header?.(), slots.default?.()]) })
  for (const name of ['t-drawer', 't-dialog', 't-button', 't-checkbox', 't-tag', 't-icon', 't-tooltip', 't-popup', 't-loading', 't-empty', 't-select', 't-input', 't-input-number', 't-textarea', 't-alert', 't-pagination', 't-switch']) app.component(name, primitive)
  const errors: unknown[] = [], warnings: string[] = []
  app.config.errorHandler = error => errors.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  const context: { teleports?: Record<string, string> } = {}
  const html = await renderToString(app, context)
  assert.deepEqual(errors, [])
  assert.deepEqual(warnings, [])
  return html + Object.values(context.teleports || {}).join('')
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: API-loaded material list uses plain text and article detail keeps rendering Markdown`, async () => {
    const requests: string[] = []
    const api = evaluate(readFileSync(new URL('../api/library.ts', import.meta.url), 'utf8'), {
      '../utils/request': { get: async (url: string) => {
        requests.push(url)
        if (url.startsWith('/api/v1/entries?')) return { success: true, data: [structuredClone(record)], total: 1 }
        if (url === '/api/v1/entries/excerpt-article') return { success: true, data: structuredClone(record) }
        if (url.startsWith('/api/v1/chunks/excerpt-article?')) return { success: true, data: [{ id: 'body', content: source, chunk_index: 0 }], total: 1 }
        throw new Error(`Unexpected request: ${url}`)
      } },
    })
    const hookModule = evaluate(readFileSync(new URL('../composables/useLibrary.ts', import.meta.url), 'utf8'), {
      pinia: { storeToRefs: () => ({ cardList: ref([]), total: ref(0) }) },
      '@/stores/entry': { entryStore: () => ({}) },
      '@/api/library': api,
      '../utils/index': { formatStringDate: () => '2026-10-13' },
      'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    })
    const hook = hookModule.default('excerpts')
    await hook.loadEntries()
    hook.getCardDetails(hook.cardList.value[0])
    await new Promise<void>(resolve => setImmediate(resolve))
    assert.equal(hook.details.md[0].content, source)
    const card = component('../views/library/components/DocumentListView.vue', {
      '@/utils/markdownPlainText': markdownPlainText,
      '@/utils/entrySourceLabels': entrySourceLabels,
      '@/utils/files': { formatFileSize: () => '' },
    })
    const hover = await render(card, { items: hook.cardList.value, selectedIds: new Set(), batchMode: false, traceAvailableById: {} }, locale)
    const image = locale === 'zh-CN' ? '[图片]' : '[Image]'
    assert.ok(hover.includes(`${image} ${image} HTML`))
    assert.match(hover, /bold read/)
    assert.doesNotMatch(hover, /# Article|\*\*bold|!\[remote\]|<b>HTML|chart.png|photo.png/)
    assert.equal(hook.cardList.value[0].description, source)

    const parser = new Marked()
    const identity = (value: string) => value
    const detail = component('./DocContent.vue', {
      marked: { marked: { Renderer, use: parser.use.bind(parser), parse: parser.parse.bind(parser) } },
      'marked-katex-extension': { __esModule: true, default: () => ({}) },
      'highlight.js': { __esModule: true, default: {} },
      mermaid: { __esModule: true, default: { initialize() {} } },
      '@/api/library': api,
      '@/utils/entrySourceLabels': entrySourceLabels,
      '@/utils/security': { createProtectedFileImageScope: () => ({ signal: new AbortController().signal, dispose() {} }),
        isValidURL: () => true, isValidImageURL: () => true, safeMarkdownToHTML: identity, sanitizeHTML: identity,
        createSafeImage: (url: string, alt: string) => `<img src="${url}" alt="${alt}">`,
      },
      '@/utils/markdownTableNormalize': { normalizeSpuriousTablePrefixes: identity },
      '@/utils/mermaidViewer': {}, '@/utils/clipboard': {}, '@/utils/filePreview': {}, '@/utils/api-base': {},
      '@/views/library/entryDownloadFileName': {},
    })
    const html = await render(detail, { visible: true, details: hook.details }, locale)
    assert.match(html, /<h1>Article heading<\/h1>/)
    assert.match(html, /<strong>bold<\/strong>/)
    assert.match(html, /<a href="https:\/\/example.com\/read">read<\/a>/)
    assert.match(html, /<img src="https:\/\/example.com\/chart.png" alt="remote">/)
    assert.match(html, /<img src="https:\/\/example.com\/photo.png">/)
    assert.doesNotMatch(html, /\[图片\]|\[Image\]/)
    assert.equal(hook.details.md[0].content, source)
    assert.equal(requests.length, 3)
  })
}
