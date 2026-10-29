import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import * as piniaModule from 'pinia'
import { defineComponent, h, onMounted, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { Marked, Renderer } from 'marked'
import ts from 'typescript'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import { CONTENT_TYPES } from '../types/entryClassification'
import * as markdownPlainText from '../utils/markdownPlainText'
import * as entrySourceLabels from '../utils/entrySourceLabels'
import * as rfc3339 from '../utils/rfc3339'
import * as highlight from './GlobalCommandPalette/useHighlight'
import { descendants, element, hasClass, registerPrimitives, renderer, settle as settleNodes, visibleText, type Element } from '../views/library/componentTestHarness'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zh, 'en-US': en }
const moduleOf = (value: unknown) => ({ __esModule: true, default: value })
const empty = moduleOf(defineComponent({ setup: () => () => null }))
const summary = '# Summary heading\n\n**bold** [read](https://example.com/summary-link)\n\n![chart](https://example.com/summary-chart.png) <img src="https://example.com/summary-photo.png">\n\n\\*literal\\* `**literal**`'
const structuredSummary = { overview: summary, points: ['- _Point_ [point link](https://example.com/point)'], key_terms: ['`**Term**`', '![term](https://example.com/term.png)'] }
const body = '# Article body\n\n**Body bold** [Body link](https://example.com/body-link)\n\n![body image](https://example.com/body-image.png)'
const sourceRecord = { id: 'summary-article', title: 'Summary article', file_name: 'Summary article.md', type: 'manual', file_type: 'md', source_kind: 'article', content_type: 'research', processing_mode: 'full', parse_status: 'completed', description: summary, structured_summary: structuredSummary, updated_at: '2026-10-13T00:00:00Z', metadata: {}, entities: [], topics: [] }

function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const value = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? (name.endsWith('.vue') ? empty : name.endsWith('.css') ? {} : name.endsWith('.png') ? '/image.png' : require(name)), value, value.exports)
  return value.exports
}

function evaluateFile(path: string, modules: Record<string, unknown>) {
  return evaluate(readFileSync(new URL(path, import.meta.url), 'utf8'), modules)
}

function component(path: string, modules: Record<string, unknown>) {
  const filename = new URL(path, import.meta.url).pathname
  const { descriptor, errors } = parse(readFileSync(filename, 'utf8'), { filename })
  assert.deepEqual(errors, [])
  return evaluate(compileScript(descriptor, { id: filename, inlineTemplate: true }).content, modules).default
}

async function settle() {
  await settleNodes()
  await new Promise<void>(resolve => setImmediate(resolve))
  await settleNodes()
}

