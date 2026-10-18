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

async function mountLibrary(locale = 'zh-CN', initialQuery: Record<string, any> = {}, view = 'grid', unavailable = false, customType = false) {
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
  let rebuildResponse: (path: string) => Promise<any> = async path => path.endsWith('/preview') ? previewResult : batchResult
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
  const transport = {
    async post(path: string, data: unknown) { rebuildRequests.push({ path, data }); return rebuildResponse(path) },
    async get(path: string) {
      const url = new URL(path, 'http://localhost')
      assert.equal(url.pathname, '/api/v1/entries')
      const query = new URLSearchParams(url.searchParams)
      requests.push(query)
      const filtered = records.filter(record => {
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
    '@/api/entities': { listEntities: async () => ({ success: true, data: [] }) },
    '@/api/topics': { listTopics: async () => ({ success: true, data: [] }) },
    '@/api/library': api, '@/stores/entry': store,
    '../utils/index': { formatStringDate: () => '2026-10-13' },
    'tdesign-vue-next': { MessagePlugin: {} },
    '@/stores/resources': { useResourcesStore: () => ({}) },
  })
  const empty = moduleOf(defineComponent({ setup: () => () => null }))
  let backendAvailable = !unavailable
  const modules: Record<string, any> = {
    '@/api/entities': { listEntities: async () => ({ success: true, data: [] }) },
    '@/api/topics': { listTopics: async () => ({ success: true, data: [] }) },
    '@/utils/bodyReference': bodyReference,
    '@/composables/useLibraryScope': { useLibraryScope: () => scope },
    'vue-router': vueRouter,
    '@/composables/useLibrary': hook,
    '@/stores/ui': { useUIStore: () => ({}) },
    '@/stores/pendingCounts': { usePendingCountsStore: () => ({ total: null, refresh: async () => {} }) },
    '@/stores/commandPalette': { useCommandPaletteStore: () => ({}) },
    '@/stores/resources': { useResourcesStore: () => ({ library: unavailable ? null : { id: 'library-source' }, fetchLibrary: async () => { if (!backendAvailable) throw new Error('Backend unavailable'); return { id: 'library-source' } } }) },
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
    'tdesign-vue-next': { MessagePlugin: { success: (message: string) => notices.push(message) }, DialogPlugin: { confirm: (options: unknown) => { confirmation = options; return { destroy() { confirmation = undefined } } } } },
    './entryStatusPolling': polling,
    './entryDownloadFileName': {},
    './documentListFilters': documentListFilters,
  }
  for (const path of ['./Library.vue', './components/DocumentCardView.vue', './components/DocumentListView.vue']) {
    for (const match of readFileSync(new URL(path, import.meta.url), 'utf8').matchAll(/from\s+['"]([^'"]+\.vue)['"]/g)) modules[match[1]] = empty
  }
  modules['@/components/EntryBatchDialog.vue'] = moduleOf(loadComponent(new URL('../../components/EntryBatchDialog.vue', import.meta.url), modules))
  modules['@/components/LibraryScopeSelect.vue'] = moduleOf(defineComponent({
    emits: ['loaded'], setup: (_, { emit }) => {
      onMounted(() => emit('loaded', [
        { id: 'library-source', name: 'Research', display_name: 'Research', template: 'own', content_types: [...CONTENT_TYPES, 'SOP'].map(name => ({ name, enabled: true })) },
        { id: 'reports', name: 'AI Reports', template: 'generated', content_types: [] },
      ]))
      return () => null
    },
  }))
  modules['@/components/SelectionCheckbox.vue'] = moduleOf(loadComponent(new URL('../../components/SelectionCheckbox.vue', import.meta.url), modules))
  for (const view of ['Card', 'List']) modules[`./components/Document${view}View.vue`] = moduleOf(
    loadComponent(new URL(`./components/Document${view}View.vue`, import.meta.url), modules),
  )
  if (unavailable) {
    modules['@/api/entities'] = { ENTITY_TYPES: ['company'], listEntities: async () => { if (!backendAvailable) throw new Error('Backend unavailable'); return {success:true,data:[{id:'recovered',canonical_name:'Recovered entity',entity_type:'company',aliases:[]}] } }, getEntityNameEmbeddingRebuildStatus: async () => { if (!backendAvailable) throw new Error('Backend unavailable'); return {success:true,data:{pending_count:0}} } }
    modules['@/stores/follows'] = {useFollowStore:()=>({refreshAfterTargetChange:async()=>{}})}
    modules['./EntityGraph.vue']=empty;modules['./MergeSuggestions.vue']=empty;modules['./EntityRelations.vue']=empty;modules['@/components/FollowToggle.vue']=empty
    modules['./components/EntryWorkbench.vue']=moduleOf(loadComponent(new URL('./components/EntryWorkbench.vue',import.meta.url),modules))
  }
  const library = loadComponent(new URL('./Library.vue', import.meta.url), modules)
  const router = vueRouter.createRouter({ history: vueRouter.createMemoryHistory(), routes: [
    { path: '/platform/library', component: library },
    { path: '/platform/search', component: empty.default },
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
  for (const name of ['t-dropdown', 't-skeleton', 't-pagination']) app.component(name, empty.default)
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
    node.tag === 'select' && node.props['data-options']?.some((option: any) => option.value === value))!
  const ids = () => descendants(root).filter(node => node.props['data-select-id']).map(node => node.props['data-select-id'])
  return {
    root, router, requests, records, errors, control, byOptions, ids, t, scope, rebuildRequests, notices, previewResult, batchResult,
    get confirmation() { return confirmation },
    setRebuildResponse(response: (path: string) => Promise<any>) { rebuildResponse = response },
    recover() { backendAvailable = true },
    get route() { return router.currentRoute.value },
    async change(node: ReturnType<typeof element>, value: unknown) { node.props.onChange({ value }); await waitForLoad() },
    async navigate(query: Record<string, any>) { await router.push({ path: '/platform/library', query }); await waitForLoad() },
    async leave() { await router.push('/platform/search'); await waitForLoad() },
    async back() { router.back(); await waitForLoad() },
    async forward() { router.forward(); await waitForLoad() },
    async list() {
      descendants(root).find(node => hasClass(node, 'doc-view-toggle-btn') && node.props['aria-pressed'] === false)!.props.onClick()
      await settle()
      assert.ok(descendants(root).some(node => hasClass(node, 'doc-list-row')))
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

for (const locale of ['zh-CN', 'en-US']) {
  test(`${locale}: source filter loads cards and rows through the real hook and API with pagination`, async () => {
    const env = await mountLibrary(locale)
    try {
      const source = env.control('library.materialCategory')
      assert.deepEqual(source.props['data-options'], [
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

test('missing-image filter persists in the URL and combines with processing status', async () => {
  const env = await mountLibrary('zh-CN', { incomplete: 'true', parse_status: 'completed' })
  try {
    const images = env.control('library.imageStatusFilter')
    assert.equal(images.props.value, 'true')
    assert.equal(env.requests.at(-1)!.get('incomplete'), 'true')
    assert.ok(env.ids().length > 0)
    assert.ok(env.ids().every(id => env.records.find(record => record.id === id)?.incomplete))
    await env.list()
    assert.ok(env.ids().every(id => env.records.find(record => record.id === id)?.incomplete))
    await env.change(images, 'false')
    assert.equal(env.route.query.incomplete, 'false')
    assert.equal(env.requests.at(-1)!.get('parse_status'), 'completed')
    assert.ok(env.ids().every(id => !env.records.find(record => record.id === id)?.incomplete))
    await env.leave()
    await env.back()
    assert.equal(env.control('library.imageStatusFilter').props.value, 'false')
    assert.equal(env.requests.at(-1)!.get('incomplete'), 'false')
    await env.change(env.control('library.imageStatusFilter'), '')
    assert.equal(env.route.query.incomplete, undefined)
    assert.equal(env.requests.at(-1)!.has('incomplete'), false)
  } finally { env.cleanup() }
})

test('source filters combine with existing filters and retain URL state on reload and history changes', async () => {
  const env = await mountLibrary('zh-CN', { source_kind: 'mine', tab: 'documents' })
  try {
    assert.ok(env.ids().every(id => id.startsWith('mine')))
    assert.equal(env.requests[0].get('source_kind'), 'mine')
    await env.change(env.byOptions('tutorial'), 'tutorial')
    await env.change(env.control('articleJudgments.starLabel'), 'true')
    await env.change(env.byOptions('md'), 'md')
    await env.change(env.byOptions('completed'), 'completed')
    await env.change(env.control('library.channelLabel'), 'api')
    const date = descendants(env.root).find(node => hasClass(node, 'doc-date-range--updated'))!
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
    source_kind: env.control('library.materialCategory'), content_type: env.byOptions('tutorial'),
    starred: env.control('articleJudgments.starLabel'), file_type: env.byOptions('md'),
    parse_status: env.byOptions('completed'), source: env.control('library.channelLabel'),
    keyword: descendants(env.root).find(node => hasClass(node, 'doc-search-input'))!,
  })) assert.equal(node.props.value, filters[key as keyof typeof combinedQuery] || '', `restored ${key} control`)
  const date = descendants(env.root).find(node => hasClass(node, 'doc-date-range--updated'))!
  assert.deepEqual(date.props.value, filters.start_date || filters.end_date ? [filters.start_date || '', filters.end_date || ''] : [])
}
function assertCombinedLoad(env: Awaited<ReturnType<typeof mountLibrary>>, view: string) {
  assertFilters(env, combinedQuery)
  assert.deepEqual(env.ids(), ['mine'])
  const request = env.requests.at(-1)!
  for (const [key, value] of Object.entries(combinedAPIQuery)) assert.equal(request.get(key), value, `restored API ${key}`)
  assert.equal(request.get('page'), '1')
  assert.equal(env.route.query.preserved, 'value')
  assert.equal(env.route.query.tab, 'documents')
  assert.ok(descendants(env.root).some(node => hasClass(node, view === 'list' ? 'doc-list-row' : 'entry-card')))
}

for (const locale of ['zh-CN', 'en-US']) for (const view of ['grid', 'list']) {
  test(`${locale} ${view}: every filter persists and restores through reload, route return, history back and forward`, async () => {
    let env = await mountLibrary(locale, { tab: 'documents' }, view)
    try {
      await env.change(env.control('library.materialCategory'), combinedQuery.source_kind)
      await env.change(env.byOptions('tutorial'), combinedQuery.content_type)
      await env.change(env.control('articleJudgments.starLabel'), combinedQuery.starred)
      await env.change(env.byOptions('md'), combinedQuery.file_type)
      await env.change(env.byOptions('completed'), combinedQuery.parse_status)
      await env.change(env.control('library.channelLabel'), combinedQuery.source)
      await env.change(descendants(env.root).find(node => hasClass(node, 'doc-date-range--updated'))!, [combinedQuery.start_date, combinedQuery.end_date])
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

test('source, star and image filter changes clear selected entries before loading filtered data', async () => {
  const env = await mountLibrary('zh-CN', {}, 'list')
  try {
    const selected = () => descendants(env.root).filter(node => node.props['data-select-id'] && hasClass(node, 'selected'))
    const select = (id: string) => {
      const row = descendants(env.root).find(node => node.props['data-select-id'] === id)!
      descendants(row).find(node => node.tag === 'input')!.props.onChange({ checked: true })
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
    select('mine')
    await settle()
    await env.change(env.control('library.imageStatusFilter'), 'false')
    assert.equal(selected().length, 0)
    assert.ok(env.ids().every(id => !env.records.find(record => record.id === id)!.incomplete))
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
    assert.equal(env.byOptions('SOP').props.value, 'SOP')
    assert.equal(env.byOptions('md').props.value, 'md')
    assert.equal(env.route.query.content_type, 'SOP')
    assert.equal(env.route.query.file_type, 'md')
    assert.equal(env.requests.at(-1)!.get('content_type'), 'SOP')
    assert.equal(env.requests.at(-1)!.get('file_type'), 'md')
    const channels = env.control('library.channelLabel').props['data-options'] as Array<{ value: string }>
    assert.ok(!channels.some(item => ['manual', 'url'].includes(item.value)))
    assert.ok(env.control('library.materialCategory'))
  } finally { env.cleanup() }
})

test('bulk management chooses scope before action and preserves every list filter through confirmation', async () => {
  const env = await mountLibrary('zh-CN', { keyword: 'Needle', starred: 'true', parse_status: 'failed', entity_ids: 'entity-1', topic_ids: 'topic-1', published_from: '2026-10-01' })
  const button = (key: string) => descendants(env.root).find(node => node.tag === 'button' && visibleText(node) === env.t(key))
  try {
    for (const key of ['library.rebuildLibrary', 'library.rebuildLibraryAll', 'knowledgeReview.reviewAll']) assert.equal(button(key), undefined)
    assert.equal(button('entryBatch.title')!.props.disabled, false, 'all-match management is available without checked rows or a single library')
    env.scope.value = ['library-source']; await settle()
    button('entryBatch.title')!.props.onClick(); await settle()
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.scopeStep')))
    assert.equal(env.control('entryBatch.action'), undefined)
    assert.equal(button('entryBatch.preview'), undefined)
    assert.deepEqual(env.rebuildRequests, [])
    button('entryBatch.nextAction')!.props.onClick(); await settle()
    await env.change(env.control('entryBatch.action'), 'reprocess')
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.actionStep')))
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.reprocessHint')))
    const selection = { filters: { libraries: ['library-source'], entity_ids: ['entity-1'], topic_ids: ['topic-1'], published_from: '2026-10-01', keyword: 'Needle', starred: true, parse_status: 'failed' } }
    button('entryBatch.preview')!.props.onClick(); await settle()
    assert.deepEqual(env.rebuildRequests, [{ path: '/api/v1/entries/batches/preview', data: { action: 'reprocess', selection } }])
    assert.ok(visibleText(env.root).includes('Matching sample'))
    assert.equal(button('entryBatch.confirm')!.props.disabled, true)
    button('entryBatch.confirm')!.props.onClick(); await settle()
    assert.equal(env.rebuildRequests.length, 1, 'unchecked confirmation cannot submit')
    descendants(env.root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!.props.onChange({ checked: true }); await settle()
    button('entryBatch.changeScope')!.props.onClick(); await settle()
    assert.equal(button('entryBatch.confirm'), undefined)
    button('entryBatch.nextAction')!.props.onClick(); await settle()
    assert.equal(env.control('entryBatch.action').props.value, 'reprocess', 'returning preserves the chosen action')
    assert.equal(button('entryBatch.confirm'), undefined, 'returning invalidates preview and confirmation')
    button('entryBatch.preview')!.props.onClick(); await settle()
    assert.equal(button('entryBatch.confirm')!.props.disabled, true)
    descendants(env.root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!.props.onChange({ checked: true }); await settle()
    let finish!: (response: unknown) => void
    env.setRebuildResponse(() => new Promise(resolve => { finish = resolve }))
    button('entryBatch.confirm')!.props.onClick(); await settle()
    assert.equal(button('entryBatch.confirm')!.props.disabled, true)
    assert.equal(button('entryBatch.confirm')!.props['data-loading'], true)
    button('entryBatch.confirm')!.props.onClick(); await settle()
    assert.equal(env.rebuildRequests.length, 3, 'busy submit rejects duplicate calls')
    assert.deepEqual(env.rebuildRequests[2], { path: '/api/v1/entries/batches', data: { action: 'reprocess', selection, preview_fingerprint: 'preview-1' } })
    assert.deepEqual(env.notices, [], 'submission never announces completed success')
    finish(env.batchResult); await new Promise(resolve => setImmediate(resolve)); await settle()
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.status.completed')))
    assert.equal(env.route.query.batch_id, 'batch-1')
    assert.ok(descendants(env.root).some(node => node.tag === 'a' && node.props.href === '/platform/entries?batch_id=batch-1'))
  } finally { env.cleanup() }
})

test('bulk checked scope can switch to all matches and cancellation submits nothing', async () => {
  const env = await mountLibrary('en-US', {}, 'list')
  const button = (key: string) => descendants(env.root).find(node => node.tag === 'button' && visibleText(node) === env.t(key))
  try {
    const row = descendants(env.root).find(node => node.props['data-select-id'] === 'mine')!
    descendants(row).find(node => node.tag === 'input')!.props.onChange({ checked: true }); await settle()
    button('entryBatch.title')!.props.onClick(); await settle()
    assert.equal(env.control('entryBatch.scope').props.value, 'selected')
    button('entryBatch.nextAction')!.props.onClick(); await settle()
    button('entryBatch.preview')!.props.onClick(); await settle()
    assert.deepEqual(env.rebuildRequests[0].data, { action: 'star', starred: true, selection: { ids: ['mine'] } })
    button('entryBatch.changeScope')!.props.onClick(); await settle()
    await env.change(env.control('entryBatch.scope'), 'matched')
    button('entryBatch.nextAction')!.props.onClick(); await settle()
    button('entryBatch.preview')!.props.onClick(); await settle()
    assert.deepEqual(env.rebuildRequests[1].data, { action: 'star', starred: true, selection: { filters: { libraries: [], entity_ids: [], topic_ids: [] } } })
    button('common.close')!.props.onClick(); await settle()
    assert.equal(button('entryBatch.preview'), undefined)
    assert.equal(env.rebuildRequests.length, 2, 'cancellation never submits a batch')
  } finally { env.cleanup() }
})

test('bulk management retains a readable conflict and allows a deliberate preview retry', async () => {
  const env = await mountLibrary('en-US')
  const button = (key: string) => descendants(env.root).find(node => node.tag === 'button' && visibleText(node) === env.t(key))!
  try {
    env.setRebuildResponse(async () => { throw { status: 409, message: 'A processing task is already running in this library.' } })
    button('entryBatch.title').props.onClick(); await settle()
    button('entryBatch.nextAction').props.onClick(); await settle()
    await env.change(env.control('entryBatch.action'), 'reprocess')
    button('entryBatch.preview').props.onClick(); await settle()
    assert.ok(visibleText(env.root).includes('A processing task is already running in this library.'))
    assert.deepEqual(env.notices, [])
    assert.equal(button('entryBatch.preview').props.disabled, false)
    assert.equal(env.rebuildRequests.length, 1)
    env.setRebuildResponse(async path => path.endsWith('/preview') ? env.previewResult : env.batchResult)
    button('entryBatch.preview').props.onClick(); await settle()
    assert.ok(!visibleText(env.root).includes('A processing task is already running in this library.'))
    assert.equal(env.rebuildRequests.length, 2)
    assert.equal(button('entryBatch.confirm').props.disabled, true)
    descendants(env.root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!.props.onChange({ checked: true }); await settle()
    button('entryBatch.confirm').props.onClick(); await settle()
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.status.completed')))
    assert.equal(env.rebuildRequests.length, 3)
  } finally { env.cleanup() }
})
