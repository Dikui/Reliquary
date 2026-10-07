import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { compileScript, compileStyleAsync, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zhCN from '../i18n/locales/zh-CN'
import enUS from '../i18n/locales/en-US'
import { safeExternalUrl } from '../utils/externalUrl'
import * as display from '../views/knowledge/components/knowledgeContextDisplay'
import * as scope from '../api/knowledge-context-scope'
import * as citation from '../utils/markdownCitation'
import type { KnowledgeRepostSource } from '../api/knowledge-context'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const compile = (code: string) => ts.transpileModule(code, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
function evaluate(code: string, modules: Record<string, unknown>, environment: Record<string, unknown> = {}) {
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', 'window', 'localStorage', compile(code))(
    (name: string) => name.endsWith('.css') ? {} : modules[name] ?? require(name), module, module.exports,
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
const repostSFC = compileSFC('../views/knowledge/components/RepostSources.vue')
const detailSFC = compileSFC('./doc-content.vue')
const workbenchSFC = compileSFC('../views/knowledge/components/KnowledgeWorkbench.vue')
const actualRepostSources = repostSFC.create({ '@/utils/externalUrl': { safeExternalUrl } })
const sharedRepostModule = { __esModule: true, default: actualRepostSources }
const emptyComponent = defineComponent({ setup: () => () => null })
const emptyModule = { __esModule: true, default: emptyComponent }
const envelope = (data: unknown) => Promise.resolve({ success: true, data })
const unexpectedAPI = () => { throw new Error('The repost rendering test must not call a remote API') }

function source(overrides: Partial<KnowledgeRepostSource> = {}): KnowledgeRepostSource {
  return {
    id: 'repost-a', knowledge_id: 'message-a', publisher: 'P4 News',
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
  for (const name of ['t-drawer', 't-button', 't-icon', 't-tag', 't-tooltip', 't-loading', 't-empty', 't-select', 't-input', 't-input-number', 't-textarea', 't-checkbox', 't-alert', 't-pagination', 't-popup', 't-popconfirm', 't-switch']) app.component(name, stub)
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
  environment?: Record<string, unknown>
  scenario?: (state: any) => void | Promise<void>
  ssrContext?: Record<string, any>
  details?: Record<string, unknown>
}
async function renderDetails(sources: KnowledgeRepostSource[] | null | undefined, locale: keyof typeof messages = 'zh-CN', options: DetailRenderOptions = {}) {
  const component = detailSFC.create({
    marked: { marked: { use() {}, Renderer: class {}, parse: (value: string) => value } },
    'marked-katex-extension': { __esModule: true, default: () => ({}) },
    'highlight.js': { __esModule: true, default: {} },
    mermaid: { __esModule: true, default: { initialize() {} } },
    'tdesign-vue-next': { MessagePlugin: { error: unexpectedAPI, warning: unexpectedAPI } },
    '@/api/knowledge-base/index': { KNOWLEDGE_CHUNK_PAGE_SIZE: 20, getKnowledgeDetails: unexpectedAPI },
    '@/utils/security': { isValidURL: (value: string) => Boolean(safeExternalUrl(value)) },
    '@/utils/markdownTableNormalize': {}, '@/utils/mermaidViewer': {}, '@/utils/wikiLineDiff': {},
    '@/stores/auth': { useAuthStore: () => ({ hasRole: () => false }) },
    '@/components/document-preview.vue': emptyModule,
    '@/components/knowledge-processing-timeline.vue': emptyModule,
    '@/views/knowledge/components/RepostSources.vue': sharedRepostModule,
    '@/views/knowledge/knowledgeDownloadFileName': {}, '@/utils/filePreview': {}, '@/utils/api-base': {},
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
    visible: true, canEditKB: false, canDownloadKB: false, kbId: 'kb-a', knowledgeType: 'document',
    details: { id: 'message-a', title: 'Original source', type: 'manual', parse_status: 'completed',
      summary_status: 'completed', md: [], total: 0, metadata: {}, repost_sources: sources, ...options.details },
  }, locale, options.ssrContext)
}

async function renderWorkbench(sources: KnowledgeRepostSource[], locale: keyof typeof messages) {
  const component = workbenchSFC.create({
    'tdesign-vue-next': { MessagePlugin: { error: unexpectedAPI } },
    './MergeSuggestions.vue': emptyModule, './EvidenceExtractionDetails.vue': emptyModule,
    './ReportMarkdown.vue': emptyModule, './RepostSources.vue': sharedRepostModule,
    './knowledgeContextDisplay': display,
    '@/stores/auth': { useAuthStore: () => ({ hasRole: () => false }) },
    '@/api/knowledge-domains': { listKnowledgeDomains: () => envelope([]) },
    '@/api/knowledge-context-scope': scope,
    '@/api/knowledge-context': {
      listKnowledgeContextEntities: () => envelope([]), listKnowledgeContextEvents: () => envelope([]),
      listKnowledgeContextEvidence: () => envelope([{ id: 'evidence-a', title: 'Original source',
        knowledge_base_id: 'kb-a', body_hash: 'abc', version: 1, status: 'current', extraction_status: 'complete', repost_sources: sources }]),
    },
    '@/api/knowledge-workflows': {}, '@/api/chat/index': {}, '@/utils/markdownCitation': citation,
  })
  const setup = component.setup
  component.setup = (props: object, context: object) => {
    const state = setup(props, context)
    onServerPrefetch(async () => { await state.selectSection('evidence'); await nextTick() })
    return state
  }
  return renderComponent(component, { kbId: 'kb-a', canManage: false }, locale)
}
function repostBlock(html: string) {
  const block = html.match(/<div class="repost-sources"[^>]*>[\s\S]*?<\/ul><\/div>/)?.[0]
  assert.ok(block, 'the actual shared component rendered a repost list')
  return block
}

for (const locale of ['zh-CN', 'en-US'] as const) {
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
    assert.doesNotMatch(block, /knowledgeWorkbench\.sourceTime\./)
  })

  test(`${locale} workbench and detail render the same real RepostSources component`, async () => {
    const detail = await renderDetails(sourceRows, locale)
    const workbench = await renderWorkbench(sourceRows, locale)
    assert.equal(repostBlock(detail), repostBlock(workbench), 'shared rendering gives identical labels, fields and links')
    assert.match(detailSFC.source, /import RepostSources from ['"]@\/views\/knowledge\/components\/RepostSources\.vue['"]/)
    assert.match(workbenchSFC.source, /import RepostSources from ['"]\.\/RepostSources\.vue['"]/)
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
      assert.ok(intake.includes(messages[locale].knowledgeBase.intakePreviewOriginal))
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
