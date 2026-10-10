import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { resolve } from 'node:path'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { createRenderer, defineComponent, h, nextTick, reactive } from 'vue'
import ts from 'typescript'

const require = createRequire(import.meta.url)
const detailPath = resolve('src/components/doc-content.vue')
const source = (path: string) => readFileSync(path, 'utf8')
function compile(path: string, text = source(path)) {
  const { descriptor, errors } = parse(text, { filename: path })
  assert.deepEqual(errors, [])
  const script = compileScript(descriptor, { id: path })
  const template = compileTemplate({ id: path, filename: path, source: descriptor.template!.content, compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  return { script, template }
}
function unresolvedReferences(path: string, text = source(path)) {
  const { script, template } = compile(path, text)
  const filename = path + '.ts'
  const content = script.content.replace(/\/\/\s*@ts-nocheck\b/g, '')
  const options: ts.CompilerOptions = { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext, moduleResolution: ts.ModuleResolutionKind.Bundler, skipLibCheck: true, noEmit: true }
  const host = ts.createCompilerHost(options)
  const getSourceFile = host.getSourceFile.bind(host)
  host.getSourceFile = (name, version, onError, createNew) => name === filename ? ts.createSourceFile(name, content, version, true) : getSourceFile(name, version, onError, createNew)
  const program = ts.createProgram([filename], options, host)
  const missing = program.getSemanticDiagnostics(program.getSourceFile(filename)).filter(d => [2304, 2552, 18004].includes(d.code)).map(d => ts.flattenDiagnosticMessageText(d.messageText, '\n'))
  const globals = new Set(['$t', '$emit', '$slots', '$attrs', '$props'])
  for (const match of template.code.matchAll(/\b_ctx\.([\w$]+)/g)) {
    if (!(match[1] in (script.bindings ?? {})) && !globals.has(match[1])) missing.push(`Undefined template reference: ${match[1]}`)
  }
  return [...new Set(missing)]
}

test('detail and related article components have defined script and template references', () => {
  for (const path of [detailPath, resolve('src/components/KnowledgeClassification.vue'), resolve('src/components/knowledge-processing-timeline.vue'), resolve('src/views/knowledge/components/DocumentActionMenu.vue'), resolve('src/views/knowledge/components/DocumentCardView.vue'), resolve('src/views/knowledge/components/DocumentListView.vue'), resolve('src/views/knowledge/components/RepostSources.vue')]) assert.deepEqual(unresolvedReferences(path), [], path)
})

test('reference audit detects deleted callbacks in watchers, lifecycle, expose and template events', () => {
  const fixture = `<script setup lang="ts">// @ts-nocheck\nimport { watch, onUnmounted } from 'vue'; const id = 1; watch(() => id, () => deletedWatcher()); onUnmounted(() => deletedCleanup()); defineExpose({ deletedExpose });</script><template><button @click="deletedEvent">Close</button></template>`
  const missing = unresolvedReferences(detailPath, fixture).join('\n')
  for (const name of ['deletedWatcher', 'deletedCleanup', 'deletedExpose', 'deletedEvent']) assert.ok(missing.includes(name), name)
})

function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => name.endsWith('.css') ? {} : modules[name] ?? require(name), module, module.exports)
  return module.exports
}

type HostNode = { children: HostNode[]; parent?: HostNode; querySelector: () => null; querySelectorAll: () => []; text?: string }
const node = (): HostNode => ({ children: [], querySelector: () => null, querySelectorAll: () => [] })
const renderer = createRenderer<HostNode, HostNode>({
  querySelector: node,
  createElement: node, createText: text => ({ ...node(), text }), createComment: node,
  setText: (el, text) => { el.text = text }, setElementText: (el, text) => { el.text = text }, patchProp() {},
  insert(el, parent, anchor) {
    if (el.parent) el.parent.children.splice(el.parent.children.indexOf(el), 1)
    const index = anchor ? parent.children.indexOf(anchor) : -1
    parent.children.splice(index < 0 ? parent.children.length : index, 0, el); el.parent = parent
  },
  remove(el) { if (el.parent) el.parent.children.splice(el.parent.children.indexOf(el), 1) },
  parentNode: el => el.parent ?? null,
  nextSibling: el => el.parent?.children[el.parent.children.indexOf(el) + 1] ?? null,
  insertStaticContent(text, parent) { const el = { ...node(), text, parent }; parent.children.push(el); return [el, el] },
})

