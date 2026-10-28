import assert from 'node:assert/strict'
import { graphElements, graphStyles } from './entityGraphPresentation'
import test, { type TestContext } from 'node:test'
import { defineComponent, h, onMounted, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import { ENTITY_TYPES } from '../../../api/entity-types'
import { scopedLibraryIDs } from '../../../utils/libraryScope'
import type { SearchEntity, SourceLocation } from '../../../types/search'
import type { GraphSearchRequest, GraphSearchResponse } from '../../../types/graph'
import { click, descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../componentTestHarness'

const center: SearchEntity = { id: 'company', library_id: 'official', canonical_name: '星河科技', entity_type: 'company' }
const peer: SearchEntity = { id: 'product', library_id: 'official', canonical_name: '云帆助手', entity_type: 'product' }
const location: SourceLocation = { heading_path: ['产品概述'], paragraph: 3, start: 10, end: 20, body_hash: 'body-hash', mapping: 'exact' }
const result = (): GraphSearchResponse => ({
  status: 'ok', libraries: ['official'], mode: 'neighbors', hops: 1, expansion_limit: 100, candidates: {},
  entities: [center, peer], paths: [], documents: [], truncated: true, truncations: [{ reason: 'document_limit' }], pending_sync: { global: 3, involved: 1 },
  edges: ['official', 'media'].map(library => ({ id: `edge-${library}`, library_id: library, subject_id: peer.id, object_id: center.id, relation_type: '隶属', version: 1, source_count: 1, sources: [{ entry_id: `entry-${library}`, title: '云帆助手产品介绍', library_id: library, library_name: library, library_directory: library, published_at: null, source_unverified: true, acquisition: 'verbatim', evidence: [{ locations: [location] }] }] })),
})

async function fixture(t: TestContext, search: (request: GraphSearchRequest, signal?: AbortSignal) => Promise<unknown> = async () => ({ success: true, data: result() })) {
  const globals = ['ResizeObserver', 'MutationObserver', 'getComputedStyle', 'document'] as const
  const previous = globals.map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const)
  class Observer { observe() {} disconnect() {} }
  Object.defineProperties(globalThis, {
    ResizeObserver: { configurable: true, value: Observer }, MutationObserver: { configurable: true, value: Observer },
    document: { configurable: true, value: { documentElement: {} } },
    getComputedStyle: { configurable: true, value: () => ({ fontFamily: 'sans-serif', getPropertyValue: () => '#526a88' }) },
  })
  const requests: Array<{ request: GraphSearchRequest; signal?: AbortSignal }> = []
  const graphs: any[] = [], sources: Array<[string, SourceLocation | undefined]> = [], centers: string[] = []
  const selectedEntity = ref(center), active = ref(true), savedScope = ref<string[]>([])
  const component = loadComponent(new URL('./EntityGraph.vue', import.meta.url), {
    'cytoscape': { __esModule: true, default: (options: any) => {
      const graph = { options, handlers: {} as Record<string, (event: unknown) => void>, destroyed: false, moves: [] as unknown[],
        on(_name: string, selector: string, handler: (event: unknown) => void) { graph.handlers[selector] = handler },
        elements: () => options.elements.map((item: any) => ({ data: (key: string) => item.data[key], isNode: () => !item.data.source, toggleClass() {} })),
        style() {}, destroy() { graph.destroyed = true }, resize() {}, fit() {}, zoom() { return 1 }, center() {},
        panBy(move: unknown) { graph.moves.push(move) },
      }
      graphs.push(graph); return graph
    } },
    './entityGraphPresentation': { graphElements, graphStyles },
    '@/utils/search': { searchErrorCode: (value: any) => value.code },
    '@/components/ui': {
      ...Object.fromEntries(['Button', 'EmptyState', 'ErrorState', 'Skeleton'].map(name => [name, defineComponent({ props: ['title', 'description', 'label'], setup: (props, { attrs, slots }) => () => h(name === 'Button' ? 'button' : 'div', attrs, [props.title, props.description, props.label, slots.default?.()]) })])),
      Checkbox: defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup: (props, { attrs, emit }) => () => h('input', { ...attrs, type: 'checkbox', checked: props.modelValue, onChange: (event: any) => emit('update:modelValue', event.target.checked) }) }),
      Select: defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup: (props, { attrs, emit, slots }) => () => h('select', { ...attrs, value: props.modelValue, onChange: (event: any) => emit('update:modelValue', event.target.value) }, slots.default?.()) }),
    },
    '@/api/graph': { searchGraph: (request: GraphSearchRequest, signal?: AbortSignal) => { requests.push({ request, signal }); return search(request, signal) } },
    '@/api/entity-types': { ENTITY_TYPES }, '@/utils/libraryScope': { scopedLibraryIDs, readLibraryScope: () => savedScope.value },
    '@/composables/useLibraryScope': { useLibraryScope: () => ref<string[]>([]) },
    '@/components/LibraryScopeSelect.vue': { __esModule: true, default: defineComponent({
      emits: ['loaded', 'update:modelValue'], setup(_, { emit }) {
        onMounted(() => emit('loaded', ['official', 'media'].map(id => ({ id, name: id, display_name: id, relation_types: [{ name: '隶属', enabled: true }] }))))
        return () => h('button', { 'data-scope': true, onClick: () => emit('update:modelValue', ['media']) }, 'Media only')
      },
    }) },
  })
  const app = renderer.createApp(defineComponent({ setup: () => () => h(component, { entity: selectedEntity.value, active: active.value, relationTypeOptions: ['发布', '研发', '隶属'], onOpenSource: (id: string, position?: SourceLocation) => sources.push([id, position]), onOpenEntity: (id: string) => centers.push(id) }) }))
  registerPrimitives(app)
  delete app._context.components['t-alert']; delete app._context.components['t-select']
  app.component('t-alert', defineComponent({ props: ['message'], setup: props => () => h('p', { role: 'alert' }, props.message) }))
  app.component('t-select', defineComponent({ props: ['modelValue', 'options'], emits: ['update:modelValue'], setup: (props, { attrs, emit }) => () => h('select', { ...attrs, value: props.modelValue, onChange: (value: unknown) => emit('update:modelValue', value) }) }))
  const errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } }))
  const root = element('root'); app.mount(root)
  let mounted = true
  const unmount = () => { if (mounted) { app.unmount(); mounted = false } }
  t.after(() => { unmount(); for (const [key, descriptor] of previous) { if (descriptor) Object.defineProperty(globalThis, key, descriptor); else delete (globalThis as any)[key] } })
  await settle()
  return { root, selectedEntity, active, savedScope, requests, graphs, sources, centers, unmount, errors }
}

