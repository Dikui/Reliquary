import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, ref, onMounted } from 'vue'
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

async function scenario(t: any) {
  const open = ref(false), query = ref(''), libraryScope = ref<string[]>([]), paths: any[] = []
  const recentQueries = ref(['Earlier query'])
  const store = { open, initialQuery: ref(''), recentQueries, recentTargets: ref([]),
    closePalette: () => { open.value = false }, openPalette: (q = '') => { store.initialQuery.value = q; open.value = true },
    pushRecent() {}, pushTarget() {}, clearRecent: () => { recentQueries.value = [] },
  }
  const location = { paragraph: 3, start: 5, end: 15, heading_path: [], body_hash: 'hash', mapping: 'exact' }
  const fileGroups = ref([{ entryId: 'entry-a', libraryId: 'a', libraryName: 'Research library', title: '<unsafe title>', contentType: 'research', chunks: [{ id: 'chunk', chunk_index: 0, content: 'Snippet', score: 0.9, match_type: 'vector', locations: [location] }] }])
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
    './GlobalCommandPalette/useSearch': { useCmdkSearch: () => ({ query, libraryScope, composing: ref(false), loading: ref(false), hasSearched: ref(true), fileGroups, entities: ref([{ id: 'entity-a', library_id: 'a', canonical_name: 'Orion', entity_type: 'company' }]), entityError: ref(''), searchError: ref(''), clearResults() {}, retry() {} }) },
    './GlobalCommandPalette/visibleFileChunks': chunks, './GlobalCommandPalette/commands': commands,
    './GlobalCommandPalette/useHighlight': highlight, '@/utils/markdownPlainText': plain, '@/utils/bodyReference': reference,
    './LibraryScopeSelect.vue': { __esModule: true, default: defineComponent({ emits: ['loaded'], setup(_props, { emit }) { onMounted(() => emit('loaded', [{ id: 'a', name: 'Research library', color: 1 }])); return () => null } }) },
    './SearchImage.vue': { __esModule: true, default: defineComponent({ props: ['path'], setup: props => () => h('span', { 'data-palette-image': props.path }) }) }, '@/components/EntryUpdateNotice.vue': empty,
    '@/views/settings/RetrievalSettings.vue': empty,
  }
  for (const name of ['ResultItem', 'ResultGroup']) modules[`./GlobalCommandPalette/${name}.vue`] = { __esModule: true, default: loadComponent(new URL(`./GlobalCommandPalette/${name}.vue`, import.meta.url), modules) }
  const component = loadComponent(new URL('./GlobalCommandPalette.vue', import.meta.url), modules)
  const mounted = mount({ setup: () => () => h('div', [h('button', { id: 'palette-opener', onClick: () => store.openPalette() }, 'Search'), h(component)]) })
  t.after(() => mounted.unmount())
  const opener = doc.getElementById('palette-opener')!
  opener.focus(); await send(opener, 'click'); await settle()
  return { open, query, paths, location, opener, store, libraryScope, fileGroups }
}

test('palette restores its opener and preserves recent-query and digit keyboard actions', async t => {
  const { open, query, paths, opener } = await scenario(t)
  const input = doc.querySelector('input')!
  assert.equal(doc.activeElement, input)
  assert.ok(doc.body.textContent.includes('Earlier query'))
  await send(input, 'keydown', { key: '1', metaKey: true })
  assert.equal(query.value, 'Earlier query')
  query.value = ''; await settle()
  await send(input, 'keydown', { key: '2', metaKey: true })
  assert.equal(paths[0], '/platform/entries')
  assert.equal(open.value, false)
  await settle()
  assert.equal(doc.activeElement, opener)
})

test('summary-only candidates stay in materials and open without claiming a body reference', async t => {
  const { query, paths, fileGroups } = await scenario(t)
  fileGroups.value[0]!.chunks = [{ id: '', chunk_index: -1, content: 'Model summary match', score: 0.9, match_type: 'keyword', locations: [] } as any]
  Object.assign(fileGroups.value[0]!, { imagePath: 'match.png' })
  query.value = 'Orion'; await settle()
  const groups = doc.querySelectorAll('[role="group"]')
  assert.ok(groups.some((node: any) => node.props['aria-label'] === 'Materials'))
  assert.ok(!groups.some((node: any) => node.props['aria-label'] === 'Body snippets'))
  assert.ok(doc.querySelectorAll('span').some((node: any) => node.props.innerHTML === 'Model summary match'))
  assert.equal(doc.querySelector('[data-palette-image]')?.getAttribute('data-palette-image'), 'match.png')
  await send(doc.querySelector('[data-cmdk-index="0"]')!, 'click')
  assert.deepEqual(paths[0], { path: '/platform/entries/entry-a', query: {} })
})

test('scope chips and full-search navigation retain the query and chosen library', async t => {
  const { query, paths, libraryScope } = await scenario(t)
  const current = doc.querySelectorAll('button').find((node: any) => node.textContent.includes('Current library: Research library'))!
  await send(current, 'click')
  assert.deepEqual(libraryScope.value, ['a'])
  query.value = '  Orion  '; await settle()
  const link = doc.querySelectorAll('a').find((node: any) => node.textContent === 'View all in search')!
  assert.equal(link.getAttribute('href'), '/platform/search?q=Orion&libraries=a')
  await send(link, 'click')
  assert.deepEqual(paths[0], { path: '/platform/search', query: { q: 'Orion', libraries: 'a' } })
})

test('row keyboard navigation moves actual focus and Enter activates only the newly selected row', async t => {
  const { query, paths } = await scenario(t)
  query.value = 'Orion'; await settle()
  const rows = doc.querySelectorAll('[data-cmdk-index]')
  rows[0]!.focus()
  await send(rows[0]!, 'keydown', { key: 'ArrowDown' })
  assert.ok(doc.activeElement === rows[1], 'ArrowDown moves focus from a tabbable row')
  await send(rows[1]!, 'keydown', { key: 'Enter' })
  assert.equal(paths.length, 1)
  assert.ok(paths[0].query.reference)
})

test('materials, body snippets and entities use their owning routes, and selected rows expose one focus', async t => {
  const { query, paths, location, store, opener } = await scenario(t)
  query.value = 'Orion'; await settle()
  const rows = () => doc.querySelectorAll('[data-cmdk-index]')
  assert.ok(doc.body.textContent.includes('Materials'))
  assert.ok(doc.body.textContent.includes('Body snippets'))
  assert.ok(doc.body.textContent.includes('Entities'))
  assert.ok(doc.body.textContent.includes('Research'))
  assert.equal(rows().filter((node: any) => node.props['aria-selected']).length, 1)
  await send(rows()[1]!, 'mousemove')
  await send(doc.querySelector('input')!, 'keydown', { key: 'Enter' })
  assert.deepEqual(paths[0], reference.entryDetailLocation('entry-a', location))
  store.openPalette('Orion'); await settle()
  await send(rows()[2]!, 'click')
  assert.deepEqual(paths[1], { path: '/platform/entities', query: { entity_id: 'entity-a', libraries: 'a' } })
  store.openPalette('Orion'); await settle()
  await send(rows()[0]!, 'click')
  assert.deepEqual(paths[2], reference.entryDetailLocation('entry-a'))
  store.openPalette(); await settle()
  await send(doc.querySelector('input')!, 'keydown', { key: 'Escape' })
  assert.equal(doc.activeElement, opener)
})