test('mounted detail switches articles and modes, closes and unmounts without undefined references', async () => {
  const previous = new Map<string, PropertyDescriptor | undefined>()
  for (const [name, value] of Object.entries({ window: { innerWidth: 1440, addEventListener() {}, removeEventListener() {} }, localStorage: { getItem: () => null, setItem() {} }, document: { body: { style: {} }, querySelector: () => null, querySelectorAll: () => [], addEventListener() {}, removeEventListener() {} } })) {
    previous.set(name, Object.getOwnPropertyDescriptor(globalThis, name)); Object.defineProperty(globalThis, name, { configurable: true, value })
  }
  const errors: unknown[] = []; const warnings: string[] = []; const unhandled: unknown[] = []
  let imageHydrations = 0
  const rejection = (error: unknown) => unhandled.push(error)
  process.on('unhandledRejection', rejection)
  let app: ReturnType<typeof renderer.createApp> | undefined
  try {
    const empty = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
    const passthrough = (value: string) => value
    const { script, template } = compile(detailPath)
    const detail = evaluate(script.content, {
      marked: { marked: { use() {}, Renderer: class {}, parse: passthrough } }, 'marked-katex-extension': { __esModule: true, default: () => ({}) },
      'highlight.js': { __esModule: true, default: {} }, mermaid: { __esModule: true, default: { initialize() {} } },
      'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) }, 'tdesign-vue-next': { MessagePlugin: { success() {}, warning() {}, error() {} } },
      '@/api/knowledge-base/index': { KNOWLEDGE_CHUNK_PAGE_SIZE: 25 },
      '@/utils/clipboard': { copyWithToast() {} },
      '@/utils/security': { sanitizeHTML: passthrough, safeMarkdownToHTML: passthrough, isValidURL: () => true, hydrateProtectedFileImages: async () => { imageHydrations++ } },
      '@/utils/markdownTableNormalize': { normalizeSpuriousTablePrefixes: passthrough }, '@/utils/mermaidViewer': {},
      '@/components/SafeTextarea.vue': empty, '@/components/document-preview.vue': empty, '@/components/knowledge-processing-timeline.vue': empty, '@/components/KnowledgeClassification.vue': empty,
      '@/views/knowledge/components/RepostSources.vue': empty, '@/views/knowledge/knowledgeDownloadFileName': {}, '@/utils/filePreview': { isKnownPreviewableExt: () => false }, '@/utils/api-base': {},
    }).default
    detail.render = evaluate(template.code, {}).render
    let state: any
    const setup = detail.setup
    detail.setup = (props: object, context: object) => { state = setup(props, context); return state }
    const article = (id: string, mode = 'full') => ({ id, type: 'manual', file_type: 'md', file_name: `${id}.md`, title: id, parse_status: 'completed', processing_mode: mode, md: [], total: 0, metadata: {}, custom_metadata: {} })
    const props = reactive({ visible: true, details: article('event') })
    app = renderer.createApp(defineComponent({ setup: () => () => h(detail, props) }))
    const stub = defineComponent({ props: ['visible'], setup: (props, { slots }) => () => props.visible === false ? null : h('div', slots.default?.()) })
    for (const name of new Set(source(detailPath).match(/\bt-[\w-]+(?=[\s>])/g))) app.component(name, stub)
    app.config.globalProperties.$t = (key: string) => key
    app.config.errorHandler = error => errors.push(error)
    app.config.warnHandler = warning => warnings.push(warning)
    app.mount(node()); await nextTick()
    const beforeVaultMetadata = imageHydrations
    Object.assign(props.details, { source_kind: 'article', vault_path: 'articles/2026-10/event.md' })
    await nextTick(); await nextTick()
    assert.ok(imageHydrations > beforeVaultMetadata, 'late vault metadata hydrates images even when chunks arrived first')
    for (const value of [article('another-event'), article('tutorial', 'summary_only'), article('research'), article('reference', 'summary_only')]) {
      state.chunkPage.value = 3; state.loadedChunkPage.value = 3
      props.details = value; await nextTick()
      assert.deepEqual(errors, []); assert.deepEqual(warnings, [])
      assert.equal(state.chunkPage.value, 1); assert.equal(state.loadedChunkPage.value, 1)
    }
    props.visible = false; await nextTick()
    props.details = article(''); await nextTick()
    props.details = article('reopened-event'); props.visible = true; await nextTick()
    app.unmount(); app = undefined
    await nextTick(); await new Promise(resolve => setImmediate(resolve))
    assert.deepEqual(errors, []); assert.deepEqual(warnings, []); assert.deepEqual(unhandled, [])
  } finally {
    app?.unmount(); process.removeListener('unhandledRejection', rejection)
    for (const [name, descriptor] of previous) { if (descriptor) Object.defineProperty(globalThis, name, descriptor); else Reflect.deleteProperty(globalThis, name) }
  }
})
