import assert from 'node:assert/strict'
import test from 'node:test'
import { createRequire } from 'node:module'
import { createRenderer, defineComponent, h, markRaw, nextTick, ref } from 'vue'
import { createPinia, defineStore } from 'pinia'
import { createI18n } from 'vue-i18n'
import { loadComponent } from '../views/library/componentTestHarness'
import * as references from '../utils/bodyReference'
import * as highlights from './GlobalCommandPalette/useHighlight'
import * as excerpts from '../utils/markdownPlainText'
import * as visible from './GlobalCommandPalette/visibleFileChunks'
import en from '../i18n/locales/en-US'
import zh from '../i18n/locales/zh-CN'

const require = createRequire(import.meta.url)
// Real TDesign components run on Vue's host renderer. Only native browser nodes
// and unrelated API-backed children are replaced; teleport and computed styles
// come from the installed Dialog/Drawer implementations, not a template parser.
function nativeHost() {
  function eventTarget(target: any) {
    const listeners = new Map<string, Set<(event: any) => void>>()
    target.addEventListener = (type: string, listener: (event: any) => void) => { if (!listeners.has(type)) listeners.set(type, new Set()); listeners.get(type)!.add(listener) }
    target.removeEventListener = (type: string, listener: (event: any) => void) => listeners.get(type)?.delete(listener)
    target.listeners = listeners
    return target
  }
  const descendants = (node: any): any[] => [node, ...node.children.flatMap(descendants)]
  function create(tag: string, text = ''): any {
    const node: any = { tag, tagName: tag.toUpperCase(), nodeType: tag.startsWith('#') ? 3 : 1, text, children: [], parentNode: null, props: {}, style: {}, dataset: {}, offsetWidth: 100, clientWidth: 100, clientHeight: 1000, scrollHeight: 1000, offsetHeight: 1000 }
    node.classList = { add() {}, remove() {}, contains(value: string) { return String(node.props.class || '').split(' ').includes(value) } }
    node.setAttribute = (key: string, value: string) => { node.props[key] = value }
    node.removeAttribute = (key: string) => { delete node.props[key] }
    node.getAttribute = (key: string) => node.props[key]
    node.addEventListener = () => {}
    node.removeEventListener = () => {}
    node.getBoundingClientRect = () => ({ width: 100, height: 100, left: 0, top: 0, right: 100, bottom: 100 })
    node.focus = () => { documentObject.activeElement = node }
    node.blur = () => { if (documentObject.activeElement === node) documentObject.activeElement = null }
    node.setSelectionRange = () => {}
    node.getRootNode = () => documentObject
    Object.defineProperty(node, 'ownerDocument', { get: () => documentObject })
    node.contains = (child: any) => descendants(node).includes(child)
    node.querySelectorAll = (selector: string) => descendants(node).filter(item => selector.startsWith('.') ? String(item.props.class || '').split(' ').includes(selector.slice(1)) : item.tag === selector)
    node.querySelector = (selector: string) => node.querySelectorAll(selector)[0] || null
    node.appendChild = (child: any) => { insert(child, node); return child }
    node.removeChild = (child: any) => { remove(child); return child }
    return markRaw(node)
  }
  function remove(node: any) { if (node.parentNode) node.parentNode.children.splice(node.parentNode.children.indexOf(node), 1); node.parentNode = null }
  function insert(node: any, parent: any, anchor?: any) { remove(node); node.parentNode = parent; const index = anchor ? parent.children.indexOf(anchor) : -1; if (index < 0) parent.children.push(node); else parent.children.splice(index, 0, node) }
  const body = create('body'), head = create('head'), documentElement = create('html'), appRoot = create('main')
  appRoot.props.id = 'app'; appRoot.style.transform = 'translateZ(0)'; appRoot.style.isolation = 'isolate'; insert(appRoot, body)
  const documentObject: any = eventTarget({ body, head, documentElement, activeElement: null, createElement: create, createElementNS: (_ns: string, tag: string) => create(tag), addEventListener() {}, removeEventListener() {}, querySelector: (selector: string) => selector === 'body' ? body : selector === 'head' ? head : selector === '#app' ? appRoot : body.querySelector(selector) })
  const renderer = createRenderer<any, any>({
    createElement: create, createText: text => create('#text', text), createComment: text => create('#comment', text),
    setText: (node, text) => { node.text = text }, setElementText: (node, text) => { node.text = text; node.children = [] },
    parentNode: node => node.parentNode, nextSibling: node => node.parentNode?.children[node.parentNode.children.indexOf(node) + 1] || null,
    insert, remove, querySelector: documentObject.querySelector,
    patchProp(node, key, _old, value) { node.props[key] = value; if (key === 'style') Object.assign(node.style, value); if (key === 'value') node.value = value },
  })
  return { body, head, appRoot, documentObject, renderer, descendants, eventTarget }
}
async function settle() { for (let i = 0; i < 4; i++) { await nextTick(); await new Promise(resolve => setTimeout(resolve, 5)) } }

