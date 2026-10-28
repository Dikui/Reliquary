import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRenderer, createSSRApp, defineComponent, h, reactive } from 'vue'
import { compileScript, parse } from '@vue/compiler-sfc'
import { parse as parseHTML } from '@vue/compiler-dom'
import { createRequire } from 'node:module'
import ts from 'typescript'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import { marked } from 'marked'
import markedKatex from 'marked-katex-extension'
import hljs from 'highlight.js'
import { loadComponent } from '../views/library/componentTestHarness'
import { entryPresentationTestModules } from '../views/library/entryPresentationTestModules'
import * as sourceLabels from '../utils/entrySourceLabels'
import * as security from '../utils/security'
import * as normalize from '../utils/markdownTableNormalize'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import { TestElement, doc, settle } from './ui/__tests__/dom'

const url = new URL('./DocContent.vue', import.meta.url)
const empty = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
const modules: Record<string, unknown> = {
  ...entryPresentationTestModules,
  marked: { marked }, 'marked-katex-extension': { __esModule: true, default: markedKatex },
  'katex/dist/katex.min.css': {}, 'highlight.js': { __esModule: true, default: hljs }, 'highlight.js/styles/github.css': {},
  mermaid: { __esModule: true, default: { initialize() {} } },
  '@/api/library': { ENTRY_CHUNK_PAGE_SIZE: 25 }, '@/utils/mermaidViewer': {}, '@/utils/clipboard': {}, '@/utils/api-base': {},
  '@/utils/entrySourceLabels': sourceLabels, '@/utils/markdownTableNormalize': normalize,
  '@/utils/security': { ...security, safeMarkdownToHTML: (value: string) => value, sanitizeHTML: (value: string) => value },
}
for (const [, path] of readFileSync(url, 'utf8').matchAll(/from ['"]([^'"]+\.vue)['"]/g)) modules[path!] = empty
const Component = loadComponent(url, modules)

test('the compiled EntryDetail call casts bare reading to Boolean and never mounts the old drawer', async () => {
  const invocation = readFileSync(new URL('../views/library/EntryDetail.vue', import.meta.url), 'utf8').match(/<DocContent\b[\s\S]*?\/>/)![0]
  const source = `<script setup>
    import { ref } from 'vue'
    import DocContent from './DocContent.vue'
    const props = defineProps(['details'])
    const renderDetails = props.details, entry = props.details, bodyReady = true, reference = undefined
    const content = ref(), imagePreview = ref()
    const state = { loadBody() {}, refresh() {} }, editingChanged = () => {}
    </script><template>${invocation}</template>`
  const descriptor = parse(source, { filename: 'EntryDetailCallsite.vue' }).descriptor
  const compiled = compileScript(descriptor, { id: 'entry-detail-callsite', inlineTemplate: true })
  const observed: any[] = []
  const child = { ...Component, setup(props: any, context: any) {
    observed.push({ reading: props.reading, visible: props.visible })
    return Component.setup(props, context)
  } }
  const module = { exports: {} as any }, require = createRequire(import.meta.url)
  const code = ts.transpileModule(compiled.content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => name === './DocContent.vue' ? { __esModule: true, default: child } : require(name), module, module.exports)
  const app = createSSRApp(module.exports.default, { details: { id: 'reading-entry', title: 'Saved body', md: [{ content: 'The actual body' }], custom_metadata: {}, parse_status: 'completed' } })
  for (const name of new Set(readFileSync(url, 'utf8').match(/\bt-[\w-]+(?=[\s>])/g))) {
    app.component(name, defineComponent({ setup: (_props, { slots }) => () => h('aside', { 'data-legacy-drawer': name === 't-drawer' }, slots.default?.()) }))
  }
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en } }))
  const html = await renderToString(app)
  assert.deepEqual(observed, [{ reading: true, visible: true }])
  assert.match(html, /class="doc-reading"/)
  assert.match(html, /The actual body/)
  assert.doesNotMatch(html, /data-legacy-drawer|doc-main-drawer|doc_content|doc-detail-rows|doc-content-section/)
})