test('entity graph preserves directed parallel edges, scope, evidence navigation, and local selection', async t => {
  const f = await fixture(t)
  assert.deepEqual(f.requests[0]!.request, { libraries: ['official'], entity: { id: center.id }, mode: 'neighbors', hops: 2, relation_types: [], limit: 50 })
  assert.equal(f.graphs[0].options.elements.length, 4)
  assert.deepEqual(f.graphs[0].options.elements.slice(2).map((item: any) => [item.data.id, item.data.source, item.data.target]), [
    ['relation:edge-official', 'entity:product', 'entity:company'], ['relation:edge-media', 'entity:product', 'entity:company'],
  ])
  assert.ok(visibleText(f.root).includes(zh.entityGraph.documentsTruncated))
  assert.ok(!visibleText(f.root).includes(zh.entityGraph.truncated))
  assert.match(visibleText(f.root), /全空间待同步 3 条记录，其中本图涉及 1 条/)
  f.graphs[0].handlers.edge({ target: { data: () => 'edge-official' } }); await settle()
  assert.match(visibleText(f.root), /云帆助手产品介绍/)
  const evidence = descendants(f.root).find(node => node.tag === 'button' && visibleText(node).includes('产品概述'))!
  await click(evidence)
  assert.deepEqual(f.sources, [['entry-official', location]])
  f.graphs[0].handlers.node({ target: { data: () => peer.id } }); await settle()
  assert.deepEqual(f.centers, [peer.id])
  assert.equal(f.requests.length, 1, 'selection and evidence navigation do not query')
  const canvas = descendants(f.root).find(node => node.props.class === 'graph-canvas')!
  let prevented = false
  canvas.props.onKeydown({ key: 'ArrowRight', preventDefault() { prevented = true } })
  assert.equal(prevented, true); assert.deepEqual(f.graphs[0].moves, [{ x: -40, y: 0 }])
  await click(descendants(f.root).find(node => node.tag === 'button' && visibleText(node) === '3 跳')!)
  assert.equal(f.requests.at(-1)!.request.hops, 3)
  f.active.value = false; await settle()
  f.active.value = true; await settle()
  assert.deepEqual(f.requests.at(-1)!.request.libraries, ['official'])
  assert.deepEqual(f.errors, [])
})

