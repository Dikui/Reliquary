import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, ref } from 'vue'
import { doc, mount, send, settle } from './ui/__tests__/dom'
import { loadUiComponent } from './ui/uiTestLoader'
import { loadComponent } from '../views/library/componentTestHarness'
import * as commands from './GlobalCommandPalette/commands'
import * as chunks from './GlobalCommandPalette/visibleFileChunks'
import * as highlight from './GlobalCommandPalette/useHighlight'
import * as plain from '../utils/markdownPlainText'
import * as reference from '../utils/bodyReference'
import { CONTENT_TYPES } from '../types/entryClassification'

Object.defineProperty(globalThis, 'Document', { configurable: true, value: class { static [Symbol.hasInstance](value: unknown) { return value === doc } } })

async function scenario(t: any, timeline: boolean, locale: string) {
  const open = ref(false), query = ref(''), paths: any[] = []
  const recentQueries = ref(['Earlier query'])
  const store = { open, initialQuery: ref(''), recentQueries, recentTargets: ref([]),
    closePalette: () => { open.value = false }, openPalette: (q = '') => { store.initialQuery.value = q; open.value = true },
    pushRecent() {}, pushTarget() {}, clearRecent: () => { recentQueries.value = [] },
  }
  const location = { paragraph: 3, start: 5, end: 15, heading_path: [], body_hash: 'hash', mapping: 'exact' }
  const fileGroups = ref([{ entryId: 'entry-a', libraryId: 'a', libraryName: 'Research library', title: '<unsafe title>', imagePath: 'chart.png', contentType: 'research', chunks: [{ id: 'chunk', chunk_index: 0, content: 'Snippet', score: 0.9, match_type: 'vector', locations: [location] }] }])
  const ui = Object.fromEntries(['Dialog', 'Drawer', 'Button', 'Tag', 'Chip'].map(name => [name, loadUiComponent(name)]))
  ui.LibMark = defineComponent({ setup: () => () => h('i') })
  const empty = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
  const modules: Record<string, any> = {
    pinia: { storeToRefs: (value: any) => value },
    '@/stores/commandPalette': { useCommandPaletteStore: () => store },
    '@/stores/auth': { useAuthStore: () => ({}) },
    'vue-router': { useRoute: () => ({ query: { libraries: 'a' } }), useRouter: () => ({ push: (target: any) => paths.push(target), resolve: (target: any) => ({ href: target.path }) }) },
    '@/components/ui': ui, '@/types/entryClassification': { CONTENT_TYPES },
    '@/composables/useTheme': { useTheme: () => ({ currentTheme: ref('light'), setTheme() {} }) },
    './GlobalCommandPalette/useSearch': { useCmdkSearch: () => ({ query, libraryScope: ref([]), composing: ref(false), loading: ref(false), hasSearched: ref(true), fileGroups, entities: ref([{ id: 'entity-a', library_id: 'a', canonical_name: 'Orion', entity_type: 'company' }]), entityError: ref(''), searchError: ref(''), clearResults() {}, retry() {} }) },
    './GlobalCommandPalette/visibleFileChunks': chunks, './GlobalCommandPalette/commands': commands,
    './GlobalCommandPalette/useHighlight': highlight, '@/utils/markdownPlainText': plain, '@/utils/bodyReference': reference,
    './LibraryScopeSelect.vue': empty, '@/components/EntryUpdateNotice.vue': empty,
    './SearchImage.vue': { __esModule: true, default: defineComponent({ setup() {
      const previewOpen = ref(false)
      return () => h('div', [h('button', { 'data-preview-trigger': true, onClick: () => { previewOpen.value = true } }, 'Image'),
        h(ui.Dialog, { modelValue: previewOpen.value, layer: 2500, title: 'Image', 'onUpdate:modelValue': (value: boolean) => { previewOpen.value = value } }, () => h('button', { 'data-preview-close': true }, 'Image content'))])
    } }) },
    '@/views/settings/RetrievalSettings.vue': { __esModule: true, default: defineComponent({ setup: () => () => h('input', { 'data-retrieval-input': true }) }) },
  }
  for (const name of ['ResultItem', 'ResultGroup']) modules[`./GlobalCommandPalette/${name}.vue`] = { __esModule: true, default: loadComponent(new URL(`./GlobalCommandPalette/${name}.vue`, import.meta.url), modules) }
  const component = loadComponent(new URL('./GlobalCommandPalette.vue', import.meta.url), modules)
  const lowerOpen = ref(true), timelineOpen = ref(timeline)
  const mounted = mount({ setup: () => () => h('div', [
    h('div', { role: 'dialog', style: { zIndex: 2001 } }, 'Legacy material drawer'),
    timeline ? h('div', { role: 'dialog', style: { zIndex: 2101 } }, 'Legacy timeline drawer') : null,
    h('button', { id: 'palette-opener', onClick: () => store.openPalette() }, 'Search'), h(component)]) })
  mounted.app.config.globalProperties.$i18n.locale = locale
  t.after(() => mounted.unmount())
  const opener = doc.getElementById('palette-opener')!
  opener.focus(); await send(opener, 'click'); await settle()
  return { open, query, paths, location, opener, store, lowerOpen, timelineOpen }
}

for (const locale of ['en-US', 'zh-CN']) for (const timeline of [false, true]) test(`${locale}: real Reka palette, retrieval and image layers close one at a time above ${timeline ? 'timeline' : 'material'}`, async t => {
  let legacyEscapes = 0
  const legacyListener = (event: KeyboardEvent) => { if (event.key === 'Escape') legacyEscapes++ }
  document.addEventListener('keydown', legacyListener)
  t.after(() => document.removeEventListener('keydown', legacyListener))
  const { open, query, lowerOpen, timelineOpen } = await scenario(t, timeline, locale)
  query.value = 'Orion'; await settle()
  const palette = doc.querySelectorAll('[role="dialog"]').find((node: any) => String(node.props.class || '').includes('rq-cmdk-dialog'))!
  assert.equal(palette.parentNode, doc.body)
  assert.equal(palette.style.zIndex, 2401)
  const settings = doc.querySelectorAll('button').find((node: any) => node.textContent === (locale === 'zh-CN' ? '检索参数' : 'Retrieval settings'))!
  await send(settings, 'click')
  const input = doc.querySelector('[data-retrieval-input]')!
  assert.equal(input.closest('[role="dialog"]').style.zIndex, 2451)
  input.focus()
  assert.ok(doc.activeElement === input, 'Settings fields accept focus')
  await send(input, 'keydown', { key: 'Escape' })
  assert.equal(doc.querySelector('[data-retrieval-input]'), null)
  assert.equal(open.value, true)
  await send(doc.querySelector('[data-preview-trigger]')!, 'click')
  const preview = doc.querySelector('[data-preview-close]')!
  assert.equal(preview.closest('[role="dialog"]').style.zIndex, 2501)
  await send(preview, 'keydown', { key: 'Escape' })
  assert.equal(doc.querySelector('[data-preview-close]'), null)
  assert.equal(open.value, true)
  await send(doc.querySelector('[data-preview-trigger]')!, 'keydown', { key: 'Escape' })
  assert.equal(open.value, false)
  assert.equal(lowerOpen.value, true)
  assert.equal(timelineOpen.value, timeline)
  assert.equal(legacyEscapes, 0, 'Legacy document-level drawers must not receive handled Escape events')
})