for (const locale of ['en-US', 'zh-CN']) for (const timeline of [false, true]) test(`${locale}: real TDesign search, settings and viewer layers and Escape above ${timeline ? 'timeline' : 'material'} drawer masks`, async context => {
  const host = nativeHost(), previous = new Map<string, PropertyDescriptor | undefined>()
  const windowObject = host.eventTarget({ document: host.documentObject, innerHeight: 1000, innerWidth: 1440, addEventListener() {}, removeEventListener() {}, getComputedStyle: () => ({ transitionDuration: '0s', transitionDelay: '0s', animationDuration: '0s', animationDelay: '0s', transitionProperty: 'none', getPropertyValue: () => '' }) })
  const globals = { document: host.documentObject, window: windowObject, getComputedStyle: windowObject.getComputedStyle, requestAnimationFrame: (callback: () => void) => setTimeout(callback, 0), cancelAnimationFrame: clearTimeout, HTMLElement: class { static [Symbol.hasInstance](value: any) { return !!value?.tag } }, Element: class { static [Symbol.hasInstance](value: any) { return !!value?.tag } }, Document: class { static [Symbol.hasInstance](value: any) { return value === host.documentObject } }, ShadowRoot: class {}, SVGElement: class {} }
  for (const [key, value] of Object.entries(globals)) { previous.set(key, Object.getOwnPropertyDescriptor(globalThis, key)); Object.defineProperty(globalThis, key, { configurable: true, writable: true, value }) }
  const pinia = createPinia()
  const useStore = defineStore('palette-layer-test', { state: () => ({ open: false, initialQuery: 'fixture', recentQueries: [] as string[] }), actions: { closePalette() { this.open = false }, pushRecent() {}, clearRecent() {}, openPalette() { this.open = true } } })
  const store = useStore(pinia), query = ref(''), fileGroups = ref([{ entryId: 'entry', libraryId: 'mine', imagePath: 'chart.png', title: 'Result', chunks: [{ id: 'chunk', entry_id: 'entry', content: 'Snippet', score: 1, chunk_index: 0, match_type: 'keyword' }] }])
  const empty = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
  let app: ReturnType<typeof host.renderer.createApp> | undefined
  const errors: unknown[] = [], navigations: any[] = []
  const detailOpen = ref(true), timelineOpen = ref(timeline)
  try {
    const { Dialog, Drawer, ImageViewer } = require('tdesign-vue-next')
    const modules: Record<string, any> = {
      '@/stores/commandPalette': { useCommandPaletteStore: () => store }, '@/stores/auth': { useAuthStore: () => ({}) },
      'vue-router': { useRoute: () => ({ name: 'search', query: {} }), useRouter: () => ({ push: (value: any) => navigations.push(value) }) },
      './GlobalCommandPalette/useSearch': { useCmdkSearch: () => ({ query, libraryScope: ref([]), composing: ref(false), loading: ref(false), hasSearched: ref(true), fileGroups, clearResults() {}, searchError: ref(''), retry() {} }) },
      './GlobalCommandPalette/visibleFileChunks': visible, './GlobalCommandPalette/useHighlight': highlights, '@/utils/markdownPlainText': excerpts, '@/utils/bodyReference': references,
      './GlobalCommandPalette/commands': { buildCommands: () => [], filterCommands: () => [] }, './LibraryScopeSelect.vue': empty, './SearchImage.vue': { __esModule: true, default: defineComponent({ setup: () => () => h('button', { class: 'search-image-open' }) }) },
      '@/views/settings/RetrievalSettings.vue': { __esModule: true, default: defineComponent({ setup: () => () => h('input', { 'data-settings-input': true }) }) },
    }
    for (const name of ['ResultGroup', 'ResultItem']) modules[`./GlobalCommandPalette/${name}.vue`] = { __esModule: true, default: loadComponent(new URL(`./GlobalCommandPalette/${name}.vue`, import.meta.url), {}) }
    const palette = loadComponent(new URL('./GlobalCommandPalette.vue', import.meta.url), modules)
    const preview = loadComponent(new URL('./ImagePreview.vue', import.meta.url), { 'tdesign-vue-next': { ImageViewer } })
    const previewOpen = ref(false)
    const Root = defineComponent({ setup: () => () => h('div', [
      h(Drawer, { attach: 'body', visible: detailOpen.value, 'onUpdate:visible': (value: boolean) => { detailOpen.value = value }, zIndex: 2000, class: 'doc-main-drawer', header: false, footer: false, closeBtn: false }),
      timeline ? h(Drawer, { attach: 'body', visible: timelineOpen.value, 'onUpdate:visible': (value: boolean) => { timelineOpen.value = value }, zIndex: 2100, class: 'kp-secondary-drawer', header: false, footer: false, closeBtn: false }) : null,
      h(palette), h(preview, { visible: previewOpen.value, images: [], onClose: () => { previewOpen.value = false } }),
    ]) })
    app = host.renderer.createApp(Root); app.use(pinia); app.use(createI18n({ legacy: false, locale, messages: { 'en-US': en, 'zh-CN': zh } }))
    app.component('t-dialog', Dialog); app.component('t-drawer', Drawer)
    app.component('t-icon', defineComponent({ props: ['name'], setup: props => () => h('span', { 'data-icon': props.name }) }))
    for (const name of ['t-tooltip', 't-loading', 't-button']) app.component(name, defineComponent({ setup: (_props, { slots }) => () => h('span', slots.default?.()) }))
    app.config.errorHandler = error => errors.push(error)
    app.mount(host.appRoot); await settle(); store.open = true; await settle()
    assert.deepEqual(errors, [])
    const nodes = () => host.descendants(host.body)
    const byClass = (value: string) => nodes().find(node => String(node.props.class || '').split(' ').includes(value))
    const search = byClass('cmdk-dialog'), detail = byClass('doc-main-drawer')
    assert.ok(search && detail)
    assert.ok(search.parentNode === host.body, 'Actual Dialog context teleports out of the transformed app')
    assert.equal(detail.parentNode, host.body)
    assert.equal(search.style.zIndex, 2400)
    assert.equal(detail.style.zIndex, 2000)
    assert.ok(host.descendants(detail).some(node => node.props.class === 't-drawer__mask'))
    if (timeline) { const trace = byClass('kp-secondary-drawer'); assert.equal(trace.parentNode, host.body); assert.equal(trace.style.zIndex, 2100) }
    const result = byClass('cmdk-item'); assert.ok(result); result.props.onClick(); assert.equal(navigations.length, 1)
    store.open = true; await settle()
    const settingsButton = nodes().find(node => host.descendants(node).some(child => child.props['data-icon'] === 'setting') && node.tag === 'button')
    assert.ok(settingsButton); settingsButton.props.onClick(); await settle()
    const settings = byClass('cmdk-retrieval-drawer'); assert.ok(settings)
    assert.ok(settings.parentNode === host.body, 'Actual settings Drawer is not nested in the app or search Dialog')
    assert.equal(settings.style.zIndex, 2450)
    const input = host.descendants(settings).find(node => node.props['data-settings-input']); assert.ok(input); input.focus(); assert.equal(host.documentObject.activeElement, input)
    previewOpen.value = true; await settle()
    const viewer = byClass('t-image-viewer'); assert.ok(viewer)
    assert.equal(viewer.parentNode, host.body)
    assert.equal(viewer.style.zIndex, 3000, 'Actual image viewer clears both search and settings')
    context.diagnostic(JSON.stringify([detail, ...(timeline ? [byClass('kp-secondary-drawer')] : []), search, settings, viewer].map(node => ({ class: node.props.class, parent: node.parentNode.tag, zIndex: node.style.zIndex }))))
    async function escapeFrom(target: any) {
      let stopped = false, immediate = false
      const event = { key: 'Escape', code: 'Escape', target, defaultPrevented: false,
        preventDefault() { this.defaultPrevented = true }, stopPropagation() { stopped = true },
        stopImmediatePropagation() { immediate = true; stopped = true } }
      const path: any[] = []
      for (let node = target; node; node = node.parentNode) path.push(node)
      path.push(host.documentObject, windowObject)
      for (const node of path) {
        for (const listener of [node.props?.onKeydown, ...(node.listeners?.get('keydown') || [])].flat().filter(Boolean)) {
          Object.assign(event, { currentTarget: node })
          listener(event)
          // Browser-dispatched events allow a microtask checkpoint after each
          // listener. This exposes the old double-close after the popup stack updates.
          await nextTick()
          if (immediate) break
        }
        if (stopped) break
      }
      await settle()
    }
    const lowerLayersRemain = () => {
      assert.equal(detailOpen.value, true, 'Escape must preserve the material drawer')
      assert.equal(timelineOpen.value, timeline, 'Escape must preserve the timeline drawer')
    }
    await escapeFrom(viewer)
    assert.equal(previewOpen.value, false, 'Only the viewer closes first')
    assert.equal(store.open, true)
    assert.ok(byClass('cmdk-retrieval-drawer').props.class.includes('t-drawer--open'))
    lowerLayersRemain()
    await escapeFrom(input)
    assert.ok(!byClass('cmdk-retrieval-drawer').props.class.includes('t-drawer--open'), 'Only settings close next')
    assert.equal(store.open, true)
    lowerLayersRemain()
    const searchInput = byClass('cmdk__input'); assert.ok(searchInput)
    await escapeFrom(searchInput)
    assert.equal(store.open, false, 'Only search closes last')
    lowerLayersRemain()
    store.open = true; await settle()
    await escapeFrom(byClass('search-image-open'))
    assert.equal(store.open, false, 'Escape from the returned thumbnail focus also closes only search')
    lowerLayersRemain()
    assert.deepEqual(errors, [])
  } finally {
    app?.unmount(); await new Promise(resolve => setTimeout(resolve, 320))
    for (const [key, descriptor] of previous) { if (descriptor) Object.defineProperty(globalThis, key, descriptor); else delete (globalThis as any)[key] }
  }
})