for (const locale of ['zh-CN', 'en-US']) test(`${locale}: the reading layout retains formulas, diagrams, syntax highlighting and protected images`, async () => {
  const body = '# Heading\n\nInline $x^2$ and a formula:\n\n$$\\frac{a}{b}$$\n\n```typescript\nconst value = 1\n```\n\n```mermaid\ngraph TD; A-->B\n```\n\n![Chart](../attachments/figure.png)'
  const app = createSSRApp(Component, { visible: true, reading: true, details: { id: 'entry', title: 'Stored material', source_kind: 'article', vault_path: 'articles/entry.md', parse_status: 'completed', md: [{ content: body }], custom_metadata: {} } })
  for (const name of new Set(readFileSync(url, 'utf8').match(/\bt-[\w-]+(?=[\s>])/g))) app.component(name, empty.default)
  app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } }))
  const html = await renderToString(app)
  assert.ok(html.includes('class="doc-reading"'))
  assert.ok(html.includes('katex'))
  assert.ok(html.includes('hljs-keyword'))
  assert.match(html, /class="mermaid"[^>]*>graph TD; A--&gt;B|class="mermaid"[^>]*>graph TD; A-->B/)
  assert.ok(html.includes('vault://entry/..%2Fattachments%2Ffigure.png'))
  assert.ok(!html.includes('t-drawer'))
})

// Unlike the generic component host, this host replaces v-html children. That
// makes a fresh raw Mermaid node or placeholder image observable after updates.
class MarkdownElement extends TestElement {
  private markup = ''
  get innerHTML() { return this.markup }
  set innerHTML(value: string) {
    this.markup = value
    for (const child of [...this.childNodes]) child.remove()
    const append = (ast: any, parent: TestElement) => {
      if (ast.type === 2) { const text = new MarkdownElement('#text'); text.text = ast.content; parent.appendChild(text) }
      if (ast.type !== 1) return
      const node = new MarkdownElement(ast.tag)
      for (const prop of ast.props) if (prop.type === 6) node.setAttribute(prop.name, prop.value?.content || '')
      for (const child of ast.children) append(child, node)
      parent.appendChild(node)
    }
    for (const ast of parseHTML(value).children) append(ast, this)
  }
  get classList() {
    const values = () => String(this.props.class || '').split(/\s+/).filter(Boolean)
    return { contains: (value: string) => values().includes(value), add: (value: string) => { this.props.class = [...new Set([...values(), value])].join(' ') }, remove: (value: string) => { this.props.class = values().filter(item => item !== value).join(' ') } }
  }
  matches(selector: string): boolean {
    if (selector.includes(',')) return selector.split(',').some(value => this.matches(value.trim()))
    const not = /:not\(([^)]+)\)/.exec(selector)
    if (not) return this.matches(selector.replace(not[0], '')) && !this.matches(not[1]!)
    const classes = [...selector.matchAll(/\.([\w-]+)/g)].map(match => match[1]!)
    if (classes.some(value => !this.classList.contains(value))) return false
    return super.matches(selector.replace(/\.[\w-]+/g, '') || '*')
  }
  get src() { return this.getAttribute('src') || '' }
  set src(value: string) { this.setAttribute('src', value) }
  get type() { return this.getAttribute('type') || '' }
  set type(value: string) { this.setAttribute('type', value) }
  get className() { return this.getAttribute('class') || '' }
  set className(value: string) { this.setAttribute('class', value) }
  get currentSrc() { return this.src }
  get alt() { return this.getAttribute('alt') || '' }
  replaceWith(node: TestElement) { this.parentElement?.insertBefore(node, this); this.remove() }
}
const runtimeRenderer = createRenderer<TestElement, TestElement>({
  createElement: tag => new MarkdownElement(tag),
  createText: text => { const node = new MarkdownElement('#text'); node.text = text; return node },
  createComment: text => { const node = new MarkdownElement('#comment'); node.text = text; return node },
  setText: (node, value) => { node.text = value }, setElementText: (node, value) => { node.textContent = value },
  parentNode: node => node.parentElement, nextSibling: node => node.nextSibling,
  insert: (node, parent, anchor) => parent.insertBefore(node, anchor || null), remove: node => node.remove(),
  patchProp(node, key, previous, value) {
    if (key === 'innerHTML') (node as MarkdownElement).innerHTML = value
    else if (key.startsWith('on')) {
      const name = key.slice(2).toLowerCase()
      if (previous) node.removeEventListener(name, previous)
      if (value) node.addEventListener(name, value)
    } else if (key === 'style') Object.assign(node.style, value)
    else node.props[key] = value
  },
  insertStaticContent(value, parent, anchor) {
    const holder = new MarkdownElement('div'); holder.innerHTML = value
    const children = [...holder.childNodes]
    for (const child of children) parent.insertBefore(child, anchor || null)
    return [children[0]!, children.at(-1)!]
  },
})