test('entity graph cancels stale queries and never renders an older entity response', async t => {
  let resolveOld: (value: unknown) => void = () => {}
  let resolveLeaving: (value: unknown) => void = () => {}
  const f = await fixture(t, request => 'id' in request.entity && request.entity.id === center.id
    ? new Promise(resolve => { resolveOld = resolve })
    : 'id' in request.entity && request.entity.id === 'leaving' ? new Promise(resolve => { resolveLeaving = resolve }) : Promise.resolve({ success: true, data: { ...result(), entities: [peer], edges: [], documents: [], truncated: false, truncations: [], pending_sync: { global: 0, involved: 0 } } }))
  f.selectedEntity.value = peer; await settle()
  assert.equal(f.requests[0]!.signal!.aborted, true)
  assert.equal(f.graphs.length, 0)
  resolveOld({ success: true, data: result() }); await settle()
  assert.equal(f.graphs.length, 0)
  assert.match(visibleText(f.root), /当前条件下未找到关系/)
  f.selectedEntity.value = { ...peer, id: 'leaving' }; await settle()
  f.unmount()
  assert.equal(f.requests.at(-1)!.signal!.aborted, true)
  resolveLeaving({ success: true, data: result() }); await settle()
  assert.equal(f.graphs.length, 0)
  assert.deepEqual(f.errors, [])
})

test('a missing graph entity has an empty state while other failures remain retryable', async t => {
  const f = await fixture(t, async request => {
    if (request.hops === 2) throw { status: 404, code: 1003 }
    throw { status: 503, message: 'Unavailable' }
  })
  assert.ok(visibleText(f.root).includes(zh.entityPage.notFound))
  assert.equal(visibleText(f.root).includes(zh.common.retry), false)
  await click(descendants(f.root).find(node => node.tag === 'button' && visibleText(node) === '1 跳')!)
  assert.ok(visibleText(f.root).includes(zh.entityGraph.unavailable))
  assert.ok(visibleText(f.root).includes(zh.common.retry))
})

test('null graph evidence retains source navigation without inventing a paragraph', async t => {
  const data = result()
  data.edges[0]!.sources[0]!.evidence = null as any
  const f = await fixture(t, async () => ({ success: true, data }))
  f.graphs[0].handlers.edge({ target: { data: () => 'edge-official' } }); await settle()
  await click(descendants(f.root).find(node => node.tag === 'button' && visibleText(node) === '云帆助手产品介绍')!)
  assert.deepEqual(f.sources, [['entry-official', undefined]])
  assert.deepEqual(f.errors, [])
})

test('graph toolbar sends all selected relation types and each supported relationship limit', async t => {
  const f = await fixture(t)
  const toolbar = descendants(f.root).find(node => node.props.class === 'graph-bar')!
  assert.ok(toolbar, 'filters share the hop toolbar')
  const checkbox = (name: string) => descendants(toolbar).find(node => node.tag === 'input' && node.props['aria-label'] === name)!
  assert.ok(checkbox('发布'), 'the library relation type filters are present')
  checkbox('发布').props.onChange({ target: { checked: true } }); await settle()
  checkbox('研发').props.onChange({ target: { checked: true } }); await settle()
  assert.deepEqual(f.requests.at(-1)!.request.relation_types, ['发布', '研发'])
  checkbox('发布').props.onChange({ target: { checked: false } }); await settle()
  assert.deepEqual(f.requests.at(-1)!.request.relation_types, ['研发'])
  const limit = descendants(toolbar).find(node => node.tag === 'select' && node.props['aria-label'] === zh.entityGraph.limit)!
  assert.ok(limit, 'the relationship limit is present')
  for (const count of [20, 100, 50]) {
    limit.props.onChange({ target: { value: String(count) } }); await settle()
    assert.equal(f.requests.at(-1)!.request.limit, count)
    assert.deepEqual(f.requests.at(-1)!.request.relation_types, ['研发'])
  }
  await click(descendants(f.root).find(node => node.tag === 'button' && visibleText(node) === zh.common.clear)!)
  assert.deepEqual(f.requests.at(-1)!.request.relation_types, [])
  assert.deepEqual(f.errors, [])
})
