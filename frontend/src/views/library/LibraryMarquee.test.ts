import * as bodyReference from '../../utils/bodyReference'
import assert from 'node:assert/strict'
import * as entrySourceLabels from '../../utils/entrySourceLabels'
import * as markdownPlainText from '../../utils/markdownPlainText'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent, h, reactive, ref } from 'vue'
import { useMarqueeSelect } from '../../composables/useMarqueeSelect'
import * as polling from './entryStatusPolling'
import * as documentListFilters from './documentListFilters'
import { SEARCH_SOURCES } from '../../types/search'
import { descendants, element, hasClass, loadComponent, registerPrimitives, renderer, settle } from './componentTestHarness'

const require = createRequire(import.meta.url)
const installedCheckbox = require('tdesign-vue-next').Checkbox

async function mountLibrary(realCheckbox = false) {
  const previous = { document: globalThis.document, window: globalThis.window, Element: globalThis.Element, now: Date.now, storage: Object.getOwnPropertyDescriptor(globalThis, 'localStorage') }
  let now = 1000
  Date.now = () => now
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: () => null, setItem() {} } })
  const listeners = new Map<string, Set<(event: any) => void>>()
  globalThis.Element = class { static [Symbol.hasInstance](value: any) { return !!value?.tag } } as any
  globalThis.document = {
    body: { style: { userSelect: '', removeProperty() { this.userSelect = '' } } },
    addEventListener(name: string, callback: any) { if (!listeners.has(name)) listeners.set(name, new Set()); listeners.get(name)!.add(callback) },
    removeEventListener(name: string, callback: any) { listeners.get(name)?.delete(callback) },
  } as any
  globalThis.window = { innerHeight: 900, addEventListener() {}, removeEventListener() {} } as any
  const items = (realCheckbox ? ['a', 'b', 'c', 'd'] : ['a', 'b', 'c']).map(id => ({ id, type: 'manual', parse_status: 'draft', file_name: id, created_at: '2026-10-12T00:00:00Z' }))
  const cardList = ref(items)
  const stub = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
  const opened: string[] = []
  const modules: Record<string, any> = {
    '@/utils/bodyReference': bodyReference,
    '@/composables/useLibraryScope': { useLibraryScope: () => ref([]) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'vue-router': { useRoute: () => ({ query: {} }), useRouter: () => ({ replace() {} }) },
    '@/composables/useMarqueeSelect': { useMarqueeSelect },
    '@/composables/useLibrary': { __esModule: true, default: () => ({
      cardList, total: ref(items.length), moreIndex: ref(-1), details: reactive({}),
      loadEntries: async () => { cardList.value = [...items] },
      getCardDetails: (item: { id: string }) => opened.push(item.id),
    }) },
    '@/stores/ui': { useUIStore: () => ({}) },
    '@/stores/pendingCounts': { usePendingCountsStore: () => ({ total: null, refresh: async () => {} }) },
    '@/stores/commandPalette': { useCommandPaletteStore: () => ({}) },
    '@/stores/resources': { useResourcesStore: () => ({ library: { id: 'library' }, fetchLibrary: async () => ({ id: 'library' }) }) },
    '@/stores/editorResources': { useEditorResourcesStore: () => ({ parserEngines: [], ensureParserEngines: async () => {} }) },
    '@/stores/reparseConfirm': { useReparseConfirmStore: () => ({}) },
    '@/types/entryClassification': { CONTENT_TYPES: [] },
    '@/types/search': { SEARCH_SOURCES },
    '@/api/entities': { listEntities: async () => ({ success: true, data: [] }) },
    '@/api/topics': { listTopics: async () => ({ success: true, data: [] }) },
    '@/api/library': {},
    '@/utils/entryTrace': {},
    '@/utils/entrySourceLabels': entrySourceLabels,
    '@/utils/markdownPlainText': markdownPlainText,
    '@/utils/files': { formatFileSize: () => '', getFileIcon: () => '' },
    '@/assets/img/more-actions.png': { __esModule: true, default: '' },
    'tdesign-vue-next': { MessagePlugin: {} },
    './entryStatusPolling': polling,
    './entryDownloadFileName': {},
    './documentListFilters': documentListFilters,
  }
  for (const file of ['./Library.vue', './components/DocumentCardView.vue', './components/DocumentListView.vue']) {
    const url = new URL(file, import.meta.url)
    for (const match of readFileSync(url, 'utf8').matchAll(/from\s+['"]([^'"]+\.vue)['"]/g)) modules[match[1]] = stub
  }
  modules['@/components/SelectionCheckbox.vue'] = { __esModule: true,
    default: loadComponent(new URL('../../components/SelectionCheckbox.vue', import.meta.url), modules) }
  const baseline = process.env.RELIQUARY_SELECTION_BASELINE
  function source(file: string) {
    return baseline ? new URL(`../../../../docs/tasks/naming-04/evidence/rework-1/${baseline}-${file}`, import.meta.url) :
      new URL(file === 'Library.vue' ? './Library.vue' : `./components/${file}`, import.meta.url)
  }
  for (const view of ['Card', 'List']) modules[`./components/Document${view}View.vue`] = {
    __esModule: true, default: loadComponent(source(`Document${view}View.vue`), modules),
  }
  const app = renderer.createApp(loadComponent(source('Library.vue'), modules))
  registerPrimitives(app)
  app.component('t-checkbox', realCheckbox ? installedCheckbox : defineComponent({ props: ['checked'], emits: ['change'], setup: (props, { emit }) => () =>
    h('input', { checked: props.checked, onChange: (event: any) => emit('change', event.checked, { e: event }) }) }))
  for (const name of ['t-date-range-picker', 't-dropdown', 't-skeleton']) app.component(name, stub.default)
  app.config.globalProperties.$t = (key: string) => key
  const errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  const root = element('root')
  app.mount(root)
  await settle()
  assert.deepEqual(errors, [])
  const cards = () => descendants(root).filter(node => node.props['data-select-id'])
  const selected = () => cards().filter(node => hasClass(node, 'is-selected') || hasClass(node, 'selected')).map(node => node.props['data-select-id'])
  const container = descendants(root).find(node => hasClass(node, 'doc-scroll-container'))!
  function decorate() {
    for (const node of descendants(root)) {
      Object.assign(node, {
        dataset: { selectId: node.props['data-select-id'] }, scrollLeft: 0, scrollTop: 0,
        getBoundingClientRect: () => {
          const index = cards().indexOf(node)
          return index < 0 ? { left: 0, top: 0, right: 400, bottom: 200 } : { left: 20 + 120 * index, top: 20, right: 120 + 120 * index, bottom: 120 }
        },
        closest(selector: string) {
          for (let current: any = node; current; current = current.parent) {
            if (selector.includes('[data-select-id]') && current.props['data-select-id']) return current
            if (selector.split(', ').includes(current.tag)) return current
          }
          return null
        },
      })
    }
    container.querySelectorAll = () => cards()
  }
  function event(target: any, x = 0, y = 0, modifiers = {}) { return { target, button: 0, clientX: x, clientY: y, stopPropagation() {}, preventDefault() {}, ...modifiers } }
  function dispatch(name: string, value: any) { for (const callback of listeners.get(name) || []) callback(value) }
  const mouseEvents: any[] = []
  function domEvent(target: any, type: string, modifiers: Record<string, boolean> = {}) {
    const event = new Event(type, { bubbles: true, cancelable: true })
    Object.defineProperty(event, 'target', { value: target })
    if (type !== 'change') Object.defineProperties(event, { button: { value: 0 },
      shiftKey: { value: !!modifiers.shiftKey }, ctrlKey: { value: !!modifiers.ctrlKey }, metaKey: { value: !!modifiers.metaKey } })
    const path = []
    for (let node = target; node; node = node.parent) path.push(node)
    const invoke = (node: any, capture: boolean) => {
      Object.defineProperty(event, 'currentTarget', { configurable: true, value: node })
      const handler = node.props[`on${type[0].toUpperCase()}${type.slice(1)}${capture ? 'Capture' : ''}`]
      for (const fn of Array.isArray(handler) ? handler : handler ? [handler] : []) fn(event)
    }
    for (const node of [...path].reverse()) { invoke(node, true); if (event.cancelBubble) break }
    if (!event.cancelBubble) for (const node of path) { invoke(node, false); if (event.cancelBubble) break }
    mouseEvents.push({ type, tag: target.tag, class: target.props.class, modifiers, defaultPrevented: event.defaultPrevented })
    return event
  }
  async function mouseCheckbox(index: number, modifiers: Record<string, boolean> = {}, click = true, inputTarget = false) {
    now += 200; decorate()
    const card = cards()[index]
    const input = descendants(card).find(node => node.tag === 'input')!
    const target = inputTarget ? input : descendants(card).find(node => hasClass(node, 't-checkbox__input'))!
    assert.ok(target, 'mouse target is an internal element of the installed TDesign checkbox')
    const down = domEvent(target, 'mousedown', modifiers)
    domEvent(target, 'mouseup', modifiers)
    if (click) {
      const clicked = domEvent(target, 'click', modifiers)
      // Emulate label activation observed in Chrome: Shift on the visible span
      // selects text instead of forwarding to the hidden input. Native change
      // has no modifier properties even when an input click has them.
      if (!clicked.defaultPrevented && (!modifiers.shiftKey || inputTarget)) {
        if (!inputTarget) domEvent(input, 'click', modifiers)
        const changed = domEvent(input, 'change')
        assert.equal('shiftKey' in changed, false, 'native change is an Event without mouse modifiers')
      }
    }
    await settle()
    assert.deepEqual(errors, [])
    assert.equal(descendants(root).some(node => hasClass(node, 'doc-marquee-box')), false)
    return down
  }
  return {
    root, cards, selected, opened, errors, mouseCheckbox, mouseEvents,
    async batch(view: 'card' | 'list') {
      if (view === 'list') {
        descendants(root).find(node => hasClass(node, 'doc-view-toggle-btn') && node.props['aria-pressed'] === false)!.props.onClick()
      } else {
        descendants(root).find(node => hasClass(node, 'batch-manage-btn'))?.props.onClick()
        if (!descendants(root).some(node => node.tag === 'input')) {
          decorate()
          container.props.onMousedown(event(container, 5, 5))
          dispatch('mousemove', event(container, 15, 5))
          dispatch('mouseup', event(container, 15, 5))
        }
      }
      await settle()
    },
    async drag(index: number | null, start: [number, number], end: [number, number], expected: string[], modifiers = {}) {
      now += 200
      decorate()
      const card = index === null ? null : cards()[index]
      const target = card ? descendants(card).find(node => hasClass(node, 'card-time') || hasClass(node, 'row-file-name'))! : container
      container.props.onMousedown(event(target, ...start, modifiers))
      dispatch('mousemove', event(target, ...end, modifiers))
      await settle()
      assert.deepEqual(selected(), expected, 'selection while dragging')
      dispatch('mouseup', event(target, ...end, modifiers))
      if (card) card.props.onClick(event(target, ...end, modifiers))
      await settle()
      assert.deepEqual(selected(), expected, 'selection after mouseup and the following card click')
    },
    async clickCard(index: number) {
      now += 200; decorate()
      const card = cards()[index]
      container.props.onMousedown(event(card, 30 + 120 * index, 30))
      dispatch('mouseup', event(card))
      card.props.onClick(event(card))
      await settle()
    },
    async check(index: number, checked: boolean, modifiers = {}) {
      descendants(cards()[index]).find(node => node.tag === 'input')!.props.onChange({ checked, ...modifiers })
      await settle()
    },
    cleanup() {
      app.unmount(); assert.equal([...listeners.values()].reduce((sum, set) => sum + set.size, 0), 0)
      globalThis.document = previous.document; globalThis.window = previous.window; globalThis.Element = previous.Element; Date.now = previous.now
      if (previous.storage) Object.defineProperty(globalThis, 'localStorage', previous.storage)
      else delete (globalThis as any).localStorage
    },
  }
}