async function mountedReading(holdImages = false) {
  const oldCreate = doc.createElement, oldImage = Object.getOwnPropertyDescriptor(globalThis, 'Image')
  doc.createElement = (tag: string) => new MarkdownElement(tag)
  Object.defineProperty(globalThis, 'Image', { configurable: true, value: class {
    onload?: () => void
    set src(_value: string) { queueMicrotask(() => this.onload?.()) }
  } })
  let release!: () => void
  const imageGate = new Promise<void>(resolve => { release = resolve })
  const previews: unknown[] = []
  const view = loadComponent(url, {
    ...modules,
    mermaid: { __esModule: true, default: { initialize() {}, async run({ nodes }: { nodes: MarkdownElement[] }) {
      for (const node of nodes) {
        if (node.getAttribute('data-processed')) continue
        node.setAttribute('data-processed', 'true')
        node.innerHTML = '<svg><title>Rendered chart</title></svg>'
      }
    } } },
    '@/utils/security': { ...modules['@/utils/security'] as object,
      async hydrateProtectedFileImages(root: MarkdownElement) {
        const images = root.querySelectorAll('img') as MarkdownElement[]
        if (holdImages) await imageGate
        for (const image of images) image.src = `blob:${image.alt}`
      },
    },
  })
  const body = '# Heading\n\nOriginal paragraph\n\n```mermaid\ngraph TD; A-->B\n```\n\n![Chart](../attachments/figure.png)'
  const props = reactive({ reading: '', visible: true, details: { id: 'entry', source_kind: 'article', vault_path: 'articles/entry.md', md: [{ content: body }], custom_metadata: {}, parse_status: 'completed' }, reference: undefined as any })
  const app = runtimeRenderer.createApp({ setup: () => () => h(view, { ...props, onPreviewImage: (image: unknown) => previews.push(image) }) })
  for (const name of new Set(readFileSync(url, 'utf8').match(/\bt-[\w-]+(?=[\s>])/g))) app.component(name, empty.default)
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en } }))
  const errors: unknown[] = []; app.config.errorHandler = error => errors.push(error)
  const root = new MarkdownElement('main'); doc.body.appendChild(root); app.mount(root)
  await settle()
  return { root, props, previews, release, errors, async close() {
    app.unmount(); root.remove(); release(); await settle(); doc.createElement = oldCreate
    if (oldImage) Object.defineProperty(globalThis, 'Image', oldImage); else Reflect.deleteProperty(globalThis, 'Image')
  } }
}

test('reference-only and status updates preserve hydrated images and rendered diagrams', async () => {
  const view = await mountedReading()
  try {
    const image = view.root.querySelector('img') as MarkdownElement
    const graph = view.root.querySelector('.mermaid')
    assert.equal(image.src, 'blob:Chart')
    assert.ok(graph?.querySelector('svg'))
    view.props.reference = { paragraph: 2, start: 11, end: 29, body_hash: 'hash' }
    await settle()
    assert.ok(view.root.querySelector('img') === image, 'a reference update must not replace the hydrated image')
    assert.ok(view.root.querySelector('.mermaid') === graph, 'a reference update must not replace the rendered graph')
    assert.ok(graph?.querySelector('svg'))
    assert.ok(view.root.querySelector('[data-reference]'))
    view.props.details = { ...view.props.details, parse_status: 'failed', md: [...view.props.details.md] }
    await settle()
    assert.ok(view.root.querySelector('img') === image)
    assert.ok(view.root.querySelector('.mermaid')?.querySelector('svg'))
    view.root.querySelector('.reading-image')!.click()
    assert.deepEqual(view.previews, [{ src: 'blob:Chart', alt: 'Chart' }])
    view.props.details.md = [{ content: view.props.details.md[0]!.content.replace('Original paragraph', 'Updated paragraph').replace('Chart', 'New chart') }]
    await settle()
    assert.match(view.root.textContent, /Updated paragraph/)
    assert.equal((view.root.querySelector('img') as MarkdownElement).src, 'blob:New chart')
    assert.ok(view.root.querySelector('.mermaid')?.querySelector('svg'))
    assert.deepEqual(view.errors, [])
  } finally { await view.close() }
})

test('a reference update during image loading cannot leave the current body unhydrated', async () => {
  const view = await mountedReading(true)
  try {
    view.props.reference = { paragraph: 2, start: 11, end: 29, body_hash: 'hash' }
    await settle()
    view.release(); await settle()
    assert.equal((view.root.querySelector('img') as MarkdownElement).src, 'blob:Chart')
    assert.ok(view.root.querySelector('.mermaid')?.querySelector('svg'))
    assert.deepEqual(view.errors, [])
  } finally { await view.close() }
})
