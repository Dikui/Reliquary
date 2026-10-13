import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { createRenderer, defineComponent, h, nextTick, reactive, ref, Teleport } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import * as entrySourceLabels from '../utils/entrySourceLabels'
import * as markdownPlainText from '../utils/markdownPlainText'

const require = createRequire(import.meta.url)
const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')
const evaluate = (code: string, modules: Record<string, unknown>) => {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => name.endsWith('.css') ? {} : modules[name] ?? require(name), module, module.exports)
  return module.exports
}
type Node = { type: string; children: Node[]; props: Record<string, any>; style: Record<string, any>; text?: string; parent?: Node; querySelector: () => null; querySelectorAll: () => [] }
const node = (type = 'root'): Node => ({ type, children: [], props: {}, style: {}, querySelector: () => null, querySelectorAll: () => [] })
let body = node('body')
const renderer = createRenderer<Node, Node>({
  querySelector: selector => selector === 'body' ? body : null, createElement: node, createText: text => ({ ...node('text'), text }), createComment: () => node('comment'),
  setText: (el, text) => { el.text = text }, setElementText: (el, text) => { el.text = text }, patchProp: (el, key, _previous, value) => { el.props[key] = value },
  insert(el, parent, anchor) {
    if (el.parent) el.parent.children.splice(el.parent.children.indexOf(el), 1)
    const index = anchor ? parent.children.indexOf(anchor) : -1
    parent.children.splice(index < 0 ? parent.children.length : index, 0, el); el.parent = parent
  },
  remove: el => { if (el.parent) el.parent.children.splice(el.parent.children.indexOf(el), 1); el.parent = undefined },
  parentNode: el => el.parent ?? null,
  nextSibling: el => el.parent?.children[el.parent.children.indexOf(el) + 1] ?? null,
  insertStaticContent(text, parent) { const el = { ...node('static'), text, parent }; parent.children.push(el); return [el, el] },
})
const flatten = (root: Node): Node[] => [root, ...root.children.flatMap(flatten)]
const event = { stopPropagation() {} }
const settle = async () => { await nextTick(); await new Promise<void>(resolve => setImmediate(resolve)); await nextTick() }

