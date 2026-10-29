import { loadUiComponent } from '../../components/ui/uiTestLoader'
import * as trace from '../../utils/entryTrace'
import * as externalUrl from '../../utils/externalUrl'
import * as processing from '../../utils/entryProcessingStatus'
import * as bodyReference from '../../utils/bodyReference'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent, h, onMounted, ref } from 'vue'
import ts from 'typescript'
import { CONTENT_TYPES } from '../../types/entryClassification'
import { SEARCH_SOURCES } from '../../types/search'
import * as entrySourceLabels from '../../utils/entrySourceLabels'
import * as markdownPlainText from '../../utils/markdownPlainText'
import zhCN from '../../i18n/locales/zh-CN'
import enUS from '../../i18n/locales/en-US'
import * as polling from './entryStatusPolling'
import * as documentListFilters from './documentListFilters'
import { descendants, element, hasClass, loadComponent, registerPrimitives, renderer, settle, visibleText } from './componentTestHarness'

const require = createRequire(import.meta.url)
const { createPinia, setActivePinia } = require('pinia') as typeof import('pinia')
const { createI18n } = require('vue-i18n') as typeof import('vue-i18n')
const vueRouter = require('vue-router') as typeof import('vue-router')
function evaluateFile(path: string, modules: Record<string, unknown>) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const code = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', code)(
    (name: string) => modules[name] ?? require(name), module, module.exports,
  )
  return module.exports
}
const moduleOf = <T>(value: T) => ({ __esModule: true, default: value })

