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
const entity = { id: 'entity-a', space_id: 1, canonical_name: 'Aurora', entity_type: 'organization', aliases: ['Aurora Labs'], created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' } satisfies Entity
const envelope = (data: unknown) => Promise.resolve({ success: true, data })
async function renderWorkbench(options: { locale?: 'zh-CN' | 'en-US'; api?: Record<string, unknown>; status?: object; entities?: Entity[]; scenario?: (state: any, cleanup: () => void) => void | Promise<void> } = {}) {
  const i18n = createI18n({ legacy: false, locale: options.locale || 'zh-CN', messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const cleanups: Array<() => void> = []
  const requests = { lists: [] as any[], details: [] as string[], rebuild: 0, status: 0, merges: [] as any[] }
  const api = { ENTITY_TYPES,
    listEntities: (params: object) => { requests.lists.push(params); return envelope(options.entities || [entity]) },
    getEntity: (id: string) => { requests.details.push(id); return envelope({ entity, entries: [{ id: 'article-a', title: 'Aurora announcement', library_id: 'library-a', created_at: '2026-10-01T00:00:00Z' }] }) },
    mergeEntities: (input: object) => { requests.merges.push(input); return Promise.resolve({ success: true }) },
    ...options.api }
  const modules: Record<string, unknown> = {
    vue: { ...require('vue'), onBeforeUnmount: (callback: () => void) => cleanups.push(callback) },
    'vue-i18n': { useI18n: () => i18n.global }, 'tdesign-vue-next': { MessagePlugin: { success() {} } },
    '@/api/entities': api,
    '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) },
    '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
    './EntityActivity.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
    './EntityCandidates.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) }, './EntityEditor.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) } }
  function evaluate(code: string) { const module = { exports: {} as any }; const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText; new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), module, module.exports); return module.exports }
  const component = evaluate(script.content).default; component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup; let state: any
  component.setup = (props: object, context: object) => { state = setup(props, context); onServerPrefetch(async () => { try { await state.refreshAll(); await options.scenario?.(state, () => cleanups.forEach(callback => callback())); await nextTick() } finally { if (state.entitySearchTimer) clearTimeout(state.entitySearchTimer) } }); return state }
  const app = createSSRApp(component)
  const stub = defineComponent({ props: ['message', 'description', 'disabled', 'visible'], setup: (props, { attrs, slots }) => () => props.visible === false ? null : h('div', { ...attrs, 'data-disabled': props.disabled || undefined }, [props.message, props.description, slots.operation?.(), slots.default?.()]) })
  for (const name of ['t-button', 't-icon', 't-select', 't-input', 't-alert', 't-empty', 't-loading', 't-dialog']) app.component(name, stub)
  const errors: unknown[] = []; app.config.errorHandler = error => errors.push(error)
  const html = await renderToString(app); assert.deepEqual(errors, [], 'the real workbench template renders without exceptions'); return { html, requests, state }
}
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: the library header entry is named Entities`, async () => {
    const page = readFileSync(new URL('../Library.vue', import.meta.url), 'utf8')
    const entry = page.match(/<button\b[^>]*@click="selectLibraryTab\('entities'\)"[^>]*>[\s\S]*?<\/button>/)?.[0]
    assert.ok(entry, 'expected the real entity entry button in the library header')
    const compiled = compileTemplate({ id: 'entity-header-entry', filename: 'Library.vue', source: entry, ssr: true, ssrCssVars: [] })
    assert.deepEqual(compiled.errors, [])
    const module = { exports: {} as { ssrRender?: (...args: any[]) => any } }
    const code = ts.transpileModule(compiled.code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', code)(require, module, module.exports)
    const app = createSSRApp({ data: () => ({ activeLibraryTab: 'entities' }), ssrRender: module.exports.ssrRender })
    app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } }))
    const html = await renderToString(app)
    assert.equal(html.match(/>([^<]*)<\/button>/)?.[1], locale === 'zh-CN' ? '实体' : 'Entities')
    assert.doesNotMatch(html, /知识工作台|Entry workbench/i)
  })
  test(`${locale}: the workbench renders only maintained entities`, async () => {
    const { html, state } = await renderWorkbench({ locale })
    assert.doesNotMatch(html, /name-embedding-rebuild|workbench-nav|merge-suggestions-test/)
    assert.deepEqual(state.entityTypeOptions.value.map((item: any) => item.value), ENTITY_TYPES)
  })
  test(`${locale}: entity details render type, aliases and article actions`, async () => {
    const { html, requests } = await renderWorkbench({ locale, scenario: state => state.selectEntity(entity) }); assert.deepEqual(requests.details, ['entity-a'])
    assert.ok(html.includes(locale === 'zh-CN' ? '机构' : 'Institution')); assert.match(html, /Aurora Labs/); assert.match(html, /Aurora announcement/)
    assert.ok(html.includes(locale === 'zh-CN' ? '提及该实体的资料' : 'Materials mentioning this entity')); assert.match(source, /@click="emit\('open-source', article.id\)"/); assert.doesNotMatch(html, /entryWorkbench\./)
  })
  for (const count of [0, 1, 2]) test(`${locale}: an entity list uses the correct alias noun for ${count}`, async () => {
    const { html } = await renderWorkbench({ locale, entities: [{ ...entity, aliases: Array.from({ length: count }, (_, index) => `Alias ${index}`) }] }); assert.ok(html.includes(locale === 'zh-CN' ? `${count} 个别名` : `${count} ${count === 1 ? 'alias' : 'aliases'}`), html)
  })
}
test('older entity details cannot replace a more recently selected entity', async () => {
  let resolveOld: (value: unknown) => void = () => {}; const second = { ...entity, id: 'entity-b', canonical_name: 'Bright' }
  await renderWorkbench({ api: { getEntity: (id: string) => id === entity.id ? new Promise(resolve => { resolveOld = resolve }) : envelope({ entity: second, entries: [] }) }, scenario: async state => { const old = state.selectEntity(entity); await state.selectEntity(second); resolveOld({ success: true, data: { entity, entries: [] } }); await old; assert.equal(state.selectedEntity.value.id, second.id) } })
})
test('failed entity detail loads keep the selection and expose retry feedback', async () => { const { html } = await renderWorkbench({ api: { getEntity: () => Promise.resolve({ success: false, error: 'Article list unavailable' }) }, scenario: state => state.selectEntity(entity) }); assert.match(html, /Aurora/); assert.match(html, /Article list unavailable/) })
test('type filters are sent to the entity list API', async () => { const { requests } = await renderWorkbench({ scenario: async state => { state.entityTypeFilter.value = 'organization'; await nextTick(); await state.loadEntities() } }); assert.equal(requests.lists.at(-1).scope, undefined); assert.equal(requests.lists.at(-1).entity_type, 'organization'); assert.equal(requests.lists.at(-1).library_id, undefined) })
test('merge transfers the selected entity into the explicit retained target', async () => { const { requests } = await renderWorkbench({ scenario: async state => { await state.selectEntity(entity); state.mergeTargetId.value = 'entity-b'; await state.confirmMerge(); assert.equal(state.mergeVisible.value, false) } }); assert.deepEqual(requests.merges, [{ keep_entity_id: 'entity-b', merge_entity_id: 'entity-a' }]) })

for (const locale of ['zh-CN','en-US'] as const) test(`${locale}: entity request failure is visible and retry restores the list`,async()=>{
 let available=false
 const failed=await renderWorkbench({locale,api:{listEntities:async()=>{if(!available)throw new Error('Entities unavailable');return {success:true,data:[entity]}}},scenario:state=>{assert.equal(state.loadError.value,'Entities unavailable')}})
 assert.ok(failed.html.includes('Entities unavailable'))
 assert.ok(failed.html.includes((locale==='zh-CN'?zhCN:enUS).common.retry))
 assert.match(failed.html,/role="alert"/)
 const restored=await renderWorkbench({locale,api:{listEntities:async()=>{if(!available)throw new Error('Entities unavailable');return {success:true,data:[entity]}}},scenario:async state=>{available=true;await state.loadEntities();assert.equal(state.loadError.value,'')}})
 assert.ok(restored.html.includes('Aurora'));assert.ok(!restored.html.includes('Entities unavailable'))
})