test('the real library and card view keep a subtracted marquee selection after the release click', async () => {
  const env = await mountLibrary()
  try {
    await env.drag(null, [5, 5], [230, 110], ['a', 'b'])
    await env.drag(0, [45, 100], [70, 80], ['b'])
  } finally { env.cleanup() }
})

for (const view of ['card', 'list'] as const) {
  test(`${view}: Shift mouse events on the actual checkbox span select the first through fourth items`, async () => {
    const env = await mountLibrary(true)
    try {
      await env.batch(view)
      await env.mouseCheckbox(0)
      assert.deepEqual(env.selected(), ['a'])
      const down = await env.mouseCheckbox(3, { shiftKey: true })
      assert.deepEqual(env.selected(), ['a', 'b', 'c', 'd'])
      assert.deepEqual(env.mouseEvents.filter(e => e.modifiers.shiftKey).map(e => e.type), ['mousedown', 'mouseup', 'click'])
      assert.equal(down.defaultPrevented, true, 'Shift mousedown prevents native text selection')
    } finally { env.cleanup() }
  })
  test(`${view}: Shift mouse events targeting the actual hidden input retain the range`, async () => {
    const env = await mountLibrary(true)
    try {
      await env.batch(view)
      await env.mouseCheckbox(0)
      await env.mouseCheckbox(3, { shiftKey: true }, true, true)
      assert.deepEqual(env.selected(), ['a', 'b', 'c', 'd'])
    } finally { env.cleanup() }
  })
  for (const modifier of ['ctrlKey', 'metaKey']) {
    test(`${view}: ${modifier} mouse events on the actual checkbox toggle once`, async () => {
      const env = await mountLibrary(true)
      try {
        await env.batch(view)
        await env.mouseCheckbox(0)
        await env.mouseCheckbox(3, { [modifier]: true })
        assert.deepEqual(env.selected(), ['a', 'd'])
        await env.mouseCheckbox(3, { [modifier]: true })
        assert.deepEqual(env.selected(), ['a'])
      } finally { env.cleanup() }
    })
  }
  test(`${view}: macOS Ctrl mouseup without click toggles and leaves the next ordinary click working`, async () => {
    const env = await mountLibrary(true)
    try {
      await env.batch(view)
      await env.mouseCheckbox(0)
      await env.mouseCheckbox(3, { ctrlKey: true }, false)
      assert.deepEqual(env.selected(), ['a', 'd'])
      await env.mouseCheckbox(1)
      assert.deepEqual(env.selected(), ['a', 'b', 'd'])
    } finally { env.cleanup() }
  })
}