async function mountLibrary(locale = 'zh-CN', initialQuery: Record<string, any> = {}, view = 'list', unavailable = false, customType = false, noLibraries = false, realPreview = false) {
  const previous = { window: globalThis.window, document: globalThis.document,
    storage: Object.getOwnPropertyDescriptor(globalThis, 'localStorage') }
  globalThis.window = { innerHeight: 900, setTimeout, clearTimeout, addEventListener() {}, removeEventListener() {} } as any
  globalThis.document = { addEventListener() {}, removeEventListener() {} } as any
  const storage = new Map([['reliquary.library.docs.viewMode', view]])
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value),
  } })
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const t = i18n.global.t
  const requests: URLSearchParams[] = []
  const scope = ref<string[]>([])
  const rebuildRequests: Array<{ path: string; data: unknown }> = [], notices: string[] = []
  const previewResult = { success: true, data: { fingerprint: 'preview-1', matched: 41, eligible: 40, skipped: 1, blocked: 0, reasons: [{ code: 'generated_library', count: 1 }], samples: [{ id: 'sample', title: 'Matching sample', library_id: 'library-source' }] } }
  const batchResult = { success: true, data: { batch_id: 'batch-1', action: 'reprocess', status: 'completed', total: 41, succeeded: 40, failed: 0, skipped: 1, pending: 0, running: 0, warnings: 0, items: [], page: 1, page_size: 20 } }
  let rebuildResponse: (path: string, data: unknown) => Promise<any> = async path => path.endsWith('/preview') ? previewResult : batchResult
  let confirmation: any
  const records = [
    { id: 'article', source_kind: 'article', title: 'Needle article', content_type: 'tutorial', starred: true },
    { id: 'mine', source_kind: 'mine', title: 'Needle document', content_type: 'tutorial', starred: true },
    { id: 'report', source_kind: 'report', title: 'Needle report', content_type: '', starred: false },
    ...Array.from({ length: 38 }, (_, index) => ({ id: `mine-${index}`, source_kind: 'mine', title: `Document ${index}`, content_type: 'event', starred: false })),
  ].map(record => ({ ...record, file_name: `${record.title}.md`, type: 'manual', file_type: 'md', channel: 'api',
    parse_status: 'completed', incomplete: record.id === 'mine', created_at: '2026-10-13T08:00:00Z', updated_at: '2026-10-13T08:00:00Z' }))
  if (customType) records.push({ ...records[0], id: 'sop', content_type: 'SOP' })
  const target = records.find(record => record.id === 'mine')!
  for (const [field, value] of Object.entries({ channel: 'web', file_type: 'pdf', parse_status: 'failed',
    updated_at: '2026-09-01T08:00:00Z', title: 'Unrelated document', starred: false, content_type: 'opinion' })) {
    records.push({ ...target, id: `mine-other-${field}`, [field]: value })
  }
  const deleted = new Set<string>(), deleteCalls: string[] = []
  let deleteFails = true, listUnavailable = false
  const transport = {
    async del(path: string) { deleteCalls.push(path); const id = path.split('/').at(-1)!; if (deleteFails) return { success: false, message: 'Delete failed' }; deleted.add(id); return { success: true } },
    async post(path: string, data: unknown) { rebuildRequests.push({ path, data }); return rebuildResponse(path, data) },
    async get(path: string) {
      const url = new URL(path, 'http://localhost')
      if (url.pathname.endsWith('/spans')) return { success: true, data: {} }
      if (url.pathname.startsWith('/api/v1/entries/')) return { success: true, data: records.find(record => record.id === url.pathname.split('/').at(-1)) }
      assert.equal(url.pathname, '/api/v1/entries')
      if (listUnavailable) throw new Error('List unavailable')
      const query = new URLSearchParams(url.searchParams)
      requests.push(query)
      const filtered = records.filter(record => {
        if (deleted.has(record.id)) return false
        if (query.has('source_kind') && record.source_kind !== query.get('source_kind')) return false
        if (query.has('keyword') && !record.title.toLowerCase().includes(query.get('keyword')!.toLowerCase())) return false
        if (query.has('content_type') && record.content_type !== query.get('content_type')) return false
        if (query.has('file_type') && record.file_type !== query.get('file_type')) return false
        if (query.has('starred') && String(record.starred) !== query.get('starred')) return false
        if (query.has('incomplete') && String(record.incomplete) !== query.get('incomplete')) return false
        if (query.has('source') && record.channel !== query.get('source')) return false
        if (query.has('parse_status') && record.parse_status !== query.get('parse_status')) return false
        if (query.has('start_time') && record.updated_at < query.get('start_time')!.replace(' ', 'T') + 'Z') return false
        if (query.has('end_time') && record.updated_at > query.get('end_time')!.replace(' ', 'T') + 'Z') return false
        return true
      })
      const offset = (Number(query.get('page')) - 1) * Number(query.get('page_size'))
      return { success: true, data: filtered.slice(offset, offset + Number(query.get('page_size'))), total: filtered.length }
    },
  }
  const api = evaluateFile('../../api/library.ts', { '../utils/request': transport })
  const store = evaluateFile('../../stores/entry.ts', {})
  const hook = evaluateFile('../../composables/useLibrary.ts', {
    '@/api/entities': { listEntities: async () => ({ success: true, data: [{ id: 'entity-1', canonical_name: 'Target Entity' }] }) },
    '@/api/topics': { listTopics: async () => ({ success: true, data: [{ id: 'topic-1', name: 'Target Topic' }] }) },
    '@/api/library': api, '@/stores/entry': store,
    '../utils/index': { formatStringDate: () => '2026-10-13' },
    '@/components/ui/feedback': { toast: { info() {}, error() {} } },
    '@/stores/resources': { useResourcesStore: () => ({}) },
  })
  const empty = moduleOf(defineComponent({ setup: () => () => null }))
  let backendAvailable = !unavailable
  const modules: Record<string, any> = {
    '@/api/entities': { listEntities: async () => ({ success: true, data: [{ id: 'entity-1', canonical_name: 'Target Entity' }] }) },
    '@/api/topics': { listTopics: async () => ({ success: true, data: [{ id: 'topic-1', name: 'Target Topic' }] }) },
    '@/utils/bodyReference': bodyReference,
    '@/composables/useLibraryScope': { useLibraryScope: () => scope },
    'vue-router': vueRouter,
    '@/composables/useLibrary': hook,
    '@/stores/ui': { useUIStore: () => ({}) },
    '@/stores/pendingCounts': { usePendingCountsStore: () => ({ total: null, refresh: async () => {} }) },
    '@/stores/commandPalette': { useCommandPaletteStore: () => ({}) },
    '@/stores/resources': { useResourcesStore: () => ({ library: unavailable || noLibraries ? null : { id: 'library-source' }, fetchLibrary: async () => { if (!backendAvailable) throw new Error('Backend unavailable'); return noLibraries ? null : { id: 'library-source' } } }) },
    '@/stores/editorResources': { useEditorResourcesStore: () => ({ parserEngines: [], ensureParserEngines: async () => {} }) },
    '@/stores/reparseConfirm': { useReparseConfirmStore: () => ({}) },
    '@/composables/useMarqueeSelect': { useMarqueeSelect: () => ({ shouldSuppressClick: () => false }) },
    '@/types/entryClassification': { CONTENT_TYPES },
    '@/types/search': { SEARCH_SOURCES },
    '@/api/library': api,
    '@/api/entry-batches': evaluateFile('../../api/entry-batches.ts', { '@/utils/request': transport }),
    '@/utils/entryTrace': {},
    '@/utils/entrySourceLabels': entrySourceLabels,
    '@/utils/markdownPlainText': markdownPlainText,
    '@/utils/files': { formatFileSize: () => '', getFileIcon: () => '' },
    '@/assets/img/more-actions.png': moduleOf(''),
    './entryStatusPolling': polling,
    './entryDownloadFileName': {},
    './documentListFilters': documentListFilters,
  }
  modules['@/components/ui'] = Object.fromEntries(['Button', 'Input', 'Select', 'Chip', 'LibMark', 'Skeleton', 'EmptyState', 'ErrorState', 'DataTable', 'Tag', 'StatusSquare', 'Checkbox', 'Dialog'].map(name => [name, loadUiComponent(name)]))
  modules['@/components/ui'].confirm = (options: unknown) => { confirmation = options; return { destroy() {}, result: new Promise<boolean>(() => {}) } }
  modules['@/components/ui'].toast = { info() {}, error() {}, success() {} }
  modules['@/utils/entryProcessingStatus'] = processing
  for (const path of ['./Library.vue', './components/DocumentListView.vue']) {
    for (const match of readFileSync(new URL(path, import.meta.url), 'utf8').matchAll(/from\s+['"]([^'"]+\.vue)['"]/g)) modules[match[1]] = empty
  }

  modules['@/components/EntryBatchDialog.vue'] = moduleOf(loadComponent(new URL('../../components/EntryBatchDialog.vue', import.meta.url), modules))
  modules['@/components/LibraryScopeSelect.vue'] = moduleOf(defineComponent({
    props: { single: Boolean }, emits: ['loaded', 'update:modelValue'], setup: (props, { emit }) => {
      onMounted(() => { if (props.single && !noLibraries) emit('update:modelValue', ['library-source']); emit('loaded', noLibraries ? [] : [
        { id: 'library-source', color: 1, name: 'Research', display_name: 'Research', template: 'own', content_types: [...CONTENT_TYPES, 'SOP'].map(name => ({ name, enabled: true })) },
        { id: 'reports', color: 2, name: 'AI Reports', template: 'generated', content_types: [] },
      ]) })
      return () => null
    },
  }))
  modules['@/components/EntryUpdateNotice.vue'] = moduleOf(loadComponent(new URL('../../components/EntryUpdateNotice.vue', import.meta.url), modules))
  for (const view of ['List']) modules[`./components/Document${view}View.vue`] = moduleOf(
    loadComponent(new URL(`./components/Document${view}View.vue`, import.meta.url), modules),
  )
  if (unavailable) {
    modules['@/api/entities'] = { ENTITY_TYPES: ['company'], listEntities: async () => { if (!backendAvailable) throw new Error('Backend unavailable'); return {success:true,data:[{id:'recovered',canonical_name:'Recovered entity',entity_type:'company',aliases:[]}] } }, getEntityNameEmbeddingRebuildStatus: async () => { if (!backendAvailable) throw new Error('Backend unavailable'); return {success:true,data:{pending_count:0}} } }
    modules['@/api/library'] = { ...api, listLibraries: async () => {
      if (!backendAvailable) throw new Error('Backend unavailable')
      return { success: true, data: [{ id: 'library-source', name: 'Research', color: 1 }] }
    } }
    modules['@/api/entities'].listEntityMergeSuggestions = async () => ({ success: true, data: [] })
    modules['@/api/relations'] = { listEntityRelations: async () => ({ success: true, data: [] }) }
    modules['@/stores/follows'] = {useFollowStore:()=>({refreshAfterTargetChange:async()=>{}})}
    modules['./EntityGraph.vue']=empty;modules['./MergeSuggestions.vue']=empty;modules['./EntityRelations.vue']=empty;modules['@/components/FollowToggle.vue']=empty
    modules['./components/EntryWorkbench.vue']=moduleOf(loadComponent(new URL('./components/EntryWorkbench.vue',import.meta.url),modules))
  }
  modules['@/components/ui'].Dialog = defineComponent({ props: ['modelValue'], setup: (props, { slots }) => () => props.modelValue ? h('section', slots.default?.()) : null })
  if (realPreview) modules['./components/EntryPreview.vue'] = moduleOf(loadComponent(new URL('./components/EntryPreview.vue', import.meta.url), { ...modules, '@/utils/entryTrace': trace, '@/utils/externalUrl': externalUrl }))
  const library = loadComponent(new URL('./Library.vue', import.meta.url), modules)
  const router = vueRouter.createRouter({ history: vueRouter.createMemoryHistory(), routes: [
    { path: '/platform/library', component: library },
    { path: '/platform/search', component: empty.default },
    { path: '/platform/libraries', component: empty.default },
  ] })
  await router.push({ path: '/platform/library', query: { preserved: 'value', ...initialQuery } })
  const app = renderer.createApp(defineComponent({ setup: () => () => h(vueRouter.RouterView) }))
  app.use(router)
  const pinia = createPinia()
  setActivePinia(pinia)
  app.use(pinia)
  app.use(i18n)
  registerPrimitives(app)
  delete app._context.components['t-select']
  delete app._context.components['t-input']
  app.component('t-select', defineComponent({
    props: ['modelValue', 'options'], emits: ['update:modelValue'],
    setup: (props, { attrs, emit }) => () => h('select', {
      ...attrs, value: props.modelValue, 'data-options': props.options,
      onChange: (event: any) => emit('update:modelValue', event.value),
    }),
  }))
  app.component('t-input', defineComponent({
    props: ['modelValue'], emits: ['update:modelValue'],
    setup: (props, { attrs, emit }) => () => h('input', { ...attrs, value: props.modelValue,
      onChange: (event: any) => emit('update:modelValue', event.value) }),
  }))
  app.component('t-date-range-picker', defineComponent({
    props: ['modelValue'], emits: ['update:modelValue'],
    setup: (props, { attrs, emit }) => () => h('input', { ...attrs, value: props.modelValue,
      onChange: (event: any) => emit('update:modelValue', event.value) }),
  }))
  app.component('t-checkbox', defineComponent({
    props: ['checked', 'modelValue', 'disabled'], emits: ['change', 'update:modelValue'],
    setup: (props, { attrs, emit }) => () => h('input', { ...attrs, type: 'checkbox', checked: props.checked, disabled: props.disabled,
      onChange: (event: any) => { emit('change', event.checked, { e: event }); emit('update:modelValue', event.checked) } }),
  }))
  delete app._context.components['t-alert']
  app.component('t-alert', defineComponent({ props: ['message'], setup: (props, { attrs, slots }) => () => h('div', attrs, [props.message, slots.operation?.()]) }))
  for (const name of ['t-dropdown', 't-skeleton', 't-pagination', 't-empty']) app.component(name, empty.default)
  const errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  const root = element('root')
  app.mount(root)
  const waitForLoad = async () => {
    await new Promise(resolve => setTimeout(resolve, 0))
    await settle()
    assert.deepEqual(errors, [])
  }
  await waitForLoad()
  const control = (label: string) => descendants(root).find(node => node.props['aria-label'] === t(label))!
  const byOptions = (value: string) => descendants(root).find(node =>
    node.tag === 'select' && descendants(node).some(option => option.tag === 'option' && option.props.value === value))!
  const content = async (value: string) => { const chip = descendants(root).find(node => node.tag === 'button' && visibleText(node) === (value || t('entries.all')))!; for (const handler of Array.isArray(chip.props.onClick) ? chip.props.onClick : [chip.props.onClick]) handler(); await waitForLoad() }
  const range = (kind: 'updated' | 'published') => ({ ...element('date-range'), props: { value: [control(`entryBatch.${kind}From`).props.value || '', control(`entryBatch.${kind}To`).props.value || ''], onChange: async (event: any) => { for (const [index, side] of ['From', 'To'].entries()) control(`entryBatch.${kind}${side}`).props.onChange({ target: { value: event.value[index] || '' } }); } } })
  const ids = () => descendants(root).filter(node => node.props['data-select-id']).map(node => node.props['data-select-id'])
  return {
    root, router, requests, records, errors, control, byOptions, ids, t, scope, rebuildRequests, notices, previewResult, batchResult, content, range, deleteCalls, setListUnavailable(value: boolean) { listUnavailable = value }, allowDelete() { deleteFails = false },
    get confirmation() { return confirmation },
    setRebuildResponse(response: (path: string, data: unknown) => Promise<any>) { rebuildResponse = response },
    recover() { backendAvailable = true },
    get route() { return router.currentRoute.value },
    async change(node: ReturnType<typeof element>, value: unknown) { if (node.tag === 'select') node.props.onChange({ target: { value, selectedOptions: (Array.isArray(value) ? value : [value]).map(value => ({ value })) } }); else if (node.tag === 'input') node.props.onInput({ target: { value, selectedOptions: (Array.isArray(value) ? value : [value]).map(value => ({ value })) } }); else node.props.onChange({ value }); await waitForLoad() },
    async navigate(query: Record<string, any>) { await router.push({ path: '/platform/library', query }); await waitForLoad() },
    async leave() { await router.push('/platform/search'); await waitForLoad() },
    async back() { router.back(); await waitForLoad() },
    async forward() { router.forward(); await waitForLoad() },
    async list() {
      descendants(root).find(node => node.tag === 'button' && visibleText(node) === t('entries.table'))!.props.onClick()
      await settle()
      assert.ok(descendants(root).some(node => hasClass(node, 'rq-table__row')))
    },
    cleanup() {
      app.unmount()
      globalThis.window = previous.window
      globalThis.document = previous.document
      if (previous.storage) Object.defineProperty(globalThis, 'localStorage', previous.storage)
      else delete (globalThis as any).localStorage
    },
  }
}