async function mountFlow(path: string, props: Record<string, any>, locale: 'zh-CN' | 'en-US', response: () => Promise<unknown>) {
  const previous = new Map<string, PropertyDescriptor | undefined>()
  for (const [name, value] of Object.entries({ window: { innerWidth: 1440, addEventListener() {}, removeEventListener() {} }, localStorage: { getItem: () => null, setItem() {} }, document: { body: { style: {} }, querySelector: () => null, querySelectorAll: () => [], addEventListener() {}, removeEventListener() {} } })) {
    previous.set(name, Object.getOwnPropertyDescriptor(globalThis, name)); Object.defineProperty(globalThis, name, { configurable: true, value })
  }
  const root = node(), errors: unknown[] = [], warnings: string[] = [], calls: string[] = [], notices: string[] = []
  body = node('body')
  const controls = { drawers: [] as any[], dialogs: [] as any[], popups: [] as any[], confirmations: [] as any[] }
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
  const passthrough = (text: string) => text
  const empty = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
  const api = evaluate(read('../api/knowledge-base.ts'), { '../utils/request': { del: (url: string) => { calls.push(url); return response() } } })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    'tdesign-vue-next': { MessagePlugin: { success: (message: string) => notices.push(message), info: (message: string) => notices.push(message), warning() {}, error() {} } },
    '@/api/knowledge-base': api,
    '@/utils/entrySourceLabels': entrySourceLabels,
    '@/utils/markdownPlainText': markdownPlainText,
    '@/utils/files': { formatFileSize: () => '', getFileIcon: () => 'file' },
    '@/assets/img/more-actions.png': 'more-actions.png',
    marked: { marked: { use() {}, Renderer: class {}, parse: passthrough } }, 'marked-katex-extension': { __esModule: true, default: () => ({}) },
    'highlight.js': { __esModule: true, default: {} }, mermaid: { __esModule: true, default: { initialize() {} } },
    '@/utils/clipboard': { copyWithToast() {} },
    '@/utils/security': { sanitizeHTML: passthrough, safeMarkdownToHTML: passthrough, isValidURL: () => true, hydrateProtectedFileImages: async () => {} },
    '@/utils/markdownTableNormalize': { normalizeSpuriousTablePrefixes: passthrough }, '@/utils/mermaidViewer': {},
    '@/views/knowledge-base/entryDownloadFileName': {}, '@/utils/filePreview': { isKnownPreviewableExt: () => false }, '@/utils/api-base': {},
  }
  const compile = (filename: string) => {
    const text = read(filename), { descriptor, errors } = parse(text, { filename })
    assert.deepEqual(errors, [])
    const script = compileScript(descriptor, { id: filename })
    const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, compilerOptions: { bindingMetadata: script.bindings } })
    assert.deepEqual(template.errors, [])
    const dependencies = { ...modules }
    for (const match of text.matchAll(/from\s+['"]([^'"]+\.vue)['"]/g)) dependencies[match[1]] ??= empty
    const component = evaluate(script.content, dependencies).default
    component.render = evaluate(template.code, dependencies).render
    if (filename === './DocumentDeleteConfirm.vue') {
      const setup = component.setup
      component.setup = (props: object, context: object) => { const state = setup(props, context); controls.confirmations.push(state); return state }
    }
    return { __esModule: true, default: component }
  }
  modules['@/components/DocumentDeleteConfirm.vue'] = compile('./DocumentDeleteConfirm.vue')
  modules['@/components/PersonalDocumentDelete.vue'] = compile('./PersonalDocumentDelete.vue')
  modules['./DocumentActionMenu.vue'] = compile('../views/knowledge-base/components/DocumentActionMenu.vue')
  modules['./DocumentMenuDelete.vue'] = compile('../views/knowledge-base/components/DocumentMenuDelete.vue')
  const component = compile(path).default
  const Drawer = defineComponent({
    props: ['visible', 'closeBtn', 'closeOnOverlayClick', 'closeOnEscKeydown'],
    setup(props, { emit, slots }) {
      controls.drawers.push({ props, close: (source: 'overlay' | 'esc' | 'button') => {
        const allowed = source === 'overlay' ? props.closeOnOverlayClick : source === 'esc' ? props.closeOnEscKeydown : props.closeBtn
        if (allowed !== false) emit('close', { trigger: source })
      }, forceClose: () => emit('close') })
      return () => props.visible ? h('section', { 'data-drawer': true }, [slots.header?.(), slots.default?.()]) : null
    },
  })
  const Dialog = defineComponent({
    props: ['attach', 'mode', 'showOverlay', 'visible', 'header', 'theme', 'closeBtn', 'closeOnOverlayClick', 'closeOnEscKeydown', 'confirmBtn', 'cancelBtn'],
    emits: ['confirm', 'close', 'cancel'],
    setup(props, { emit, slots }) {
      controls.dialogs.push({ props, confirm: () => emit('confirm'), close: (source: 'overlay' | 'esc' | 'button') => {
        const allowed = source === 'overlay' ? props.closeOnOverlayClick : source === 'esc' ? props.closeOnEscKeydown : props.closeBtn
        if (allowed !== false) emit('close', { trigger: source })
      }, forceClose: () => emit('close'), cancel: () => emit('cancel') })
      return () => props.visible ? h(Teleport, { to: props.attach, disabled: !props.attach }, [
        h('section', { 'data-modal-context': true }, [
          props.showOverlay ? h('div', { 'data-dialog-mask': true, onClick: (event: Event) => { event.stopPropagation(); if (props.closeOnOverlayClick !== false) emit('close', { trigger: 'overlay' }) } }) : null,
          h('section', { 'data-dialog': true }, slots.default?.()),
        ]),
      ]) : null
    },
  })
  const Popup = defineComponent({
    props: ['destroyOnClose', 'modelValue'],
    setup(props, { emit, slots }) {
      const visible = ref(true)
      controls.popups.push({ props, close: () => { visible.value = false; emit('update:modelValue', false); emit('visible-change', false) } })
      return () => h('section', { 'data-popup': true }, [slots.default?.(), (props.modelValue ?? visible.value) || !props.destroyOnClose
        ? h('section', { 'data-popup-content': true, hidden: !(props.modelValue ?? visible.value) }, slots.content?.()) : null])
    },
  })
  const Stub = defineComponent({ props: ['message', 'visible'], setup: (props, { attrs, slots }) => () => props.visible === false ? null : h('div', attrs, [props.message, slots.default?.()]) })
  const app = renderer.createApp(component, props)
  app.use(i18n)
  for (const name of ['t-icon', 't-button', 't-tooltip', 't-alert', 't-popup', 't-popconfirm', 't-tag', 't-progress', 't-drawer', 't-dialog', 't-loading', 't-select', 't-space', 't-pagination', 't-checkbox', 't-input', 't-input-number', 't-switch']) app.component(name, name === 't-drawer' ? Drawer : name === 't-dialog' ? Dialog : name === 't-popup' ? Popup : Stub)
  app.config.globalProperties.$t = i18n.global.t
  app.config.errorHandler = error => errors.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  app.mount(root); await settle()
  return {
    root, body, allNodes: () => [...flatten(root), ...flatten(body)], calls, notices, controls, api, modules,
    dispose() {
      app.unmount()
      for (const [name, descriptor] of previous) { if (descriptor) Object.defineProperty(globalThis, name, descriptor); else Reflect.deleteProperty(globalThis, name) }
      assert.deepEqual(errors, []); assert.deepEqual(warnings, [])
    },
  }
}

