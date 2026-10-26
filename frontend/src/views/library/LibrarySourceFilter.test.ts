import * as bodyReference from '../../utils/bodyReference'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent, h, onMounted, ref } from 'vue'
import ts from 'typescript'

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

async function mountLibrary(locale = 'zh-CN', initialQuery: Record<string, any> = {}, view = 'grid', unavailable = false, customType = false, noLibraries = false) {
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
    { id: 'article', source_kind: 'article', title: 'Needle article',  starred: true },
    { id: 'mine', source_kind: 'mine', title: 'Needle document',  starred: true },
    { id: 'report', source_kind: 'report', title: 'Needle report',  starred: false },
    ...Array.from({ length: 38 }, (_, index) => ({ id: `mine-${index}`, source_kind: 'mine', title: `Document ${index}`,  starred: false })),
  ].map(record => ({ ...record, file_name: `${record.title}.md`, type: 'manual', file_type: 'md', channel: 'api',
    parse_status: 'completed', incomplete: record.id === 'mine', created_at: '2026-10-13T08:00:00Z', updated_at: '2026-10-13T08:00:00Z' }))
  if (customType) records.push({ ...records[0], id: 'sop', })
  const target = records.find(record => record.id === 'mine')!
  for (const [field, value] of Object.entries({ channel: 'web', file_type: 'pdf', parse_status: 'failed',
    updated_at: '2026-09-01T08:00:00Z', title: 'Unrelated document', starred: false, })) {
    records.push({ ...target, id: `mine-other-${field}`, [field]: value })
  }
  const transport = {
    async post(path: string, data: unknown) { rebuildRequests.push({ path, data }); return rebuildResponse(path, data) },
    async get(path: string) {
      const url = new URL(path, 'http://localhost')
      assert.equal(url.pathname, '/api/v1/entries')
      const query = new URLSearchParams(url.searchParams)
      requests.push(query)
      const filtered = records.filter(record => {
        if (query.has('source_kind') && record.source_kind !== query.get('source_kind')) return false
        if (query.has('keyword') && !record.title.toLowerCase().includes(query.get('keyword')!.toLowerCase())) return false
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
    '@/api/domains': { listDomains: async () => ({ success: true, data: [] }) },
    '@/api/entities': { listEntities: async () => ({ success: true, data: [{ id: 'entity-1', canonical_name: 'Target Entity' }] }) },

    '@/api/library': api, '@/stores/entry': store,
    '../utils/index': { formatStringDate: () => '2026-10-13' },
    'tdesign-vue-next': { MessagePlugin: {} },
    '@/stores/resources': { useResourcesStore: () => ({}) },
  })
  const empty = moduleOf(defineComponent({ setup: () => () => null }))
  let backendAvailable = !unavailable
  const modules: Record<string, any> = {
    '@/api/domains': { listDomains: async () => ({ success: true, data: [] }) },
    '@/api/entities': { listEntities: async () => ({ success: true, data: [{ id: 'entity-1', canonical_name: 'Target Entity' }] }) },

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
    '@/types/entryClassification': { },
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
  modules['@/components/EmptyEntry.vue'] = moduleOf(loadComponent(new URL('../../components/EmptyEntry.vue', import.meta.url), modules))
  modules['./components/EntityDomainSelect.vue'] = moduleOf(loadComponent(new URL('./components/EntityDomainSelect.vue', import.meta.url), modules))
  modules['@/components/EntryBatchDialog.vue'] = moduleOf(loadComponent(new URL('../../components/EntryBatchDialog.vue', import.meta.url), modules))
  modules['@/components/LibraryScopeSelect.vue'] = moduleOf(defineComponent({
    emits: ['loaded'], setup: (_, { emit }) => {
      onMounted(() => emit('loaded', noLibraries ? [] : [
        { id: 'library-source', name: 'Research', display_name: 'Research', template: 'own', },
        { id: 'reports', name: 'AI Reports', template: 'generated', },
      ]))
      return () => null
    },
  }))
  modules['@/components/SelectionCheckbox.vue'] = moduleOf(loadComponent(new URL('../../components/SelectionCheckbox.vue', import.meta.url), modules))
  for (const view of ['Card', 'List']) modules[`./components/Document${view}View.vue`] = moduleOf(
    loadComponent(new URL(`./components/Document${view}View.vue`, import.meta.url), modules),
  )
  if (unavailable) {
    modules['@/api/entities'] = { listEntities: async () => { if (!backendAvailable) throw new Error('Backend unavailable'); return {success:true,data:[{id:'recovered',canonical_name:'Recovered entity',aliases:[]}] } } }
    modules['@/stores/follows'] = {useFollowStore:()=>({refreshAfterTargetChange:async()=>{}})}
    modules['./EntityCandidates.vue']=empty;modules['./EntityEditor.vue']=empty;modules['./EntityActivity.vue']=empty;modules['@/components/FollowToggle.vue']=empty
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
    setRebuildResponse(response: (path: string, data: unknown) => Promise<any>) { rebuildResponse = response },
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

test('a space with no libraries finishes loading and displays the material empty state', async () => {
  const env = await mountLibrary('zh-CN', {}, 'grid', false, false, true)
  try {
    assert.equal(env.requests.length, 0)
    assert.equal(descendants(env.root).some(node => hasClass(node, 'entry-card-skeleton')), false)
    assert.ok(descendants(env.root).some(node => hasClass(node, 'doc-empty-state')))
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

test('the shared entity and domain selector preserves material filter requests and URL selections', async () => {
  const env = await mountLibrary('en-US', { entity_ids: 'entity-1', domain_ids: 'domain-1' })
  try {
    const control = env.control('domains.mixedFilter')
    assert.deepEqual(control.props.value, ['entity:entity-1', 'domain:domain-1'])
    assert.deepEqual(control.props['data-options'].map((group: any) => group.children), [
      [{ value: 'entity:entity-1', label: 'Target Entity' }],
      // listDomains returned the full list without domain-1, so the URL points at a deleted domain.
      [{ value: 'domain:domain-1', label: 'Deleted domain' }],
    ])
    await env.change(control, ['entity:entity-1', 'domain:domain-2'])
    assert.equal(env.requests.at(-1)!.get('entity_ids'), 'entity-1')
    assert.equal(env.requests.at(-1)!.get('domain_ids'), 'domain-2')
    assert.equal(env.route.query.domain_ids, 'domain-2')
    await env.change(env.control('domains.mixedFilter'), [])
    assert.equal(env.requests.at(-1)!.has('entity_ids'), false)
    assert.equal(env.requests.at(-1)!.has('domain_ids'), false)
  } finally { env.cleanup() }
})

test('the visible processing status filter finds active materials and restores its URL selection', async () => {
  const env = await mountLibrary('zh-CN')
  try {
    env.records[0].parse_status = 'processing'
    const status = env.control('library.parseStatusFilter')
    assert.ok(status, 'the material toolbar exposes the processing status filter')
    assert.equal(status.props.value, '')
    assert.ok(status.props['data-options'].some((option: any) => option.value === 'processing' && option.label === '处理中'))
    await env.change(status, 'processing')
    assert.equal(env.requests.at(-1)!.get('parse_status'), 'processing')
    assert.equal(env.requests.at(-1)!.get('page'), '1')
    assert.equal(env.route.query.parse_status, 'processing')
    assert.deepEqual(env.ids(), ['article'])
    await env.leave()
    await env.back()
    assert.equal(env.control('library.parseStatusFilter').props.value, 'processing')
    assert.deepEqual(env.ids(), ['article'])
    await env.change(env.control('library.parseStatusFilter'), '')
    assert.equal(env.requests.at(-1)!.has('parse_status'), false)
    assert.equal(env.route.query.parse_status, undefined)
    assert.ok(env.ids().length > 1)
  } finally { env.cleanup() }
})

for (const locale of ['zh-CN', 'en-US']) {

}



const combinedQuery = {
  source_kind: 'mine',  starred: 'true', file_type: 'md',
  parse_status: 'completed', source: 'api', keyword: 'Needle',
  start_date: '2026-10-01', end_date: '2026-10-13',
}
const combinedAPIQuery = {
  source_kind: 'mine',  starred: 'true', file_type: 'md',
  parse_status: 'completed', source: 'api', keyword: 'Needle',
  start_time: '2026-10-01 00:00:00', end_time: '2026-10-13 23:59:59',
}
function assertFilters(env: Awaited<ReturnType<typeof mountLibrary>>, filters: Partial<typeof combinedQuery>) {
  assert.equal(env.control('articleJudgments.starLabel').props.value, filters.starred || '')

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

}

test('default and invalid URL filters are omitted while unrelated query parameters survive', async () => {
  const env = await mountLibrary('zh-CN', {
    source_kind: 'invalid',  starred: '', file_type: null,
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

test('star filter changes clear selected entries before loading filtered data', async () => {
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
    await env.change(env.control('articleJudgments.starLabel'), 'false')
    assert.equal(selected().length, 0)
    assert.equal(env.requests.at(-1)!.get('starred'), 'false')
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



test('bulk management retains exact filters and server count across both steps and uses a separate action preview', async () => {
  const env = await mountLibrary('zh-CN', { keyword: 'Needle', starred: 'false', parse_status: 'failed', entity_ids: 'entity-1',  published_from: '2026-10-01', source_kind: 'mine', source: 'api', file_type: 'md',  start_date: '2026-10-01', end_date: '2026-10-18' })
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
    button('entryBatch.title')!.props.onClick(); await settle()
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.scopeStep')))
    assert.equal(env.control('entryBatch.action'), undefined)
    assert.equal(button('entryBatch.preview'), undefined)
    assert.ok(summary().includes(env.t('entryBatch.matchedCount', { count: 73 })), 'count comes from the batch server, not the displayed list')
    for (const value of ['Research', 'Needle', 'Markdown', '2026-10-01', '2026-10-18', env.t('articleJudgments.unstarredOnly'), env.t('library.parseStatusFailed'), env.t('knowledgeSearch.sources.mine'), env.t('library.channelApi')]) assert.ok(summary().includes(value), value)
    const selection = { filters: { entity_ids: ['entity-1'], libraries: ['library-source'],  published_from: '2026-10-01', keyword: 'Needle', starred: false, parse_status: 'failed', source_kind: 'mine', source: 'api', file_type: 'md',  start_time: '2026-10-01 00:00:00', end_time: '2026-10-18 23:59:59' } }
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
    descendants(env.root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!.props.onChange({ checked: true }); await settle()
    button('entryBatch.changeScope')!.props.onClick(); await settle()
    assert.equal(button('entryBatch.confirm'), undefined)
    assert.ok(summary().includes(env.t('entryBatch.matchedCount', { count: 41 })))
    button('entryBatch.nextAction')!.props.onClick(); await settle()
    assert.equal(env.control('entryBatch.action').props.value, 'reprocess')
    assert.equal(button('entryBatch.confirm'), undefined)
    assert.equal(env.rebuildRequests.length, 2, 'moving between steps preserves the scope count')
    button('entryBatch.preview')!.props.onClick(); await settle()
    assert.equal(button('entryBatch.confirm')!.props.disabled, true)
    descendants(env.root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!.props.onChange({ checked: true }); await settle()
    let finish!: (response: unknown) => void
    env.setRebuildResponse(() => new Promise(resolve => { finish = resolve }))
    button('entryBatch.confirm')!.props.onClick(); await settle()
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
  const env = await mountLibrary('en-US', {}, 'list')
  const button = (key: string) => descendants(env.root).find(node => node.tag === 'button' && visibleText(node) === env.t(key))!
  const summary = () => visibleText(env.control('entryBatch.scopeSummary'))
  try {
    let finishOld!: (response: unknown) => void
    env.setRebuildResponse(async (_path, data: any) => data.selection.ids ? new Promise(resolve => { finishOld = resolve }) : { success: true, data: { ...env.previewResult.data, matched: 0 } })
    const row = descendants(env.root).find(node => node.props['data-select-id'] === 'mine')!
    descendants(row).find(node => node.tag === 'input')!.props.onChange({ checked: true }); await settle()
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
    descendants(env.root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!.props.onChange({ checked: true }); await settle()
    button('entryBatch.confirm').props.onClick(); await settle()
    assert.ok(visibleText(env.root).includes(env.t('entryBatch.status.completed')))
    assert.equal(env.rebuildRequests.length, 5)
  } finally { env.cleanup() }
})