test('a space with no libraries finishes loading and displays the material empty state', async () => {
  const env = await mountLibrary('zh-CN', {}, 'grid', false, false, true)
  try {
    assert.equal(env.requests.length, 0)
    assert.equal(descendants(env.root).some(node => hasClass(node, 'entry-card-skeleton')), false)
    assert.ok(descendants(env.root).some(node => hasClass(node, 'rq-empty')))
    assert.ok(visibleText(env.root).includes(env.t('library.emptyArticles')))
  } finally { env.cleanup() }
})

test('an initial library request failure stops the material loading placeholders', async () => {
  const env = await mountLibrary('zh-CN', {}, 'grid', true)
  try {
    assert.equal(env.requests.length, 0)
    assert.equal(descendants(env.root).some(node => hasClass(node, 'entry-card-skeleton')), false)
  } finally { env.cleanup() }
})

for (const locale of ['zh-CN', 'en-US']) {
  test(`${locale}: source filter loads cards and rows through the real hook and API with pagination`, async () => {
    const env = await mountLibrary(locale)
    try {
      const source = env.control('library.materialCategory')
      assert.deepEqual(descendants(source).filter(node => node.tag === 'option').map(node => ({ value: node.props.value, label: visibleText(node) })), [
        { value: '', label: env.t('library.allMaterialCategories') },
        ...SEARCH_SOURCES.map(value => ({ value, label: env.t(`knowledgeSearch.sources.${value}`) })),
      ])
      assert.equal(env.requests[0].has('source_kind'), false)
      assert.equal(env.ids().length, 35)
      await env.change(source, 'mine')
      assert.ok(env.ids().every(id => id.startsWith('mine')))
      assert.equal(env.requests.at(-1)!.get('source_kind'), 'mine')
      assert.equal(env.route.query.source_kind, 'mine')
      assert.equal(env.route.query.preserved, 'value')
      const requestCount = env.requests.length
      await env.list()
      assert.ok(env.ids().every(id => id.startsWith('mine')))
      assert.equal(env.requests.length, requestCount, 'changing view retains the already loaded data')
      const scroll = descendants(env.root).find(node => hasClass(node, 'doc-scroll-container'))!
      Object.assign(scroll, { scrollTop: 450, scrollHeight: 500, clientHeight: 100 })
      scroll.props.onScroll()
      await settle()
      assert.equal(env.requests.at(-1)!.get('page'), '2')
      assert.equal(env.requests.at(-1)!.get('source_kind'), 'mine')
      assert.equal(env.ids().length, env.records.filter(record => record.source_kind === 'mine').length)
      await env.change(source, 'article')
      assert.deepEqual(env.ids(), ['article'])
      assert.equal(env.requests.at(-1)!.get('page'), '1')
      await env.change(source, 'report')
      assert.deepEqual(env.ids(), ['report'])
      await env.change(source, '')
      assert.equal(env.requests.at(-1)!.has('source_kind'), false)
      assert.equal(env.route.query.source_kind, undefined)
      assert.equal(env.route.query.preserved, 'value')
      assert.equal(env.ids().length, 35)
    } finally { env.cleanup() }
  })
}

