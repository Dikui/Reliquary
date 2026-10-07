import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileScript, compileStyle, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import postcss from 'postcss'
import zhCN from '../../../i18n/locales/zh-CN'
import enUS from '../../../i18n/locales/en-US'
import { safeExternalUrl } from '../../../utils/externalUrl'
import type { KnowledgeAnomaly } from '../../../api/knowledge-anomalies'

const require = createRequire(import.meta.url)
const filename = 'KnowledgeAnomaliesDrawer.vue'
const source = readFileSync(new URL('./KnowledgeAnomaliesDrawer.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename })
const script = compileScript(descriptor, { id: 'anomalies-render-test' })
const template = compileTemplate({
  id: 'anomalies-render-test', filename, source: descriptor.template!.content,
  ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings },
})
assert.deepEqual(template.errors, [])

const compile = (code: string) => ts.transpileModule(code, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
function evaluate(code: string, modules: Record<string, unknown>) {
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', compile(code))(
    (name: string) => modules[name] ?? require(name), module, module.exports,
  )
  return module.exports
}
const anomalyAPI = evaluate(readFileSync(new URL('../../../api/knowledge-anomalies.ts', import.meta.url), 'utf8'), {
  '@/utils/request': { get() {}, post() {} },
})
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
function issue(overrides: Partial<KnowledgeAnomaly> = {}): KnowledgeAnomaly {
  return {
    id: 'adapter-issue', knowledge_ids: ['message-a'], kind: 'source_adapter_failed',
    description: 'expected widgets or embedded data unavailable',
    site: 'kimi.ai', source_url: 'https://kimi.ai/blog/kimi-k2-5.html', adapter: 'kimi-k25',
    reason: 'expected widgets or embedded data unavailable', proposal_type: 'none',
    proposed_tag_ids: [], proposed_remove_tag_ids: [], proposed_delete_tag_id: '', applied_changes: {},
    status: 'pending', reported_by: '', decided_by: '', decision_note: '', result: '',
    created_at: '2026-10-08T05:23:41+08:00', ...overrides,
  }
}
async function renderAnomalies({ locale = 'zh-CN', items = [issue()], failed = false }:
  { locale?: keyof typeof messages; items?: KnowledgeAnomaly[]; failed?: boolean } = {}) {
  const errors: unknown[] = []
  const failures: string[] = []
  let drawerSize = ''
  let setupState: any
  const modules: Record<string, unknown> = {
    'tdesign-vue-next': { MessagePlugin: { error: (message: string) => failures.push(message) }, DialogPlugin: {} },
    '@/utils/externalUrl': { safeExternalUrl },
    '@/api/knowledge-base': { listKnowledgeTags: () => Promise.resolve({ data: { data: [], total: 0 } }) },
    '@/api/knowledge-anomalies': {
      ...anomalyAPI,
      listKnowledgeAnomalies: () => failed ? Promise.reject(new Error()) : Promise.resolve({ data: items, has_more: false }),
    },
  }
  const component = evaluate(script.content, modules).default
  component.ssrRender = evaluate(template.code, modules).ssrRender
  const setup = component.setup
  component.setup = (props: object, context: object) => {
    const state = setup(props, context)
    setupState = state
    onServerPrefetch(async () => { await state.load(); await nextTick() })
    return state
  }
  const app = createSSRApp(component, { visible: true, kbId: 'kb-a', isOwner: false, tags: [] })
  app.use(createI18n({ legacy: false, locale, messages }))
  const stub = defineComponent({
    props: ['header', 'description', 'visible', 'loading', 'options', 'size'],
    setup(props, { attrs, slots }) {
      return () => h('div', attrs, [props.header, props.description, slots.default?.()])
    },
  })
  const drawer = defineComponent({
    props: ['size', 'header'],
    setup(props, { slots }) {
      return () => { drawerSize = props.size; return h('div', [props.header, slots.default?.()]) }
    },
  })
  app.component('t-drawer', drawer)
  for (const name of ['t-input', 't-select', 't-textarea', 't-button', 't-tag', 't-loading', 't-empty']) app.component(name, stub)
  app.config.errorHandler = error => errors.push(error)
  const html = await renderToString(app)
  assert.deepEqual(errors, [], 'the actual anomaly SFC renders without an exception')
  return { html, failures, state: setupState, drawerSize }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale} adapter issue displays localized type and every source field`, async () => {
    const { html } = await renderAnomalies({ locale })
    const copy = messages[locale].knowledgeBase.anomalies
    for (const value of [copy.kind.source_adapter_failed, copy.site, copy.sourceUrl, copy.adapter, copy.reason, copy.systemReporter, 'kimi.ai', 'kimi-k25']) {
      assert.ok(html.includes(value), `missing visible field ${value}`)
    }
    assert.match(html, /<a[^>]*href="https:\/\/kimi\.ai\/blog\/kimi-k2-5\.html"[^>]*target="_blank"[^>]*rel="noopener noreferrer"/)
    assert.equal(html.split('expected widgets or embedded data unavailable').length - 1, 1, 'reason appears once when description is the same')
    assert.doesNotMatch(html, /knowledgeBase\.anomalies\./, 'no translation key leaks into the rendered drawer')
  })

  test(`${locale} every backend anomaly kind has a translated rendered title`, async () => {
    const items = anomalyAPI.KNOWLEDGE_ANOMALY_KINDS.map((kind: KnowledgeAnomaly['kind']) => issue({ id: kind, kind }))
    const { html } = await renderAnomalies({ locale, items })
    for (const kind of anomalyAPI.KNOWLEDGE_ANOMALY_KINDS) {
      const label = (messages[locale].knowledgeBase.anomalies.kind as Record<string, string>)[kind]
      assert.equal(typeof label, 'string', `${locale} missing anomaly kind ${kind}`)
      assert.ok(html.includes(label))
    }
    assert.doesNotMatch(html, /knowledgeBase\.anomalies\./)
  })

  test(`${locale} unknown future kind has a localized fallback and retained kind`, async () => {
    const { html } = await renderAnomalies({ locale, items: [issue({ kind: 'future_issue' as KnowledgeAnomaly['kind'] })] })
    assert.ok(html.includes(messages[locale].knowledgeBase.anomalies.kind.other))
    assert.match(html, /future_issue/)
    assert.doesNotMatch(html, /knowledgeBase\.anomalies\.kind\.future_issue/)
  })
}

test('anomaly API kind registry covers all current backend report and adapter kinds', () => {
  const backend = readFileSync(new URL('../../../../../internal/application/service/knowledge_anomaly.go', import.meta.url), 'utf8')
  const manual = backend.match(/validKind := map\[string\]bool\{([^}]+)\}/)?.[1]
  assert.ok(manual)
  const manualKinds = [...manual.matchAll(/"([^"]+)":\s*true/g)].map(match => match[1])
  const adapterBackend = readFileSync(new URL('../../../../../internal/application/service/knowledge_source_adapter.go', import.meta.url), 'utf8')
  const adapterKind = adapterBackend.match(/Kind:\s*"([^"]+)"/)?.[1]
  assert.ok(adapterKind)
  assert.deepEqual([...anomalyAPI.KNOWLEDGE_ANOMALY_KINDS].sort(), [...manualKinds, adapterKind].sort())
})

test('system adapter failure is shown in the list without becoming a manual report option', async () => {
  const { state } = await renderAnomalies()
  assert.deepEqual(state.kindOptions.value.map((option: { value: string }) => option.value),
    ['suspected_error', 'outdated', 'source_conflict', 'manual_conflict', 'tag_deletion'])
})

for (const source_url of ['javascript:alert(1)', 'data:text/html,<script>alert(1)</script>', 'file:///tmp/fixture', '/local-source', '//evil.example/source', 'provider://private/file', 'not a URL']) {
  test(`unsafe source URL stays visible as escaped text: ${source_url}`, async () => {
    const { html } = await renderAnomalies({ items: [issue({ source_url })] })
    assert.doesNotMatch(html, /<a[\s>]/, 'an unsafe URL never becomes a clickable link')
    assert.match(html, /class="anomaly-item__source"/)
    assert.doesNotMatch(html, /<script>/)
  })
}

for (const source_url of ['http://127.0.0.1:18091/samples/adapter-failure.html', 'HTTPS://kimi.ai/blog/source?campaign=p4']) {
  test(`HTTP(S) source URL remains clickable: ${source_url}`, async () => {
    const { html } = await renderAnomalies({ items: [issue({ source_url })] })
    assert.match(html, /<a[^>]*target="_blank"[^>]*rel="noopener noreferrer"/)
  })
}

test('adapter issue with omitted optional metadata still displays description as failure reason', async () => {
  const { html } = await renderAnomalies({ items: [issue({ site: undefined, source_url: undefined, adapter: undefined, reason: undefined })] })
  assert.match(html, /expected widgets or embedded data unavailable/)
  assert.equal(html.split('expected widgets or embedded data unavailable').length - 1, 1)
  assert.doesNotMatch(html, /<a[\s>]|undefined/)
})

test('a separate diagnostic description is retained alongside the specific failure reason', async () => {
  const { html } = await renderAnomalies({ items: [issue({ description: 'Generic capture completed', reason: 'Embedded table not found' })] })
  assert.match(html, /Generic capture completed/)
  assert.match(html, /Embedded table not found/)
})

test('source fields and failure reason are rendered as text without executing HTML', async () => {
  const { html } = await renderAnomalies({ items: [issue({ site: '<img src=x onerror=alert(1)>', reason: '<script>alert(1)</script>' })] })
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/)
  assert.doesNotMatch(html, /<script>|<img/)
})

test('empty list and loading failure preserve the existing localized states', async () => {
  const { html } = await renderAnomalies({ items: [] })
  assert.ok(html.includes(zhCN.knowledgeBase.anomalies.empty))
  const failed = await renderAnomalies({ failed: true })
  assert.deepEqual(failed.failures, [zhCN.knowledgeBase.anomalies.loadFailed])
  assert.ok(failed.html.includes(zhCN.knowledgeBase.anomalies.empty))
})

const styleId = 'data-v-anomalies-render-test'
const compiledStyles = descriptor.styles.map(style => compileStyle({
  id: styleId, source: style.content, filename, scoped: style.scoped,
}))
for (const style of compiledStyles) assert.deepEqual(style.errors, [])
const stylesheet = postcss.parse(compiledStyles.map(style => style.code).join('\n'))
interface StyleElement { tag: string; classes?: string[]; parent?: StyleElement }

function matchesSimpleSelector(element: StyleElement, selector: string): boolean {
  const simple = selector.replaceAll(`[${styleId}]`, '')
  if (simple.includes(':')) return false
  const tag = simple.match(/^[a-z][\w-]*/i)?.[0]
  const classes = [...simple.matchAll(/\.([\w-]+)/g)].map(match => match[1])
  return (!tag || tag === element.tag) && classes.every(name => element.classes?.includes(name))
}
function matchesSelector(element: StyleElement, selector: string): boolean {
  const parts = selector.trim().split(/\s+/)
  if (!matchesSimpleSelector(element, parts.pop()!)) return false
  let ancestor = element.parent
  while (parts.length) {
    let token = parts.pop()!
    const direct = token === '>'
    if (direct) token = parts.pop()!
    while (ancestor && !matchesSimpleSelector(ancestor, token)) {
      if (direct) return false
      ancestor = ancestor.parent
    }
    if (!ancestor) return false
    ancestor = ancestor.parent
  }
  return true
}
function stylesAt(element: StyleElement, viewportWidth: number): Record<string, string> {
  const values: Record<string, string> = {}
  const priorities: Record<string, number> = {}
  stylesheet.walkRules(rule => {
    for (let parent: any = rule.parent; parent; parent = parent.parent) {
      if (parent.type !== 'atrule' || parent.name !== 'media') continue
      const max = parent.params.match(/max-width:\s*(\d+)px/)
      const min = parent.params.match(/min-width:\s*(\d+)px/)
      if (max && viewportWidth > Number(max[1]) || min && viewportWidth < Number(min[1])) return
    }
    for (const selector of rule.selectors) {
      if (!matchesSelector(element, selector)) continue
      const specificity = [...selector.matchAll(/\./g)].length * 10 + [...selector.matchAll(/(?:^|[ >])[a-z]/g)].length
      rule.walkDecls(declaration => {
        if ((priorities[declaration.prop] ?? -1) <= specificity) {
          priorities[declaration.prop] = specificity
          values[declaration.prop] = declaration.value
        }
      })
    }
  })
  return values
}
function inheritedStyle(element: StyleElement, viewport: number, property: string): string | undefined {
  for (let node: StyleElement | undefined = element; node; node = node.parent) {
    const value = stylesAt(node, viewport)[property]
    if (value !== undefined) return value
  }
  return undefined
}

const longURLPrefix = 'https://source.invalid/article?source='
const longSourceURL = longURLPrefix + 'a'.repeat(390 - longURLPrefix.length)
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale} a 390-character anomaly source retains complete text, safe href and viewport-capped drawer size`, async () => {
    const { html, drawerSize } = await renderAnomalies({ locale, items: [issue({ source_url: longSourceURL })] })
    assert.equal(drawerSize, 'min(560px, 100vw)', 'the actual drawer size prop controls TDesign content-wrapper width')
    const anchor = html.match(/<a\b([^>]*)>(.*?)<\/a>/s)
    assert.ok(anchor)
    assert.ok(anchor[1]!.includes(`href="${longSourceURL}"`))
    assert.equal(anchor[2], longSourceURL)
    assert.match(anchor[1]!, /target="_blank"/)
    assert.match(anchor[1]!, /rel="noopener noreferrer"/)
    assert.equal(html.split(longSourceURL).length - 1, 2, 'neither the displayed value nor the link destination is shortened')
  })
}