const item = (source_kind = 'mine') => ({ id: `${source_kind}-fixture`, source_kind, type: 'manual', file_type: 'md', file_name: 'Working note', title: 'Working note', parse_status: 'completed', processing_mode: 'full', md: [], total: 0, metadata: {}, custom_metadata: {}, isMore: true })
function assertLocked(surface: any) {
  assert.equal(surface.props.closeBtn, false)
  assert.equal(surface.props.closeOnOverlayClick, false)
  assert.equal(surface.props.closeOnEscKeydown, false)
  for (const source of ['overlay', 'esc', 'button'] as const) surface.close(source)
  surface.forceClose()
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: mounted personal detail cannot close during deletion and keeps a failed request available for retry`, async () => {
    let finish: (result: unknown) => void = () => {}, closes = 0, deleted = 0
    const response = () => new Promise(resolve => { finish = resolve })
    const props = reactive({ visible: true, details: item(), onCloseDoc: () => { closes++; props.visible = false }, onReviewChanged: (_details: unknown, removed: boolean) => { if (removed) { deleted++; props.visible = false } } })
    const flow = await mountFlow('./DocContent.vue', props, locale, response)
    try {
      const state = flow.controls.confirmations[0], dialog = flow.controls.dialogs[0], drawer = flow.controls.drawers[0]
      const trigger = flatten(flow.root).find(node => node.props.class === 'personal-document-delete')!
      assert.ok(trigger, 'the detail contains its real deletion trigger')
      trigger.props.onClick(event); await settle()
      dialog.confirm(); await settle()
      assert.deepEqual(flow.calls, ['/api/v1/documents/mine-fixture'])
      assertLocked(drawer); assertLocked(dialog)
      dialog.cancel(); dialog.confirm(); await settle()
      assert.equal(props.visible, true); assert.equal(state.visible.value, true); assert.equal(closes, 0); assert.equal(flow.calls.length, 1)
      finish({ success: false, message: 'Storage busy' }); await settle()
      assert.equal(state.busy.value, false); assert.equal(state.visible.value, true)
      assert.ok(flow.allNodes().some(node => node.text === 'Storage busy'), 'the failure remains rendered inside the confirmation')
      assert.equal(drawer.props.closeBtn, true); assert.equal(drawer.props.closeOnOverlayClick, true); assert.equal(drawer.props.closeOnEscKeydown, true)
      dialog.confirm(); await settle(); finish({ success: true }); await settle()
      assert.equal(flow.calls.length, 2); assert.equal(deleted, 1); assert.equal(props.visible, false)
    } finally { flow.dispose() }
  })
}

for (const view of ['DocumentCardView', 'DocumentListView'] as const) for (const source_kind of ['mine', 'article']) {
  test(`mounted ${view}: ${source_kind} deletion closes its menu before opening a body modal, blocks busy closing and retries failure`, async () => {
    let finish: (result: unknown) => void = () => {}, removed = 0, opened = 0
    let removeArticle = async () => false
    const fixture = item(source_kind)
    const props = reactive({ items: [fixture], selectedIds: new Set(), ...(view === 'DocumentCardView' ? { batchMode: false, traceAvailableById: {} } : { traceVisibleIds: {} }), onOpen: () => { opened++ }, onDeleted: (id: string) => { assert.equal(id, fixture.id); removed++ }, deleteEntry: () => removeArticle() })
    const flow = await mountFlow(`../views/knowledge-base/components/${view}.vue`, props, 'en-US', () => new Promise(resolve => { finish = resolve }))
    try {
      if (source_kind === 'article') {
        const hook = evaluate(read('../composables/useKnowledgeBase.ts'), { ...flow.modules,
          pinia: { storeToRefs: () => ({ cardList: ref([fixture]), total: ref(1) }) }, '@/stores/entry': { entryStore: () => ({}) }, '../utils/index': { formatStringDate: () => '' },
        }).default('kb')
        removeArticle = () => hook.delEntry(0, fixture, () => { removed++ })
      }
      const state = flow.controls.confirmations[0], dialog = flow.controls.dialogs[0], popup = flow.controls.popups[0]
      const trigger = flatten(flow.root).find(node => node.type === 'button' && String(node.props.class).includes('doc-action-menu-item danger'))!
      assert.ok(trigger, 'the actual card or row menu exposes a semantic deletion button')
      trigger.props.onClick(event); await settle()
      assert.equal(popup.props.modelValue, false, 'the controlled menu closes before confirmation opens')
      assert.equal(dialog.props.attach, 'body'); assert.equal(dialog.props.mode, 'modal'); assert.equal(dialog.props.showOverlay, true)
      const modal = flow.allNodes().find(node => node.props['data-dialog'])!
      assert.ok(modal, 'the confirmation renders independently from the closed menu')
      for (let ancestor = modal.parent; ancestor; ancestor = ancestor.parent) {
        assert.ok(!ancestor.props['data-popup'] && !ancestor.props['data-popup-content'], 'confirmation has no Popup ancestors')
      }
      assert.ok(flatten(flow.body).includes(modal), 'the dialog is attached to the body')
      assert.equal(flatten(flow.root).some(node => node.props['data-dialog']), false, 'no modal content remains in card or row menu DOM')
      dialog.confirm(); await settle()
      assert.deepEqual(flow.calls, [source_kind === 'mine' ? '/api/v1/documents/mine-fixture' : '/api/v1/entries/article-fixture'])
      assertLocked(dialog); dialog.cancel(); dialog.confirm(); popup.close(); await settle()
      assert.equal(state.visible.value, true); assert.equal(flow.calls.length, 1)
      assert.ok(flow.allNodes().some(node => node.props['data-dialog']), 'closing the menu keeps the pending confirmation mounted')
      const mask = flow.allNodes().find(node => node.props['data-dialog-mask'])!
      let stopped = false
      mask.props.onClick({ stopPropagation() { stopped = true } }); await settle()
      assert.equal(stopped, true); assert.equal(opened, 0, 'the mask consumes clicks without emitting an underlying entry open')
      assert.equal(state.visible.value, true)
      finish({ success: false, message: 'Storage busy' }); await settle()
      assert.equal(state.visible.value, true); assert.equal(state.busy.value, false)
      assert.ok(flow.allNodes().some(node => node.text === (source_kind === 'mine' ? 'Storage busy' : en.knowledgeBase.deleteFailed)))
      dialog.confirm(); await settle(); finish({ success: true }); await settle()
      assert.equal(flow.calls.length, 2); assert.equal(removed, 1); assert.equal(state.visible.value, false)
    } finally { flow.dispose() }
  })
}