test('source filters combine with existing filters and retain URL state on reload and history changes', async () => {
  const env = await mountLibrary('zh-CN', { source_kind: 'mine', tab: 'documents' })
  try {
    assert.ok(env.ids().every(id => id.startsWith('mine')))
    assert.equal(env.requests[0].get('source_kind'), 'mine')
    await env.content('tutorial')
    await env.change(env.control('articleJudgments.starLabel'), 'true')
    await env.change(env.byOptions('md'), 'md')
    await env.change(env.byOptions('completed'), 'completed')
    await env.change(env.control('library.channelLabel'), 'api')
    const date = env.range('updated')
    await env.change(date, ['2026-10-01', '2026-10-13'])
    const keyword = descendants(env.root).find(node => hasClass(node, 'doc-search-input'))!
    await env.change(keyword, 'Needle')
    await new Promise(resolve => setTimeout(resolve, 330))
    await settle()
    assert.deepEqual(env.ids(), ['mine'])
    const query = env.requests.at(-1)!
    for (const [key, value] of Object.entries({ source_kind: 'mine', content_type: 'tutorial', starred: 'true',
      file_type: 'md', parse_status: 'completed', source: 'api', keyword: 'Needle',
      start_time: '2026-10-01 00:00:00', end_time: '2026-10-13 23:59:59' })) assert.equal(query.get(key), value)
    await env.navigate({ ...env.route.query, source_kind: 'article' })
    assert.deepEqual(env.ids(), ['article'])
    assert.equal(env.control('library.materialCategory').props.value, 'article')
    assert.equal(env.requests.at(-1)!.get('source_kind'), 'article')
    await env.navigate({ ...env.route.query, source_kind: 'invalid' })
    assert.equal(env.control('library.materialCategory').props.value, '')
    assert.equal(env.requests.at(-1)!.has('source_kind'), false)
    assert.equal(env.route.query.source_kind, undefined)
    assert.deepEqual(env.errors, [])
  } finally { env.cleanup() }
})