for (const viewport of [390, 1440]) {
  test(`compiled scoped anomaly CSS keeps source grids shrinkable and complete URL text wrapping at ${viewport}px`, () => {
    const window: StyleElement = { tag: 'div', classes: ['anomaly-window'] }
    const list: StyleElement = { tag: 'div', classes: ['anomaly-list'], parent: window }
    const report: StyleElement = { tag: 'div', classes: ['anomaly-report'], parent: window }
    const item: StyleElement = { tag: 'article', classes: ['anomaly-item'], parent: list }
    const source: StyleElement = { tag: 'dl', classes: ['anomaly-item__source'], parent: item }
    const row: StyleElement = { tag: 'div', parent: source }
    const value: StyleElement = { tag: 'dd', parent: row }
    const link: StyleElement = { tag: 'a', parent: value }
    const unsafeText: StyleElement = { tag: 'span', parent: value }
    assert.ok(compiledStyles[0]!.code.includes(`[${styleId}]`), 'the assertions use the actual scoped compiler output')
    for (const grid of [window, list, report]) {
      assert.equal(stylesAt(grid, viewport).display, 'grid')
      assert.equal(stylesAt(grid, viewport)['grid-template-columns'], 'minmax(0, 1fr)')
    }
    assert.equal(stylesAt(list, viewport)['min-width'], '0')
    assert.equal(stylesAt(row, viewport)['grid-template-columns'], 'max-content minmax(0, 1fr)')
    assert.equal(stylesAt(value, viewport)['min-width'], '0')
    for (const text of [value, link, unsafeText]) {
      assert.equal(inheritedStyle(text, viewport, 'overflow-wrap'), 'anywhere')
      assert.equal(inheritedStyle(text, viewport, 'white-space'), 'pre-wrap')
      assert.notEqual(inheritedStyle(text, viewport, 'text-overflow'), 'ellipsis')
    }
  })
}
