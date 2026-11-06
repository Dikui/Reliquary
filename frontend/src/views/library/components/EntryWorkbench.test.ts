import { isProductShelf, shelfLevel } from '../../../utils/productShelf'
import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zhCN from '../../../i18n/locales/zh-CN'
import enUS from '../../../i18n/locales/en-US'
import { ENTITY_TYPES } from '../../../api/entity-types'
import type { Entity } from '../../../api/entities'
const require = createRequire(import.meta.url), filename = 'EntryWorkbench.vue'
const source = readFileSync(new URL('./EntryWorkbench.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename })
const script = compileScript(descriptor, { id: filename })
const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
assert.deepEqual(template.errors, [])
const entity = { id: 'entity-a', space_id: 1, library_id: 'library-a', canonical_name: 'Aurora', entity_type: 'company', aliases: ['Aurora Labs'], created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' } satisfies Entity
const envelope = (data: unknown) => Promise.resolve({ success: true, data })
async function renderWorkbench(options: { locale?: 'zh-CN' | 'en-US'; api?: Record<string, unknown>; status?: object; entities?: Entity[]; initialEntityId?: string; libraries?: object[]; scenario?: (state: any, cleanup: () => void) => void | Promise<void> } = {}) {
  const i18n = createI18n({ legacy: false, locale: options.locale || 'zh-CN', messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const cleanups: Array<() => void> = []
  const selections: Array<[string, string]> = []
  const requests = { lists: [] as any[], details: [] as string[], rebuild: 0, status: 0, merges: [] as any[] }
  const api = { ENTITY_TYPES,
    listEntityMergeSuggestions: () => envelope([{ id: 'suggestion' }]),
    listEntities: (params: object) => { requests.lists.push(params); return envelope(options.entities || [entity]) },
    getEntity: (id: string) => { requests.details.push(id); return envelope({ entity, entries: [{ id: 'article-a', title: 'Aurora announcement', content_type: 'Research', library_id: 'library-a', created_at: '2026-10-01T00:00:00Z' }] }) },
    mergeEntities: (input: object) => { requests.merges.push(input); return Promise.resolve({ success: true }) },
    getEntityNameEmbeddingRebuildStatus: () => { requests.status++; return envelope(options.status || { pending_count: 1, latest_run: null }) },
    rebuildEntityNameEmbeddings: () => { requests.rebuild++; return envelope({ pending_count: 1, latest_run: { status: 'pending', processed_count: 0, total_count: 1 } }) }, ...options.api }
  const modules: Record<string, unknown> = {
    vue: { ...require('vue'), onBeforeUnmount: (callback: () => void) => cleanups.push(callback) },
    'vue-i18n': { useI18n: () => i18n.global },
    '@/components/ui/feedback': { toast: { success() {} } },
    '@/api/library': { listLibraries: () => envelope(options.libraries || [{ id: 'library-a', name: 'Library A', color: 1 }]) },
    '@/api/relations': { listEntityRelations: () => envelope([]) },
    '@/utils/search': { searchErrorCode: (cause: any) => cause.code },
    '@/components/ui': Object.fromEntries(['Button','Combobox','Dialog','EmptyState','ErrorState','Input','LibMark','Select','Skeleton','Tabs','Tag'].map(name => [name, defineComponent({
      props: ['modelValue', 'title', 'label', 'description', 'disabled', 'items'],
      setup: (props, { attrs, slots }) => () => name === 'Dialog' && !props.modelValue ? null : name === 'Tabs' ? h('div', [h('nav', props.items.map((item: any) => h('button', item.label))), slots[String(props.modelValue)]?.()]) : h(name === 'Button' ? 'button' : 'div', { ...attrs, role: name === 'ErrorState' ? 'alert' : undefined }, [props.title, props.description, props.label, slots.default?.(), slots.footer?.()])
    })])),
    '@/utils/productShelf': { isProductShelf, shelfLevel }, '@/api/entities': api, '@/components/LibraryScopeSelect.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
    '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) },
    '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
    './MergeSuggestions.vue': { __esModule: true, default: defineComponent({ setup: () => () => h('div', { class: 'merge-suggestions-test' }, i18n.global.t('mergeSuggestions.title')) }) } }
  function evaluate(code: string) { const module = { exports: {} as any }; const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText; new Function('require', 'module', 'exports', output)((name: string) => (['./ProductShelf.vue', './EntityRelations.vue', './EntityGraph.vue', './ManagedEntityEditor.vue', './ManagedRelationEditor.vue', './ManagedAttributes.vue', './ManagedReorganization.vue'].includes(name) ? { __esModule: true, default: defineComponent({ setup: () => () => h('div', { 'data-relation-view': name }) }) } : modules[name]) ?? require(name), module, module.exports); return module.exports }
  const component = evaluate(script.content).default; component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup; let state: any
  component.setup = (props: object, context: object) => { state = setup(props, context); onServerPrefetch(async () => { try { await state.refreshAll(); await options.scenario?.(state, () => cleanups.forEach(callback => callback())); await nextTick() } finally { state.clearNameEmbeddingPoll(); if (state.entitySearchTimer) clearTimeout(state.entitySearchTimer) } }); return state }
  const app = createSSRApp(component, { libraryId: 'library-a', initialEntityId: options.initialEntityId, onSelectEntity: (id: string, library: string) => selections.push([id, library]) })
  const stub = defineComponent({ props: ['message', 'description', 'disabled', 'visible'], setup: (props, { attrs, slots }) => () => props.visible === false ? null : h('div', { ...attrs, 'data-disabled': props.disabled || undefined }, [props.message, props.description, slots.operation?.(), slots.default?.()]) })
  for (const name of ['t-button', 't-icon', 't-select', 't-input', 't-alert', 't-empty', 't-loading', 't-dialog']) app.component(name, stub)
  const errors: unknown[] = []; app.config.errorHandler = error => errors.push(error)
  const html = await renderToString(app); assert.deepEqual(errors, [], 'the real workbench template renders without exceptions'); return { html, requests, state, selections }
}
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: entity page renders library filters, real pending count and four detail tabs`, async () => {
    const { html, state } = await renderWorkbench({ locale, scenario: async state => { await state.loadCatalog(); await state.loadPendingCount(); await state.selectEntity(entity) } })
    assert.match(html, /Library A/)
    assert.match(html, /\/platform\/pending\?tab=merges/)
    assert.match(html, /class="mono">1</)
    assert.deepEqual(state.detailTabs.value.map((item: any) => item.value), ['graph', 'list', 'entries', 'aliases'])
    assert.deepEqual(state.entityTypeOptions.value.map((item: any) => item.value), ENTITY_TYPES)
    assert.doesNotMatch(html, /t-button|t-input|t-select/)
  })
  test(`${locale}: materials tab exposes real source titles and creation dates`, async () => {
    const { html, requests } = await renderWorkbench({ locale, scenario: async state => { await state.selectEntity(entity); state.relationView.value = 'entries' } })
    assert.deepEqual(requests.details, ['entity-a'])
    assert.match(html, /Aurora announcement/)
    assert.match(html, /Research/)
    assert.match(html, /<table>/)
    assert.match(source, /@click="emit\('open-source', article.id\)"/)
  })
  test(`${locale}: aliases tab preserves existing name embedding rebuild`, async () => {
    const { html, requests } = await renderWorkbench({ locale, scenario: async state => { await state.selectEntity(entity); state.relationView.value = 'aliases'; await state.rebuildNameEmbeddings() } })
    assert.match(html, /Aurora Labs/)
    assert.equal(requests.rebuild, 1)
    assert.ok(html.includes(locale === 'zh-CN' ? '等待执行' : 'Pending'))
  })
}
test('older entity details cannot replace a more recently selected entity', async () => {
  let resolveOld: (value: unknown) => void = () => {}; const second = { ...entity, id: 'entity-b', canonical_name: 'Bright' }
  await renderWorkbench({ api: { getEntity: (id: string) => id === entity.id ? new Promise(resolve => { resolveOld = resolve }) : envelope({ entity: second, entries: [] }) }, scenario: async state => { const old = state.selectEntity(entity); await state.selectEntity(second); resolveOld({ success: true, data: { entity, entries: [] } }); await old; assert.equal(state.selectedEntity.value.id, second.id) } })
})
test('failed detail requests show retry while a missing entity has a specific empty state', async () => {
  const failed = await renderWorkbench({ api: { getEntity: () => Promise.reject(new Error('Entity unavailable')) }, scenario: state => state.selectEntity(entity) })
  assert.match(failed.html, /Entity unavailable/)
  assert.match(failed.html, /role="alert"/)
  const missing = await renderWorkbench({ api: { getEntity: () => Promise.reject({ status: 404, code: 1003 }) }, scenario: state => state.selectEntity(entity) })
  assert.match(missing.html, /未找到该实体/)
  assert.doesNotMatch(missing.html, /role="alert"/)
})
test('filters preserve library, entity type and the searched name', async () => {
  const { requests } = await renderWorkbench({ scenario: async state => { state.entityFilter.value = 'Aurora'; state.entityTypeFilter.value = 'company'; await nextTick(); await state.loadEntities() } })
  assert.equal(requests.lists.at(-1).entity_type, 'company'); assert.equal(requests.lists.at(-1).library_id, 'library-a'); assert.equal(requests.lists.at(-1).entity, 'Aurora')
})
test('merge selects the retained entity and emits its ID for the URL update', async () => {
  const retained = { ...entity, id: 'entity-b', canonical_name: 'Retained' }
  const { requests, state, selections } = await renderWorkbench({
    api: { getEntity: (id: string) => envelope({ entity: id === retained.id ? retained : entity, entries: [] }) },
    scenario: async state => { await state.selectEntity(entity); state.mergeTargetId.value = retained.id; await state.confirmMerge() },
  })
  assert.deepEqual(requests.merges, [{ keep_entity_id: 'entity-b', merge_entity_id: 'entity-a' }])
  assert.equal(state.mergeVisible.value, false)
  assert.equal(state.selectedEntity.value.id, 'entity-b')
  assert.equal(state.entityDetailLoading.value, false)
  assert.deepEqual(selections.at(-1), ['entity-b', 'library-a'])
})
test('unmount cancels name embedding polling and invalidates outstanding status requests', async () => {
  await renderWorkbench({ scenario: async (state, cleanup) => { await state.rebuildNameEmbeddings(); assert.ok(state.nameEmbeddingPollTimer); const request = state.nameEmbeddingRequest; cleanup(); assert.equal(state.nameEmbeddingPollTimer, null); assert.equal(state.nameEmbeddingRequest, request + 1) } })
})
test('failed entity lists show retry and successful retries restore data', async () => {
  let available = false
  const { html } = await renderWorkbench({ api: { listEntities: async () => { if (!available) throw new Error('Entities unavailable'); return envelope([entity]) } }, scenario: async state => { assert.equal(state.loadError.value, 'Entities unavailable'); available = true; await state.loadEntities(); assert.equal(state.loadError.value, '') } })
  assert.match(html, /Aurora/)
})

test('a deep linked entity loads its owning library without losing the details', async () => {
  const { state } = await renderWorkbench({ api: { getEntity: () => envelope({ entity: { ...entity, library_id: 'library-b' }, entries: [] }) }, scenario: state => state.selectInitialEntity('entity-a') })
  assert.equal(state.selectedLibraryId.value, 'library-b')
  assert.equal(state.selectedEntity.value.id, 'entity-a')
  assert.equal(state.entityDetailLoading.value, false)
})

test('a failed merge keeps the dialog, selected source and target for retry', async () => {
  const { state } = await renderWorkbench({ api: { mergeEntities: () => Promise.resolve({ success: false, error: 'Merge unavailable' }) }, scenario: async state => {
    await state.selectEntity(entity); state.mergeVisible.value = true; state.mergeTargetId.value = 'entity-b'; await state.confirmMerge()
  } })
  assert.equal(state.mergeVisible.value, true)
  assert.equal(state.mergeTargetId.value, 'entity-b')
  assert.equal(state.selectedEntity.value.id, 'entity-a')
  assert.equal(state.mergeError.value, 'Merge unavailable')
  assert.equal(state.merging.value, false)
})

test('switching libraries clears the previous delayed entity search', async () => {
  await renderWorkbench({ scenario: async state => {
    state.entityFilter.value = 'Aurora'; state.scheduleEntitySearch()
    assert.ok(state.entitySearchTimer)
    state.changeLibrary('library-b')
    assert.equal(state.entitySearchTimer, null)
  } })
})

test('related materials show a short ID while retaining the complete ID in the title', async () => {
  const id = 'dc588802-151e-42b2-bc26-ae8832d9a4ec'
  const { html } = await renderWorkbench({
    api: { getEntity: () => envelope({ entity, entries: [{ id, title: 'Readable material', content_type: 'other', created_at: '2026-10-01T00:00:00Z' }] }) },
    scenario: async state => { await state.selectEntity(entity); state.relationView.value = 'entries' },
  })
  assert.match(html, /title="dc588802-151e-42b2-bc26-ae8832d9a4ec">dc588802<\/td>/)
})

test('graph filter options come from enabled relation types in the selected library', async () => {
  const { state } = await renderWorkbench({ libraries: [{ id: 'library-a', name: 'Library A', relation_types: [
    { name: '发布', enabled: true }, { name: '隐藏', enabled: false }, { name: '研发', enabled: true },
  ] }], scenario: state => state.loadCatalog() })
  assert.deepEqual(state.graphRelationTypes.value, ['发布', '研发'])
})

// Side-record links must retain their own profile instead of selecting the first shelf product line.
test('a credential deep link keeps the entity profile in a configured product library', async () => {
  const { state } = await renderWorkbench({ initialEntityId: 'certificate', libraries: [{ id: 'library-a', name: 'Products', entity_mode: 'managed', entity_schema: { kinds: ['product_line', 'platform', 'product', 'model'].map(key => ({ key, name: key, parent_kinds: [] })), fields: [] } }], api: { getEntity: () => envelope({ entity: { ...entity, id: 'certificate', kind: 'credential' }, entries: [] }) }, scenario: async state => { await state.loadCatalog(); await state.selectInitialEntity('certificate') } })
  assert.equal(state.selectedEntity.value.id, 'certificate')
  assert.equal(state.shelfView.value, false)
})