const combinedQuery = {
  source_kind: 'mine', content_type: 'tutorial', starred: 'true', file_type: 'md',
  parse_status: 'completed', source: 'api', keyword: 'Needle',
  start_date: '2026-10-01', end_date: '2026-10-13',
}
const combinedAPIQuery = {
  source_kind: 'mine', content_type: 'tutorial', starred: 'true', file_type: 'md',
  parse_status: 'completed', source: 'api', keyword: 'Needle',
  start_time: '2026-10-01 00:00:00', end_time: '2026-10-13 23:59:59',
}
function assertFilters(env: Awaited<ReturnType<typeof mountLibrary>>, filters: Partial<typeof combinedQuery>) {
  for (const [key, node] of Object.entries({
    source_kind: env.control('library.materialCategory'),
    starred: env.control('articleJudgments.starLabel'), file_type: env.byOptions('md'),
    parse_status: env.byOptions('completed'), source: env.control('library.channelLabel'),
    keyword: descendants(env.root).find(node => hasClass(node, 'doc-search-input'))!,
  })) assert.equal(node.props.value, filters[key as keyof typeof combinedQuery] || '', `restored ${key} control`)
  const date = env.range('updated')
  assert.deepEqual(date.props.value, [filters.start_date || '', filters.end_date || ''])
}
function assertCombinedLoad(env: Awaited<ReturnType<typeof mountLibrary>>, view: string) {
  assertFilters(env, combinedQuery)
  assert.deepEqual(env.ids(), ['mine'])
  const request = env.requests.at(-1)!
  for (const [key, value] of Object.entries(combinedAPIQuery)) assert.equal(request.get(key), value, `restored API ${key}`)
  assert.equal(request.get('page'), '1')
  assert.equal(env.route.query.preserved, 'value')
  assert.equal(env.route.query.tab, 'documents')
  assert.ok(descendants(env.root).some(node => hasClass(node, view === 'table' ? 'rq-table__row' : 'entries-list__row')))
}

for (const locale of ['zh-CN', 'en-US']) for (const view of ['list', 'table']) {
  test(`${locale} ${view}: every filter persists and restores through reload, route return, history back and forward`, async () => {
    let env = await mountLibrary(locale, { tab: 'documents' }, view)
    try {
      await env.change(env.control('library.materialCategory'), combinedQuery.source_kind)
      await env.content(combinedQuery.content_type)
      await env.change(env.control('articleJudgments.starLabel'), combinedQuery.starred)
      await env.change(env.byOptions('md'), combinedQuery.file_type)
      await env.change(env.byOptions('completed'), combinedQuery.parse_status)
      await env.change(env.control('library.channelLabel'), combinedQuery.source)
      await env.change(env.range('updated'), [combinedQuery.start_date, combinedQuery.end_date])
      await env.change(descendants(env.root).find(node => hasClass(node, 'doc-search-input'))!, combinedQuery.keyword)
      await new Promise(resolve => setTimeout(resolve, 330))
      await settle()
      for (const [key, value] of Object.entries(combinedQuery)) assert.equal(env.route.query[key], value, `URL ${key}`)
      assertCombinedLoad(env, view)
      const saved = { ...env.route.query }

      await env.leave()
      assert.equal(env.route.path, '/platform/search')
      await env.back()
      assertCombinedLoad(env, view)
      await env.forward()
      assert.equal(env.route.path, '/platform/search')
      assert.deepEqual(env.ids(), [])
      await env.back()
      assertCombinedLoad(env, view)

      await env.navigate({ preserved: 'value', tab: 'documents' })
      assertFilters(env, {})
      assert.equal(env.ids().length, 35)
      for (const key of Object.keys(combinedQuery)) assert.equal(env.route.query[key], undefined, `default ${key} omitted`)
      const clearedRequest = env.requests.at(-1)!
      for (const key of Object.keys(combinedAPIQuery)) assert.equal(clearedRequest.has(key), false, `default API ${key} omitted`)
      await env.back()
      assertCombinedLoad(env, view)
      await env.forward()
      assertFilters(env, {})
      assert.equal(env.ids().length, 35)

      env.cleanup()
      env = await mountLibrary(locale, saved, view)
      assertCombinedLoad(env, view)
      assert.equal(env.requests.length, 1, 'refresh loads restored filters immediately without an unfiltered request')
    } finally { env.cleanup() }
  })
}