test('the real library appends marquee selections and keeps ordinary card clicks working', async () => {
  const env = await mountLibrary()
  try {
    await env.clickCard(0)
    assert.deepEqual(env.opened, ['a'])
    await env.drag(null, [5, 5], [100, 110], ['a'])
    await env.drag(1, [150, 30], [230, 110], ['a', 'b'])
    await env.clickCard(2)
    assert.deepEqual(env.selected(), ['a', 'b', 'c'])
    await env.clickCard(0)
    assert.deepEqual(env.selected(), ['b', 'c'])
  } finally { env.cleanup() }
})

test('explicit checkbox changes retain Shift ranges and Ctrl selection after a marquee', async () => {
  const env = await mountLibrary()
  try {
    await env.drag(null, [5, 5], [100, 110], ['a'])
    await env.check(0, true)
    await env.check(2, true, { shiftKey: true })
    assert.deepEqual(env.selected(), ['a', 'b', 'c'])
    await env.check(1, false, { ctrlKey: true })
    assert.deepEqual(env.selected(), ['a', 'c'])
  } finally { env.cleanup() }
})

test('list view still supports append and subtract marquees and Shift checkbox ranges', async () => {
  const env = await mountLibrary()
  try {
    const listButton = descendants(env.root).find(node => hasClass(node, 'doc-view-toggle-btn') && node.props['aria-pressed'] === false)!
    listButton.props.onClick(); await settle()
    assert.deepEqual(env.errors, [])
    assert.ok(hasClass(env.cards()[0], 'doc-list-row'))
    await env.drag(null, [5, 5], [230, 110], ['a', 'b'])
    await env.drag(0, [45, 100], [70, 80], ['b'])
    await env.check(0, true)
    await env.check(2, true, { shiftKey: true })
    assert.deepEqual(env.selected(), ['a', 'b', 'c'])
  } finally { env.cleanup() }
})