function environment(locale: keyof typeof messages) {
  const pinia = piniaModule.createPinia()
  piniaModule.setActivePinia(pinia)
  const calls: { method: string; url: string; body?: unknown }[] = []
  const received: any[] = []
  const record = structuredClone(sourceRecord)
  const feedArticle = { entry_id: record.id, title: record.title, source: 'article', published_at: record.updated_at, collected_at: record.updated_at, entered_at: record.updated_at, is_new: true, matched_follows: [], content_type: record.content_type, processing_mode: record.processing_mode, summary: record.structured_summary, entities: [], topics: [], snippets: [], score: 1, similar_reports: [], original_url: '' }
  async function request(method: string, url: string, requestBody?: unknown) {
    calls.push({ method, url, ...(requestBody === undefined ? {} : { body: requestBody }) })
    let response: any
    if (method === 'get' && url.startsWith('/api/v1/entries?')) response = { success: true, data: [structuredClone(record)], total: 1 }
    else if (method === 'get' && url === `/api/v1/entries/${record.id}`) response = { success: true, data: structuredClone(record) }
    else if (method === 'get' && url.startsWith(`/api/v1/chunks/${record.id}?`)) response = { success: true, data: [{ id: 'body-chunk', content: body, chunk_index: 0 }], total: 1 }
    else if (method === 'post' && url === '/api/v1/follows/feed') response = { success: true, data: { results: [structuredClone(feedArticle)], total: 1, new_count: 1, last_viewed_at: null, viewed_at: record.updated_at } }
    else throw new Error(`Unexpected request: ${method} ${url}`)
    received.push(response)
    return response
  }
  const transport = { get: (url: string) => request('get', url), post: (url: string, value?: unknown) => request('post', url, value) }
  const api = evaluateFile('../api/library.ts', { '../utils/request': transport })
  const storeModule = evaluateFile('../stores/entry.ts', { pinia: piniaModule })
  const route = reactive({ query: {} as Record<string, unknown> })
  const parser = new Marked()
  const identity = (value: string) => value
  const modules: Record<string, unknown> = {
    pinia: piniaModule,
    '@/utils/markdownPlainText': markdownPlainText, '@/utils/entrySourceLabels': entrySourceLabels, '@/utils/rfc3339': rfc3339,
    '@/types/entryClassification': { CONTENT_TYPES }, '@/api/library': api, '@/stores/entry': storeModule,
    '../utils/index': { formatStringDate: () => '2026-10-13' },
    '@/utils/files': { formatFileSize: () => '', getFileIcon: () => 'file' },
    '@/utils/security': { createProtectedFileImageScope: () => ({ signal: new AbortController().signal, dispose() {} }), isValidURL: () => true, isValidImageURL: () => true, safeMarkdownToHTML: identity, sanitizeHTML: identity, hydrateProtectedFileImages: async () => {}, createSafeImage: (url: string, alt: string) => `<img src="${url}" alt="${alt}">` },
    '@/utils/markdownTableNormalize': { normalizeSpuriousTablePrefixes: identity },
    '@/utils/mermaidViewer': {}, '@/utils/clipboard': {}, '@/utils/filePreview': { isKnownPreviewableExt: () => false }, '@/utils/api-base': {}, '@/views/library/entryDownloadFileName': {},
    marked: { marked: { Renderer, use: parser.use.bind(parser), parse: parser.parse.bind(parser) } },
    'marked-katex-extension': moduleOf(() => ({})), 'highlight.js': moduleOf({}), mermaid: moduleOf({ initialize() {} }),
    'tdesign-vue-next': { MessagePlugin: { success() {}, warning() {}, error() {} } },
    'vue-router': { useRoute: () => route, useRouter: () => ({ replace: async () => {}, push: async () => {} }) },
    './GlobalCommandPalette/useHighlight': highlight,
    '@/api/follows': evaluateFile('../api/follows.ts', { '@/utils/request': transport }),
    '@/stores/follows': { useFollowStore: () => ({ libraries: [], targets: [], targetsLoading: false, refreshState: async () => {}, loadTargets: async () => {}, markViewed: async () => ({ last_viewed_at: record.updated_at }) }) },
  }
  const classification = moduleOf(component('./EntryClassification.vue', modules))
  modules['@/components/EntryClassification.vue'] = modules['./EntryClassification.vue'] = classification
  modules['@/components/SearchEntry.vue'] = moduleOf(component('./SearchEntry.vue', modules))
  const errors: unknown[] = [], warnings: string[] = []
  function mount(value: any, select?: any) {
    const root = element('root')
    const app = renderer.createApp(value)
    app.use(pinia)
    app.use(createI18n({ legacy: false, locale, messages }))
    registerPrimitives(app)
    if (select) app.component('t-select', select)
    const primitive = defineComponent({ props: ['visible'], setup: (props, { attrs, slots }) => () => props.visible === false ? null : h('div', attrs, [slots.header?.(), slots.default?.(), slots.operation?.()]) })
    for (const name of ['t-checkbox', 't-input-number', 't-pagination', 't-switch', 't-radio-group', 't-radio-button', 't-empty']) app.component(name, primitive)
    app.component('router-link', defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { href: props.to }, slots.default?.()) }))
    app.config.errorHandler = value => errors.push(value)
    app.config.warnHandler = value => warnings.push(value)
    app.mount(root)
    return { root, close: () => app.unmount() }
  }
  return { record, feedArticle, calls, received, modules, errors, warnings, mount, hook: () => evaluateFile('../composables/useLibrary.ts', modules).default('summary-displays') }
}