test('default and invalid URL filters are omitted while unrelated query parameters survive', async () => {
  const env = await mountLibrary('zh-CN', {
    source_kind: 'invalid', content_type: ['invalid'], starred: '', file_type: null,
    parse_status: ['completed', 'failed'], source: '', keyword: '   ',
    start_date: '2026-02-30', end_date: 'invalid', tab: 'documents', preserved: 'value', custom: ['one', 'two'],
  })
  try {
    assertFilters(env, {})
    for (const key of Object.keys(combinedQuery)) assert.equal(env.route.query[key], undefined)
    assert.equal(env.route.query.preserved, 'value')
    assert.deepEqual(env.route.query.custom, ['one', 'two'])
    assert.equal(env.ids().length, 35)
    await env.change(env.control('articleJudgments.starLabel'), 'false')
    assert.equal(env.route.query.starred, 'false', 'unstarred is an active filter rather than a default')
    assert.ok(env.ids().every(id => !env.records.find(record => record.id === id)!.starred))
    await env.change(env.control('articleJudgments.starLabel'), '')
    assert.equal(env.route.query.starred, undefined)
    assert.deepEqual(env.route.query.custom, ['one', 'two'])
  } finally { env.cleanup() }
})

test('source and star filter changes clear selected entries before loading filtered data', async () => {
  const env = await mountLibrary('zh-CN', {}, 'table')
  try {
    const selected = () => descendants(env.root).filter(node => node.props['data-select-id'] && hasClass(node, 'rq-table__row--selected'))
    const select = (id: string) => {
      const row = descendants(env.root).find(node => node.props['data-select-id'] === id)!
      descendants(row).find(node => node.tag === 'input')!.props.onChange({ target: { checked: true } })
    }
    select('article')
    await settle()
    assert.equal(selected().length, 1)
    await env.change(env.control('articleJudgments.starLabel'), 'true')
    assert.equal(selected().length, 0)
    select('mine')
    await settle()
    assert.equal(selected().length, 1)
    await env.change(env.control('library.materialCategory'), 'mine')
    assert.equal(selected().length, 0)
    assert.ok(env.ids().every(id => id.startsWith('mine')))
  } finally { env.cleanup() }
})

for (const locale of ['zh-CN','en-US']) test(`${locale}: entity page mounts on initial library failure and retry restores entities`, async()=>{
 const page=await mountLibrary(locale,{tab:'entities'},'grid',true)
 try {
  assert.ok(visibleText(page.root).includes('Backend unavailable'))
  const retry=descendants(page.root).find(node=>node.tag==='button' && visibleText(node)===page.t('common.retry'))
  assert.ok(retry)
  page.recover();retry.props.onClick();await new Promise(resolve=>setImmediate(resolve));await settle()
  assert.ok(visibleText(page.root).includes('Recovered entity'))
  assert.ok(!visibleText(page.root).includes('Backend unavailable'))
 } finally { page.cleanup() }
})

test('library-defined content types and Markdown aliases survive a shared URL and reload', async () => {
  const env = await mountLibrary('zh-CN', { content_type: 'SOP', file_type: 'manual', source: 'api' }, 'grid', false, true)
  try {
    assert.deepEqual(env.ids(), ['sop'])
    assert.equal(descendants(env.root).find(node => node.tag === 'button' && visibleText(node) === 'SOP')!.props['aria-pressed'], true)
    assert.equal(env.byOptions('md').props.value, 'md')
    assert.equal(env.route.query.content_type, 'SOP')
    assert.equal(env.route.query.file_type, 'md')
    assert.equal(env.requests.at(-1)!.get('content_type'), 'SOP')
    assert.equal(env.requests.at(-1)!.get('file_type'), 'md')
    const channels = descendants(env.control('library.channelLabel')).filter(node => node.tag === 'option').map(node => ({ value: node.props.value }))
    assert.ok(!channels.some(item => ['manual', 'url'].includes(item.value)))
    assert.ok(env.control('library.materialCategory'))
    env.scope.value = ['reports']; await new Promise(resolve => setImmediate(resolve)); await settle()
    assert.equal(env.route.query.libraries, 'reports')
    assert.equal(env.requests.at(-1)!.get('libraries'), 'reports')
    await env.navigate({ libraries: 'library-source', entity_ids: 'entity-1', topic_ids: 'topic-1' })
    assert.deepEqual(env.scope.value, ['library-source'])
    assert.equal(env.requests.at(-1)!.get('libraries'), 'library-source')
    assert.equal(env.route.query.entity_ids, 'entity-1')
    assert.equal(env.route.query.topic_ids, 'topic-1')
    env.scope.value = []; await new Promise(resolve => setImmediate(resolve)); await settle()
    assert.equal(env.route.query.libraries, undefined)
    assert.equal(env.requests.at(-1)!.has('libraries'), false)
  } finally { env.cleanup() }
})

