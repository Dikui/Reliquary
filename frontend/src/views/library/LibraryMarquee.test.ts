import { entryPresentationTestModules } from './entryPresentationTestModules'
import zhCN from '../../i18n/locales/zh-CN'
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
const { createI18n } = require('vue-i18n')

async function mountLibrary() {
  const previous = { document: globalThis.document, window: globalThis.window, Element: globalThis.Element, now: Date.now, storage: Object.getOwnPropertyDescriptor(globalThis, 'localStorage') }
  let now = 1000
  Date.now = () => now
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: () => 'table', setItem() {} } })
  const listeners = new Map<string, Set<(event: any) => void>>()
  globalThis.Element = class { static [Symbol.hasInstance](value: any) { return !!value?.tag } } as any
  globalThis.document = {
    body: { style: { userSelect: '', removeProperty() { this.userSelect = '' } } },
    addEventListener(name: string, callback: any) { if (!listeners.has(name)) listeners.set(name, new Set()); listeners.get(name)!.add(callback) },
    removeEventListener(name: string, callback: any) { listeners.get(name)?.delete(callback) },
  } as any
  globalThis.window = { innerHeight: 900, addEventListener() {}, removeEventListener() {} } as any
  const items = ['a', 'b', 'c'].map(id => ({ id, type: 'manual', parse_status: 'draft', file_name: id, created_at: '2026-10-12T00:00:00Z' }))
  const cardList = ref(items)
  const stub = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
  const opened: string[] = []
  const modules: Record<string, any> = { ...entryPresentationTestModules,
    '@/utils/bodyReference': bodyReference,
    '@/composables/useLibraryScope': { useLibraryScope: () => ref([]) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key, locale: ref('zh-CN') }) },
    'vue-router': { useRoute: () => ({ query: {} }), useRouter: () => ({ replace() {} }) },
    '@/composables/useMarqueeSelect': { useMarqueeSelect },
    '@/composables/useLibrary': { __esModule: true, default: () => ({
      cardList, listError: ref(''), total: ref(items.length), moreIndex: ref(-1), details: reactive({}),
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
    './entryStatusPolling': polling,
    './entryDownloadFileName': {},
    './documentListFilters': documentListFilters,
  }
  for (const file of ['./Library.vue', './components/DocumentListView.vue']) {
    const url = new URL(file, import.meta.url)
    for (const match of readFileSync(url, 'utf8').matchAll(/from\s+['"]([^'"]+\.vue)['"]/g)) modules[match[1]] = stub
  }
  modules['@/components/EntryUpdateNotice.vue'] = { __esModule: true, default: loadComponent(new URL('../../components/EntryUpdateNotice.vue', import.meta.url), modules) }
  modules['./components/DocumentListView.vue'] = { __esModule: true, default: loadComponent(new URL('./components/DocumentListView.vue', import.meta.url), modules) }
  const app = renderer.createApp(loadComponent(new URL('./Library.vue', import.meta.url), modules))
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zhCN } }))
  registerPrimitives(app)
  for (const name of ['t-date-range-picker', 't-dropdown', 't-skeleton']) app.component(name, stub.default)
  app.config.globalProperties.$t = (key: string) => key
  const errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  const root = element('root')
  app.mount(root)
  await settle()
  assert.deepEqual(errors, [])
  const cards = () => descendants(root).filter(node => node.props['data-select-id'])
  const selected = () => cards().filter(node => hasClass(node, 'rq-table__row--selected')).map(node => node.props['data-select-id'])
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
  return {
    root,
    async drag(index: number | null, start: [number, number], end: [number, number], expected: string[], modifiers = {}) {
      now += 200
      decorate()
      const card = index === null ? null : cards()[index]
      const target = card ? descendants(card).find(node => node.tag === 'time')! : container
      container.props.onMousedown(event(target, ...start, modifiers))
      dispatch('mousemove', event(target, ...end, modifiers))
      await settle()
      assert.deepEqual(selected(), expected, 'selection while dragging')
      dispatch('mouseup', event(target, ...end, modifiers))
      if (card) card.props.onClick?.(event(target, ...end, modifiers))
      await settle()
      assert.deepEqual(selected(), expected, 'selection after mouseup and the following card click')
    },
    cleanup() {
      app.unmount(); assert.equal([...listeners.values()].reduce((sum, set) => sum + set.size, 0), 0)
      globalThis.document = previous.document; globalThis.window = previous.window; globalThis.Element = previous.Element; Date.now = previous.now
      if (previous.storage) Object.defineProperty(globalThis, 'localStorage', previous.storage)
      else delete (globalThis as any).localStorage
    },
  }
}

test('the material table keeps appended and subtracted marquee selections after release', async () => {
  const env = await mountLibrary()
  try {
    await env.drag(null, [5, 5], [100, 110], ['a'])
    await env.drag(1, [150, 30], [230, 110], ['a', 'b'])
    await env.drag(0, [45, 100], [70, 80], ['b'])
  } finally { env.cleanup() }
})