function summaryFields(root: Element, localizedImage: string, structured: boolean) {
  const overview = descendants(root).find(node => hasClass(node, structured ? 'summary-overview' : 'summary-description'))!
  assert.ok(overview, 'the summary component rendered its loaded data')
  const text = visibleText(overview)
  for (const value of ['Summary heading', 'bold read', `${localizedImage} ${localizedImage}`, '*literal* **literal**']) assert.ok(text.includes(value), value)
  assert.doesNotMatch(text, /# Summary|\*\*bold\*\*|!\[chart\]|<img|summary-link|summary-chart|summary-photo/)
  assert.ok(descendants(overview).every(node => node.tag !== 'a' && node.tag !== 'img' && !node.props.innerHTML))
  if (structured) {
    assert.ok(visibleText(descendants(root).find(node => hasClass(node, 'summary-points'))!).includes('Point point link'))
    assert.ok(visibleText(descendants(root).find(node => hasClass(node, 'summary-key-terms'))!).includes(`**Term** · ${localizedImage}`))
  }
}

function browserGlobals() {
  const previous = new Map<string, PropertyDescriptor | undefined>()
  for (const [name, value] of Object.entries({ window: { innerWidth: 1440, addEventListener() {}, removeEventListener() {} }, localStorage: { getItem: () => null, setItem() {} }, document: { body: { style: {} }, querySelector: () => null, addEventListener() {}, removeEventListener() {} } })) {
    previous.set(name, Object.getOwnPropertyDescriptor(globalThis, name))
    Object.defineProperty(globalThis, name, { configurable: true, value })
  }
  return () => { for (const [name, descriptor] of previous) { if (descriptor) Object.defineProperty(globalThis, name, descriptor); else Reflect.deleteProperty(globalThis, name) } }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  const image = locale === 'zh-CN' ? '[图片]' : '[Image]'
  for (const structured of [true, false]) {
    test(`${locale}: API and store loaded ${structured ? 'structured' : 'fallback'} summaries render as text in cards, list rows and detail while the body keeps Markdown`, async () => {
      const restore = browserGlobals()
      const env = environment(locale)
      if (!structured) env.record.structured_summary = null as any
      const card = component('../views/library/components/DocumentCardView.vue', env.modules)
      const list = component('../views/library/components/DocumentListView.vue', env.modules)
      const detail = component('./DocContent.vue', env.modules)
      let hook: any
      const mounted = env.mount(defineComponent({
        setup() {
          hook = env.hook()
          const opened = ref(false)
          onMounted(async () => { await hook.loadEntries(); hook.getCardDetails(hook.cardList.value[0]); opened.value = true })
          return () => h('main', [
            h('section', { 'data-display': 'card' }, h(card, { items: hook.cardList.value, selectedIds: new Set(), batchMode: false, traceAvailableById: {} })),
            h('section', { 'data-display': 'list' }, h(list, { items: hook.cardList.value, selectedIds: new Set(), traceVisibleIds: {} })),
            h('section', { 'data-display': 'detail' }, opened.value && hook.details.id ? [h(detail, { visible: true, details: hook.details })] : []),
          ])
        },
      }))
      try {
        await settle()
        assert.deepEqual(env.errors, [])
        for (const display of ['card', 'list', 'detail']) summaryFields(descendants(mounted.root).find(node => node.props['data-display'] === display)!, image, structured)
        const html = descendants(mounted.root).filter(node => hasClass(node, 'md-content')).map(node => node.props.innerHTML || '').join('')
        assert.match(html, /<h1>Article body<\/h1>/)
        assert.match(html, /<strong>Body bold<\/strong>/)
        assert.match(html, /<a href="https:\/\/example.com\/body-link">Body link<\/a>/)
        assert.match(html, /<img src="https:\/\/example.com\/body-image.png" alt="body image">/)
        assert.doesNotMatch(html, /\[图片\]|\[Image\]/)
        assert.equal(hook.cardList.value[0].description, summary)
        assert.equal(hook.details.description, summary)
        assert.deepEqual(hook.details.structured_summary, env.record.structured_summary)
        assert.equal(hook.details.md[0].content, body)
        assert.deepEqual(env.calls.map(call => call.url), ['/api/v1/entries?page=1&page_size=35', `/api/v1/entries/${env.record.id}`, `/api/v1/chunks/${env.record.id}?page=1&page_size=25`])
        assert.deepEqual(env.received[0].data[0], env.record)
        assert.deepEqual(env.received[1].data, env.record)
        assert.deepEqual(env.errors, [])
        assert.deepEqual(env.warnings, [])
      } finally { mounted.close(); restore() }
    })
  }
  test(`${locale}: follow feed loads summaries through its page and API once while preserving escaped and code symbols`, async () => {
    const env = environment(locale)
    const mounted = env.mount(component('../views/library/FollowFeed.vue', env.modules))
    try {
      await settle()
      const article = descendants(mounted.root).find(node => node.props['data-article-id'] === env.record.id)!
      assert.ok(article)
      summaryFields(article, image, true)
      assert.ok(env.calls.some(call => call.method === 'post' && call.url === '/api/v1/follows/feed'))
      const loaded = env.received.find(response => response.data.results)?.data.results[0]
      assert.deepEqual(loaded.summary, structuredSummary)
      assert.deepEqual(env.feedArticle.summary, structuredSummary)
      assert.deepEqual(env.errors, [])
      assert.deepEqual(env.warnings, [])
    } finally { mounted.close() }
  })
}

test('initial detail mount, document switch and refreshed mount render API image descriptions and library type options', async () => {
  const restore = browserGlobals()
  const env = environment('zh-CN')
  env.modules['@/api/topics'] = { listTopics: async () => ({ success: true, data: [] }) }
  env.modules['@/api/entry-judgments'] = {}
  env.modules['@/components/EntryArticleJudgments.vue'] = moduleOf(component('./EntryArticleJudgments.vue', env.modules))
  const detail = component('./DocContent.vue', env.modules)
  let hook: any
  const mounts: ReturnType<typeof env.mount>[] = []
  const setRecord = (id: string, caption: string, type: string) => Object.assign(env.record, {
    id, content_type: type,
    image_descriptions: [{ ordinal: 1, url: 'chart.png', description: caption, locations: [{ paragraph: 3, start: 0, end: 1 }] }],
    library_content_types: [{ name: type, enabled: true, extract_graph: true, position: 0 }, { name: '停用类型', enabled: false, extract_graph: false, position: 1 }],
  })
  const mount = () => {
    const mounted = env.mount(defineComponent({ setup() {
      hook = env.hook()
      onMounted(() => hook.getCardDetails({ id: env.record.id }))
      return () => h(detail, { visible: true, details: hook.details })
    } }), defineComponent({ props: ['options'], setup: props => () => h('select', (props.options || []).map((row: any) => h('option', { value: row.value }, row.label))) }))
    mounts.push(mounted)
    return mounted
  }
  const check = (root: Element, caption: string, type: string) => {
    const section = descendants(root).find(node => hasClass(node, 'image-descriptions'))
    assert.ok(section, 'initial API response reaches the image description component')
    assert.ok(visibleText(section).includes(caption))
    assert.deepEqual(hook.details.library_content_types.filter((row: any) => row.enabled).map((row: any) => row.name), [type])
    assert.ok(descendants(root).some(node => node.tag === 'option' && visibleText(node) === type))
    assert.ok(!descendants(root).some(node => node.tag === 'option' && visibleText(node) === '停用类型'))
    assert.ok(visibleText(root).includes('图片说明'))
    assert.equal(hook.details.md[0].content, body)
  }
  try {
    setRecord('first', '第一张图的数字说明', '机构快讯')
    const first = mount(); await settle(); check(first.root, '第一张图的数字说明', '机构快讯')
    setRecord('second', '第二张图的图表说明', '研究论文')
    hook.getCardDetails({ id: 'second' })
    assert.deepEqual(hook.details.image_descriptions, [])
    assert.deepEqual(hook.details.library_content_types, [])
    await settle(); check(first.root, '第二张图的图表说明', '研究论文')
    assert.ok(!visibleText(first.root).includes('第一张图的数字说明'))
    first.close()
    const refreshed = mount(); await settle(); check(refreshed.root, '第二张图的图表说明', '研究论文')
    assert.deepEqual(env.errors, []); assert.deepEqual(env.warnings, [])
    assert.ok(env.calls.every(call => call.method === 'get'), 'no topic save or reprocessing is needed')
  } finally { for (const mounted of mounts.slice(1)) mounted.close(); if (mounts.length === 1) mounts[0].close(); restore() }
})