test('bulk management retains exact filters and server count across both steps and uses a separate action preview', async () => {
  const env = await mountLibrary('zh-CN', { keyword: 'Needle', starred: 'false', parse_status: 'failed', entity_ids: 'entity-1', topic_ids: 'topic-1', published_from: '2026-10-01', source_kind: 'mine', source: 'api', file_type: 'md', content_type: 'tutorial', start_date: '2026-10-01', end_date: '2026-10-18' })
  const button = (key: string) => descendants(env.root).find(node => node.tag === 'button' && visibleText(node) === env.t(key))
  const summary = () => visibleText(env.control('entryBatch.scopeSummary'))
  try {
    let countRequest = true
    env.setRebuildResponse(async path => {
      if (countRequest) { countRequest = false; return { success: true, data: { ...env.previewResult.data, matched: 73, eligible: 0, fingerprint: 'scope-only' } } }
      return path.endsWith('/preview') ? env.previewResult : env.batchResult
    })
    for (const key of ['library.rebuildLibrary', 'library.rebuildLibraryAll']) assert.equal(button(key), undefined)
    assert.equal(button('entryBatch.title')!.props.disabled, false)
    env.scope.value = ['library-source']; await settle()
    await env.change(env.control('entryBatch.entities'), ['entity-1'])
    await env.change(env.control('entryBatch.topics'), ['topic-1'])
    button('entryBatch.title')!.props.onClick(); await settle()
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.scopeStep')))
    assert.equal(env.control('entryBatch.action'), undefined)
    assert.equal(button('entryBatch.preview'), undefined)
    assert.ok(summary().includes(env.t('entryBatch.matchedCount', { count: 73 })), 'count comes from the batch server, not the displayed list')
    for (const value of ['Research', 'Target Entity', 'Target Topic', 'Needle', 'tutorial', 'Markdown', '2026-10-01', '2026-10-18', env.t('articleJudgments.unstarredOnly'), env.t('library.parseStatusFailed'), env.t('knowledgeSearch.sources.mine'), env.t('library.channelApi')]) assert.ok(summary().includes(value), value)
    const selection = { filters: { libraries: ['library-source'], entity_ids: ['entity-1'], topic_ids: ['topic-1'], published_from: '2026-10-01', keyword: 'Needle', starred: false, parse_status: 'failed', source_kind: 'mine', source: 'api', file_type: 'md', content_type: 'tutorial', start_time: '2026-10-01 00:00:00', end_time: '2026-10-18 23:59:59' } }
    assert.deepEqual(env.rebuildRequests, [{ path: '/api/v1/entries/batches/preview', data: { action: 'star', starred: true, selection } }])
    const firstSummary = summary()
    button('entryBatch.nextAction')!.props.onClick(); await settle()
    await env.change(env.control('entryBatch.action'), 'reprocess')
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.actionStep')))
    assert.equal(summary(), firstSummary, 'both steps retain the same filters and count')
    assert.equal(env.rebuildRequests.length, 1, 'changing action does not recount the scope')
    assert.equal(button('entryBatch.confirm'), undefined, 'scope counting never authorizes an action')
    button('entryBatch.preview')!.props.onClick(); await settle()
    assert.deepEqual(env.rebuildRequests[1], { path: '/api/v1/entries/batches/preview', data: { action: 'reprocess', selection } })
    assert.ok(summary().includes(env.t('entryBatch.matchedCount', { count: 41 })), 'the action preview refreshes the live count')
    assert.equal(button('entryBatch.confirm')!.props.disabled, true)
    button('entryBatch.confirm')!.props.onClick(); await settle()
    assert.equal(env.rebuildRequests.length, 2)
    descendants(env.root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!.props.onChange({ target: { checked: true } }); await settle()
    button('entryBatch.changeScope')!.props.onClick(); await settle()
    assert.equal(button('entryBatch.confirm'), undefined)
    assert.ok(summary().includes(env.t('entryBatch.matchedCount', { count: 41 })))
    button('entryBatch.nextAction')!.props.onClick(); await settle()
    assert.equal(env.control('entryBatch.action').props.value, 'reprocess')
    assert.equal(button('entryBatch.confirm'), undefined)
    assert.equal(env.rebuildRequests.length, 2, 'moving between steps preserves the scope count')
    button('entryBatch.preview')!.props.onClick(); await settle()
    assert.equal(button('entryBatch.confirm')!.props.disabled, true)
    descendants(env.root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!.props.onChange({ target: { checked: true } }); await settle()
    let finish!: (response: unknown) => void
    env.setRebuildResponse(() => new Promise(resolve => { finish = resolve }))
    button('entryBatch.confirm')!.props.onClick(); await settle()
    assert.ok(env.confirmation)
    void env.confirmation.onConfirm(); await settle()
    assert.equal(button('entryBatch.confirm')!.props.disabled, true)
    button('entryBatch.confirm')!.props.onClick(); await settle()
    assert.equal(env.rebuildRequests.length, 4, 'busy submit rejects duplicate calls')
    assert.deepEqual(env.rebuildRequests[3], { path: '/api/v1/entries/batches', data: { action: 'reprocess', selection, preview_fingerprint: 'preview-1' } })
    assert.deepEqual(env.notices, [])
    finish(env.batchResult); await new Promise(resolve => setImmediate(resolve)); await settle()
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.status.completed')))
    assert.equal(env.route.query.batch_id, 'batch-1')
    assert.ok(descendants(env.root).some(node => node.tag === 'a' && node.props.href === '/platform/entries?batch_id=batch-1'))
  } finally { env.cleanup() }
})

test('scope changes ignore stale counts and checked summaries distinguish unavailable IDs', async () => {
  const env = await mountLibrary('en-US', {}, 'table')
  const button = (key: string) => descendants(env.root).find(node => node.tag === 'button' && visibleText(node) === env.t(key))!
  const summary = () => visibleText(env.control('entryBatch.scopeSummary'))
  try {
    let finishOld!: (response: unknown) => void
    env.setRebuildResponse(async (_path, data: any) => data.selection.ids ? new Promise(resolve => { finishOld = resolve }) : { success: true, data: { ...env.previewResult.data, matched: 0 } })
    const row = descendants(env.root).find(node => node.props['data-select-id'] === 'mine')!
    descendants(row).find(node => node.tag === 'input')!.props.onChange({ target: { checked: true } }); await settle()
    button('entryBatch.title').props.onClick(); await settle()
    assert.equal(env.control('entryBatch.scope').props.value, 'selected')
    assert.ok(summary().includes(env.t('entryBatch.checkedHint')))
    assert.ok(summary().includes(env.t('entryBatch.countLoading')))
    assert.equal(button('entryBatch.nextAction').props.disabled, true)
    assert.deepEqual(env.rebuildRequests[0].data, { action: 'star', starred: true, selection: { ids: ['mine'] } })
    await env.change(env.control('entryBatch.scope'), 'matched')
    assert.ok(summary().includes(env.t('entryBatch.matchedCount', { count: 0 })), 'zero is a successful count')
    assert.equal(button('entryBatch.nextAction').props.disabled, false)
    finishOld({ success: true, data: { ...env.previewResult.data, matched: 999 } }); await settle()
    assert.ok(summary().includes(env.t('entryBatch.matchedCount', { count: 0 })))
    assert.ok(!summary().includes('999'))
    env.setRebuildResponse(async () => ({ success: true, data: { ...env.previewResult.data, matched: 1, reasons: [{ code: 'unavailable', count: 1 }] } }))
    await env.change(env.control('entryBatch.scope'), 'selected')
    assert.ok(summary().includes(env.t('entryBatch.selectedCount', { selected: 1, found: 0, unavailable: 1 })))
    button('entryBatch.nextAction').props.onClick(); await settle()
    assert.ok(summary().includes(env.t('entryBatch.selectedCount', { selected: 1, found: 0, unavailable: 1 })))
    button('common.close').props.onClick(); await settle()
    assert.ok(env.rebuildRequests.every(request => request.path.endsWith('/preview')), 'counting and cancellation never submit a batch')
  } finally { env.cleanup() }
})

test('scope count failures block advancement until retry and action conflicts remain retryable', async () => {
  const env = await mountLibrary('en-US')
  const button = (key: string) => descendants(env.root).find(node => node.tag === 'button' && visibleText(node) === env.t(key))!
  try {
    env.setRebuildResponse(async () => { throw new Error('Count unavailable') })
    button('entryBatch.title').props.onClick(); await settle()
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.countFailed')))
    assert.equal(button('entryBatch.nextAction').props.disabled, true)
    assert.equal(env.control('entryBatch.action'), undefined)
    env.setRebuildResponse(async () => env.previewResult)
    button('entryBatch.retryCount').props.onClick(); await settle()
    assert.equal(button('entryBatch.nextAction').props.disabled, false)
    button('entryBatch.nextAction').props.onClick(); await settle()
    await env.change(env.control('entryBatch.action'), 'reprocess')
    env.setRebuildResponse(async () => { throw { status: 409, message: 'A processing task is already running in this library.' } })
    button('entryBatch.preview').props.onClick(); await settle()
    assert.ok(visibleText(env.root).includes('A processing task is already running in this library.'))
    assert.deepEqual(env.notices, [])
    assert.equal(button('entryBatch.preview').props.disabled, false)
    env.setRebuildResponse(async path => path.endsWith('/preview') ? env.previewResult : env.batchResult)
    button('entryBatch.preview').props.onClick(); await settle()
    assert.ok(!visibleText(env.root).includes('A processing task is already running in this library.'))
    assert.equal(button('entryBatch.confirm').props.disabled, true)
    descendants(env.root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!.props.onChange({ target: { checked: true } }); await settle()
    button('entryBatch.confirm').props.onClick(); await settle()
    await env.confirmation.onConfirm(); await settle()
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.status.completed')))
    assert.equal(env.rebuildRequests.length, 5)
  } finally { env.cleanup() }
})

for (const kind of ['article', 'mine']) test(`${kind}: preview deletion waits for confirmation, retries failure and keeps the original deletion endpoint`, async () => {
  const env = await mountLibrary('en-US', { source_kind: kind }, 'list', false, false, false, true)
  try {
    const remove = descendants(env.root).find(node => node.tag === 'button' && visibleText(node) === env.t('library.deleteDocument'))!
    assert.ok(remove)
    remove.props.onClick(); await settle()
    assert.deepEqual(env.deleteCalls, [])
    assert.ok(env.confirmation.body.includes(kind === 'article' ? 'Needle article' : 'Needle document'))
    await assert.rejects(() => env.confirmation.onConfirm())
    assert.ok(env.ids().includes(kind))
    env.allowDelete()
    await env.confirmation.onConfirm(); await settle()
    assert.deepEqual(env.deleteCalls, [`/api/v1/${kind === 'article' ? 'entries' : 'documents'}/${kind}`, `/api/v1/${kind === 'article' ? 'entries' : 'documents'}/${kind}`])
    assert.ok(!env.ids().includes(kind))
  } finally { env.cleanup() }
})

test('a failed list request is distinct from an empty result and retries the same filters', async () => {
  const env = await mountLibrary('en-US')
  try {
    env.setListUnavailable(true)
    await env.change(env.control('library.materialCategory'), 'mine')
    assert.ok(visibleText(env.root).includes(env.t('entries.loadFailed')))
    assert.ok(!visibleText(env.root).includes(env.t('library.emptyArticles')))
    env.setListUnavailable(false)
    const retry = descendants(env.root).find(node => node.tag === 'button' && visibleText(node) === env.t('common.retry'))!
    retry.props.onClick(); await new Promise(resolve => setImmediate(resolve)); await settle()
    assert.ok(!visibleText(env.root).includes(env.t('entries.loadFailed')))
    assert.ok(env.ids().every(id => id.startsWith('mine')))
    assert.equal(env.requests.at(-1)!.get('source_kind'), 'mine')
  } finally { env.cleanup() }
})
