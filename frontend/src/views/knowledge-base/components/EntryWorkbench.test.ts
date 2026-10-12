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
const entity = { id: 'entity-a', space_id: 1, canonical_name: 'Aurora', entity_type: 'company', aliases: ['Aurora Labs'], created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' } satisfies Entity
const envelope = (data: unknown) => Promise.resolve({ success: true, data })
async function renderWorkbench(options: { locale?: 'zh-CN' | 'en-US'; api?: Record<string, unknown>; status?: object; entities?: Entity[]; scenario?: (state: any, cleanup: () => void) => void | Promise<void> } = {}) {
  const i18n = createI18n({ legacy: false, locale: options.locale || 'zh-CN', messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const cleanups: Array<() => void> = []
  const requests = { lists: [] as any[], details: [] as string[], rebuild: 0, status: 0, merges: [] as any[] }
  const api = { ENTITY_TYPES,
    listEntities: (params: object) => { requests.lists.push(params); return envelope(options.entities || [entity]) },
    getEntity: (id: string) => { requests.details.push(id); return envelope({ entity, entries: [{ id: 'article-a', title: 'Aurora announcement', knowledge_base_id: 'kb-a', created_at: '2026-10-01T00:00:00Z' }] }) },
    mergeEntities: (input: object) => { requests.merges.push(input); return Promise.resolve({ success: true }) },
    getEntityNameEmbeddingRebuildStatus: () => { requests.status++; return envelope(options.status || { pending_count: 1, latest_run: null }) },
    rebuildEntityNameEmbeddings: () => { requests.rebuild++; return envelope({ pending_count: 1, latest_run: { status: 'pending', processed_count: 0, total_count: 1 } }) }, ...options.api }
  const modules: Record<string, unknown> = {
    vue: { ...require('vue'), onBeforeUnmount: (callback: () => void) => cleanups.push(callback) },
    'vue-i18n': { useI18n: () => i18n.global }, 'tdesign-vue-next': { MessagePlugin: { success() {} } },
    '@/api/entities': api,
    '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) },
    '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
    './MergeSuggestions.vue': { __esModule: true, default: defineComponent({ setup: () => () => h('div', { class: 'merge-suggestions-test' }, i18n.global.t('mergeSuggestions.title')) }) } }
  function evaluate(code: string) { const module = { exports: {} as any }; const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText; new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), module, module.exports); return module.exports }
  const component = evaluate(script.content).default; component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup; let state: any
  component.setup = (props: object, context: object) => { state = setup(props, context); onServerPrefetch(async () => { try { await state.refreshAll(); await options.scenario?.(state, () => cleanups.forEach(callback => callback())); await nextTick() } finally { state.clearNameEmbeddingPoll(); if (state.entitySearchTimer) clearTimeout(state.entitySearchTimer) } }); return state }
  const app = createSSRApp(component)
  const stub = defineComponent({ props: ['message', 'description', 'disabled', 'visible'], setup: (props, { attrs, slots }) => () => props.visible === false ? null : h('div', { ...attrs, 'data-disabled': props.disabled || undefined }, [props.message, props.description, slots.default?.()]) })
  for (const name of ['t-button', 't-icon', 't-select', 't-input', 't-alert', 't-empty', 't-loading', 't-dialog']) app.component(name, stub)
  const errors: unknown[] = []; app.config.errorHandler = error => errors.push(error)
  const html = await renderToString(app); assert.deepEqual(errors, [], 'the real workbench template renders without exceptions'); return { html, requests, state }
}
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: the knowledge base header entry is named Entities`, async () => {
    const page = readFileSync(new URL('../KnowledgeBase.vue', import.meta.url), 'utf8')
    const entry = page.match(/<button\b[^>]*@click="activeKbTab = 'entities'"[^>]*>[\s\S]*?<\/button>/)?.[0]
    assert.ok(entry, 'expected the real entity entry button in the knowledge base header')
    const compiled = compileTemplate({ id: 'entity-header-entry', filename: 'KnowledgeBase.vue', source: entry, ssr: true, ssrCssVars: [] })
    assert.deepEqual(compiled.errors, [])
    const module = { exports: {} as { ssrRender?: (...args: any[]) => any } }
    const code = ts.transpileModule(compiled.code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', code)(require, module, module.exports)
    const app = createSSRApp({ data: () => ({ activeKbTab: 'entities' }), ssrRender: module.exports.ssrRender })
    app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } }))
    const html = await renderToString(app)
    assert.equal(html.match(/>([^<]*)<\/button>/)?.[1], locale === 'zh-CN' ? '实体' : 'Entities')
    assert.doesNotMatch(html, /知识工作台|Entry workbench/i)
  })
  test(`${locale}: the workbench only renders entities and merge suggestions navigation`, async () => {
    const { html, state } = await renderWorkbench({ locale }); assert.deepEqual(state.sections, ['entities', 'merges'])
    const nav = html.match(/<nav[\s\S]*?<\/nav>/)?.[0] || ''; assert.equal((nav.match(/<button /g) || []).length, 2)
    assert.ok(nav.includes(locale === 'zh-CN' ? '实体' : 'Entities')); assert.ok(nav.includes(locale === 'zh-CN' ? '待合并建议' : 'Merge suggestions'))
    assert.match(html, locale === 'zh-CN' ? /<h2>实体<\/h2>/ : /<h2>Entities<\/h2>/)
    assert.doesNotMatch(html, /知识工作台|Entry workbench|知识上下文|对象|领域|结构化检索|关注范围|补抽|跟踪工作流|研究报告/i)
    assert.deepEqual(state.entityTypeOptions.value.map((item: any) => item.value), ENTITY_TYPES)
  })
  test(`${locale}: entity details render type, aliases and article actions`, async () => {
    const { html, requests } = await renderWorkbench({ locale, scenario: state => state.selectEntity(entity) }); assert.deepEqual(requests.details, ['entity-a'])
    assert.ok(html.includes(locale === 'zh-CN' ? '公司' : 'Company')); assert.match(html, /Aurora Labs/); assert.match(html, /Aurora announcement/)
    assert.ok(html.includes(locale === 'zh-CN' ? '提及该实体的文章' : 'Articles mentioning this entity')); assert.match(source, /@click="emit\('open-source', article.id\)"/); assert.doesNotMatch(html, /entryWorkbench\./)
  })
  for (const count of [0, 1, 2]) test(`${locale}: an entity list uses the correct alias noun for ${count}`, async () => {
    const { html } = await renderWorkbench({ locale, entities: [{ ...entity, aliases: Array.from({ length: count }, (_, index) => `Alias ${index}`) }] }); assert.ok(html.includes(locale === 'zh-CN' ? `${count} 个别名` : `${count} ${count === 1 ? 'alias' : 'aliases'}`), html)
  })
  test(`${locale}: selecting suggestions renders its panel`, async () => { const { html } = await renderWorkbench({ locale, scenario: state => { state.activeSection.value = 'merges' } }); assert.match(html, /merge-suggestions-test/); assert.doesNotMatch(html, /entity-layout/) })
}
test('signed-in user can view name embedding status and request a rebuild', async () => { const { requests, html } = await renderWorkbench({ scenario: state => state.rebuildNameEmbeddings() }); assert.ok(requests.status > 0); assert.equal(requests.rebuild, 1); assert.match(html, /name-embedding-rebuild/); assert.match(html, /等待执行/) })
test('latest name embedding rebuild failures remain visible', async () => { const { html } = await renderWorkbench({ status: { pending_count: 1, latest_run: { status: 'failed', error: 'Embedding provider unavailable', processed_count: 0, total_count: 1 } } }); assert.match(html, /失败/); assert.match(html, /Embedding provider unavailable/) })
test('unmount cancels name embedding polling and invalidates outstanding status requests', async () => {
  await renderWorkbench({ scenario: async (state, cleanup) => { await state.rebuildNameEmbeddings(); assert.ok(state.nameEmbeddingPollTimer); const request = state.nameEmbeddingRequest; cleanup(); assert.equal(state.nameEmbeddingPollTimer, null); assert.equal(state.nameEmbeddingRequest, request + 1) } })
})
test('older entity details cannot replace a more recently selected entity', async () => {
  let resolveOld: (value: unknown) => void = () => {}; const second = { ...entity, id: 'entity-b', canonical_name: 'Bright' }
  await renderWorkbench({ api: { getEntity: (id: string) => id === entity.id ? new Promise(resolve => { resolveOld = resolve }) : envelope({ entity: second, entries: [] }) }, scenario: async state => { const old = state.selectEntity(entity); await state.selectEntity(second); resolveOld({ success: true, data: { entity, entries: [] } }); await old; assert.equal(state.selectedEntity.value.id, second.id) } })
})
test('failed entity detail loads keep the selection and expose retry feedback', async () => { const { html } = await renderWorkbench({ api: { getEntity: () => Promise.resolve({ success: false, error: 'Article list unavailable' }) }, scenario: state => state.selectEntity(entity) }); assert.match(html, /Aurora/); assert.match(html, /Article list unavailable/) })
test('type filters are sent to the entity list API', async () => { const { requests } = await renderWorkbench({ scenario: async state => { state.entityTypeFilter.value = 'company'; await nextTick(); await state.loadEntities() } }); assert.equal(requests.lists.at(-1).scope, undefined); assert.equal(requests.lists.at(-1).entity_type, 'company'); assert.equal(requests.lists.at(-1).knowledge_base_id, undefined) })
test('merge transfers the selected entity into the explicit retained target', async () => { const { requests } = await renderWorkbench({ scenario: async state => { await state.selectEntity(entity); state.mergeTargetId.value = 'entity-b'; await state.confirmMerge(); assert.equal(state.mergeVisible.value, false) } }); assert.deepEqual(requests.merges, [{ keep_entity_id: 'entity-b', merge_entity_id: 'entity-a' }]) })
